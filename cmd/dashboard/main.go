// Command dashboard runs a local web server that shows supply/demand/
// equilibrium zones for one or more tickers, replicating LuxAlgo's
// "Supply and Demand Fixed Range" Pine Script indicator using data fetched
// from Yahoo Finance and stored in SQLite.
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"supplydemand/internal/server"
	"supplydemand/internal/store"
	"supplydemand/internal/yahoo"
)

// defaultPeers gives each tracked symbol a sane P/E comparison set out of the
// box. Add an entry here when tracking a new symbol via -symbols; tickers
// without an entry just get an empty peer comparison rather than an error.
var defaultPeers = map[string][]string{
	"MCD":  {"YUM", "SBUX", "QSR", "WEN"},
	"GRAB": {"UBER", "DASH", "SE"},
}

func main() {
	symbols := flag.String("symbols", "MCD,GRAB", "comma-separated ticker symbols to track")
	dbPath := flag.String("db", "data/dashboard.db", "path to the SQLite database file")
	lookback := flag.Int("lookback", 500, "number of most recent daily bars used to build zones")
	chartBars := flag.Int("chart-bars", 800, "number of most recent daily bars served to the chart (>= lookback, gives range buttons like 2Y room to work)")
	threshold := flag.Float64("threshold", 10.0, "volume threshold %% that defines a zone edge")
	resolution := flag.Int("resolution", 50, "number of price bins spanning the lookback's price range")
	addr := flag.String("addr", ":8080", "address to serve the dashboard on")
	webDir := flag.String("webdir", "web", "directory containing the dashboard's static frontend")
	fetchRange := flag.String("fetch-range", "5y", "Yahoo Finance history range to fetch on startup")
	flag.Parse()
	symbolList := strings.Split(*symbols, ",")

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("opening store: %v", err)
	}
	defer st.Close()

	for _, symbol := range symbolList {
		fetchSymbolData(st, symbol, defaultPeers[symbol], *fetchRange)
	}

	srv := server.New(st, server.Config{
		Symbols:      symbolList,
		Lookback:     *lookback,
		ChartBars:    *chartBars,
		ThresholdPct: *threshold,
		Resolution:   *resolution,
		WebDir:       *webDir,
		Peers:        defaultPeers,
	})

	log.Printf("serving dashboard for %s on http://localhost%s", strings.Join(symbolList, ", "), *addr)
	if err := http.ListenAndServe(*addr, srv.Routes()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// fetchSymbolData refreshes bars, fundamentals, and company data for symbol
// from Yahoo Finance if the stored data is missing or more than 20 hours old.
func fetchSymbolData(st *store.Store, symbol string, peerSymbols []string, fetchRange string) {
	latest, err := st.LatestTimestamp(symbol)
	if err != nil {
		log.Fatalf("checking latest stored bar for %s: %v", symbol, err)
	}
	if time.Since(latest) > 20*time.Hour {
		log.Printf("fetching %s daily history from Yahoo Finance (range=%s)...", symbol, fetchRange)
		bars, err := yahoo.FetchDaily(symbol, fetchRange)
		if err != nil {
			log.Printf("warning: could not fetch fresh data for %s: %v", symbol, err)
		} else if err := st.UpsertBars(symbol, bars); err != nil {
			log.Printf("warning: could not store fetched data for %s: %v", symbol, err)
		} else {
			log.Printf("stored %d bars for %s", len(bars), symbol)
		}
	}

	_, fundamentalsFetchedAt, haveFundamentals, err := st.Fundamentals(symbol)
	if err != nil {
		log.Fatalf("checking stored fundamentals for %s: %v", symbol, err)
	}
	if !haveFundamentals || time.Since(fundamentalsFetchedAt) > 20*time.Hour {
		log.Printf("fetching %s fundamentals from Yahoo Finance...", symbol)
		fundamentals, err := yahoo.FetchFundamentals(symbol)
		if err != nil {
			log.Printf("warning: could not fetch fundamentals for %s: %v", symbol, err)
		} else if err := st.SetFundamentals(symbol, fundamentals); err != nil {
			log.Printf("warning: could not store fundamentals for %s: %v", symbol, err)
		} else {
			log.Printf("stored fundamentals for %s", symbol)
		}
	}

	_, companyFetchedAt, haveCompanyData, err := st.CompanyData(symbol)
	if err != nil {
		log.Fatalf("checking stored company data for %s: %v", symbol, err)
	}
	if !haveCompanyData || time.Since(companyFetchedAt) > 20*time.Hour {
		log.Printf("fetching %s company data from Yahoo Finance...", symbol)
		companyData, err := yahoo.FetchCompanyData(symbol, peerSymbols)
		if err != nil {
			log.Printf("warning: could not fetch company data for %s: %v", symbol, err)
		} else if err := st.SetCompanyData(symbol, companyData); err != nil {
			log.Printf("warning: could not store company data for %s: %v", symbol, err)
		} else {
			log.Printf("stored company data for %s", symbol)
		}
	}
}
