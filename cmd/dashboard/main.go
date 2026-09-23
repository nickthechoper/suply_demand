// Command dashboard runs a local web server that shows supply/demand/
// equilibrium zones for one or more tickers, replicating LuxAlgo's
// "Supply and Demand Fixed Range" Pine Script indicator using data fetched
// from Yahoo Finance and stored in SQLite.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"supplydemand/internal/server"
	"supplydemand/internal/store"
	"supplydemand/internal/worker"
)

// defaultPeers gives each tracked symbol a sane P/E comparison set out of the
// box. Add an entry here when tracking a new symbol via -symbols; tickers
// without an entry just get an empty peer comparison rather than an error.
var defaultPeers = map[string][]string{
	"MCD":  {"YUM", "SBUX", "QSR", "WEN"},
	"GRAB": {"UBER", "DASH", "SE"},
	"JOBY": {"ACHR", "LILM", "EH"},
}

func main() {
	symbols := flag.String("symbols", "MCD,GRAB,JOBY", "comma-separated ticker symbols to track")
	symbolsCSV := flag.String("symbols-csv", "", "path to a CSV file with a \"Symbol\" column to import tickers from")
	dbPath := flag.String("db", "data/dashboard.db", "path to the SQLite database file")
	lookback := flag.Int("lookback", 500, "number of most recent daily bars used to build zones")
	chartBars := flag.Int("chart-bars", 800, "number of most recent daily bars served to the chart (>= lookback, gives range buttons like 2Y room to work)")
	threshold := flag.Float64("threshold", 10.0, "volume threshold %% that defines a zone edge")
	resolution := flag.Int("resolution", 50, "number of price bins spanning the lookback's price range")
	addr := flag.String("addr", ":8080", "address to serve the dashboard on")
	webDir := flag.String("webdir", "web", "directory containing the dashboard's static frontend")
	fetchRange := flag.String("fetch-range", "5y", "Yahoo Finance history range to fetch")
	fetchPacing := flag.Duration("fetch-pacing", time.Second, "delay between individual Yahoo Finance requests made by the background worker")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("opening store: %v", err)
	}
	defer st.Close()

	explicitSymbols := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "symbols" {
			explicitSymbols = true
		}
	})

	// Only fall back to -symbols' default when nothing is tracked yet and no
	// CSV was given; otherwise a bare `go run ./cmd/dashboard` should just
	// use whatever's already persisted in the database.
	existing, err := st.ListSymbols()
	if err != nil {
		log.Fatalf("listing tracked symbols: %v", err)
	}
	if explicitSymbols || (len(existing) == 0 && *symbolsCSV == "") {
		for _, symbol := range strings.Split(*symbols, ",") {
			symbol = strings.TrimSpace(symbol)
			if symbol == "" {
				continue
			}
			if err := st.UpsertSymbol(symbol); err != nil {
				log.Fatalf("tracking symbol %s: %v", symbol, err)
			}
		}
	}

	if *symbolsCSV != "" {
		imported, err := importSymbolsCSV(st, *symbolsCSV)
		if err != nil {
			log.Fatalf("importing symbols from %s: %v", *symbolsCSV, err)
		}
		log.Printf("imported %d symbols from %s", imported, *symbolsCSV)
	}

	trackedSymbols, err := st.ListSymbols()
	if err != nil {
		log.Fatalf("listing tracked symbols: %v", err)
	}
	if len(trackedSymbols) == 0 {
		log.Fatalf("no symbols tracked; pass -symbols or -symbols-csv")
	}

	worker.BackfillNames(st, trackedSymbols)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(ctx, trackedSymbols, worker.Config{
			Store:      st,
			Peers:      defaultPeers,
			FetchRange: *fetchRange,
			Pacing:     *fetchPacing,
		})
	}()

	srv := server.New(st, server.Config{
		Symbols:      trackedSymbols,
		Lookback:     *lookback,
		ChartBars:    *chartBars,
		ThresholdPct: *threshold,
		Resolution:   *resolution,
		WebDir:       *webDir,
		Peers:        defaultPeers,
	})

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: srv.Routes(),
	}

	serveErrs := make(chan error, 1)
	go func() {
		log.Printf("serving dashboard for %d symbols on http://localhost%s", len(trackedSymbols), *addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErrs <- err
		}
	}()

	select {
	case err := <-serveErrs:
		log.Fatalf("server error: %v", err)
	case <-ctx.Done():
		log.Printf("shutting down...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}

	select {
	case <-workerDone:
	case <-shutdownCtx.Done():
		log.Printf("worker did not stop before shutdown timeout")
	}
}

// importSymbolsCSV reads the "Symbol" column from a CSV export (e.g. a
// TradingView screener export) and tracks each ticker. Symbols are
// normalized from the dotted share-class notation some providers use
// (BRK.A) to the dash Yahoo Finance actually expects (BRK-A).
func importSymbolsCSV(st *store.Store, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return 0, err
	}
	symbolCol := -1
	for i, col := range header {
		if strings.EqualFold(strings.TrimSpace(col), "Symbol") {
			symbolCol = i
			break
		}
	}
	if symbolCol == -1 {
		return 0, fmt.Errorf("no \"Symbol\" column found in %s", path)
	}

	count := 0
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, err
		}
		symbol := strings.TrimSpace(row[symbolCol])
		if symbol == "" {
			continue
		}
		symbol = strings.ReplaceAll(symbol, ".", "-")
		if err := st.UpsertSymbol(symbol); err != nil {
			return count, fmt.Errorf("tracking symbol %s: %w", symbol, err)
		}
		count++
	}

	return count, nil
}
