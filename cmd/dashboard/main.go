// Command dashboard runs a local web server that shows supply/demand/
// equilibrium zones for a single ticker, replicating LuxAlgo's
// "Supply and Demand Fixed Range" Pine Script indicator using data fetched
// from Yahoo Finance and stored in SQLite.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"supplydemand/internal/server"
	"supplydemand/internal/store"
	"supplydemand/internal/yahoo"
)

func main() {
	symbol := flag.String("symbol", "MCD", "ticker symbol to track")
	dbPath := flag.String("db", "data/dashboard.db", "path to the SQLite database file")
	lookback := flag.Int("lookback", 500, "number of most recent daily bars used to build zones")
	chartBars := flag.Int("chart-bars", 800, "number of most recent daily bars served to the chart (>= lookback, gives range buttons like 2Y room to work)")
	threshold := flag.Float64("threshold", 10.0, "volume threshold %% that defines a zone edge")
	resolution := flag.Int("resolution", 50, "number of price bins spanning the lookback's price range")
	addr := flag.String("addr", ":8080", "address to serve the dashboard on")
	webDir := flag.String("webdir", "web", "directory containing the dashboard's static frontend")
	fetchRange := flag.String("fetch-range", "5y", "Yahoo Finance history range to fetch on startup")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("opening store: %v", err)
	}
	defer st.Close()

	latest, err := st.LatestTimestamp(*symbol)
	if err != nil {
		log.Fatalf("checking latest stored bar: %v", err)
	}
	if time.Since(latest) > 20*time.Hour {
		log.Printf("fetching %s daily history from Yahoo Finance (range=%s)...", *symbol, *fetchRange)
		bars, err := yahoo.FetchDaily(*symbol, *fetchRange)
		if err != nil {
			log.Printf("warning: could not fetch fresh data: %v", err)
		} else if err := st.UpsertBars(*symbol, bars); err != nil {
			log.Printf("warning: could not store fetched data: %v", err)
		} else {
			log.Printf("stored %d bars for %s", len(bars), *symbol)
		}
	}

	srv := server.New(st, server.Config{
		Symbol:       *symbol,
		Lookback:     *lookback,
		ChartBars:    *chartBars,
		ThresholdPct: *threshold,
		Resolution:   *resolution,
		WebDir:       *webDir,
	})

	log.Printf("serving dashboard for %s on http://localhost%s", *symbol, *addr)
	if err := http.ListenAndServe(*addr, srv.Routes()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
