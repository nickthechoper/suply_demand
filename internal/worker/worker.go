// Package worker runs the background fetch cycle that keeps bars,
// fundamentals, and company data fresh for every tracked symbol, decoupled
// from the HTTP request path.
package worker

import (
	"context"
	"log"
	"net/http"
	"time"

	"supplydemand/internal/store"
	"supplydemand/internal/yahoo"
)

const staleAfter = 20 * time.Hour

type Config struct {
	Store      *store.Store
	Peers      map[string][]string
	FetchRange string
	Pacing     time.Duration
}

// BackfillNames fills in company names for tracked symbols from company
// data already stored locally, without making any network calls. Call it
// synchronously at startup, before Run, so the dashboard shows known names
// immediately rather than waiting for the paced background loop to cycle
// around to each symbol.
func BackfillNames(st *store.Store, symbols []string) {
	for _, symbol := range symbols {
		name, err := st.SymbolName(symbol)
		if err != nil {
			log.Printf("worker: checking stored name for %s: %v", symbol, err)
			continue
		}
		if name != "" {
			continue
		}

		storedName, err := nameFromStoredCompanyData(st, symbol)
		if err != nil {
			log.Printf("worker: checking stored company data for %s: %v", symbol, err)
			continue
		}
		if storedName == "" {
			continue
		}
		if err := st.SetSymbolName(symbol, storedName); err != nil {
			log.Printf("worker: warning: could not store name for %s: %v", symbol, err)
			continue
		}
		log.Printf("worker: stored name %q for %s (from cached company data)", storedName, symbol)
	}
}

// nameFromStoredCompanyData returns the company name for symbol pulled out
// of its already-stored company data, or "" if there's no such data or it
// doesn't include the name.
func nameFromStoredCompanyData(st *store.Store, symbol string) (string, error) {
	companyData, _, ok, err := st.CompanyData(symbol)
	if err != nil || !ok {
		return "", err
	}
	return companyData.OwnName(symbol), nil
}

// Run loops, cycling through symbols and refreshing whatever's stale, until
// ctx is cancelled. Call it in its own goroutine.
func Run(ctx context.Context, symbols []string, cfg Config) {
	for {
		if ctx.Err() != nil {
			return
		}

		client, crumb, err := yahoo.NewAuthedClient()
		if err != nil {
			log.Printf("worker: could not start authed session: %v", err)
			if !sleep(ctx, time.Minute) {
				return
			}
			continue
		}

		for _, symbol := range symbols {
			refreshBars(cfg, symbol)
			if !sleep(ctx, cfg.Pacing) {
				return
			}
			refreshFundamentals(cfg, client, crumb, symbol)
			if !sleep(ctx, cfg.Pacing) {
				return
			}
			refreshCompanyData(cfg, client, crumb, symbol)
			if !sleep(ctx, cfg.Pacing) {
				return
			}
			refreshSymbolName(cfg, client, crumb, symbol)
			if !sleep(ctx, cfg.Pacing) {
				return
			}
		}

		if !sleep(ctx, time.Minute) {
			return
		}
	}
}

// sleep waits for d or ctx cancellation, whichever comes first. It reports
// whether the sleep completed normally (false means ctx was cancelled and
// the caller should stop).
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func refreshBars(cfg Config, symbol string) {
	latest, err := cfg.Store.LatestTimestamp(symbol)
	if err != nil {
		log.Printf("worker: checking latest bar for %s: %v", symbol, err)
		return
	}
	if time.Since(latest) <= staleAfter {
		return
	}

	bars, err := yahoo.FetchDaily(symbol, cfg.FetchRange)
	if err != nil {
		log.Printf("worker: warning: could not fetch bars for %s: %v", symbol, err)
		return
	}
	if err := cfg.Store.UpsertBars(symbol, bars); err != nil {
		log.Printf("worker: warning: could not store bars for %s: %v", symbol, err)
		return
	}
	log.Printf("worker: stored %d bars for %s", len(bars), symbol)
}

func refreshFundamentals(cfg Config, client *http.Client, crumb, symbol string) {
	_, fetchedAt, ok, err := cfg.Store.Fundamentals(symbol)
	if err != nil {
		log.Printf("worker: checking stored fundamentals for %s: %v", symbol, err)
		return
	}
	if ok && time.Since(fetchedAt) <= staleAfter {
		return
	}

	fundamentals, err := yahoo.FetchFundamentalsWithClient(client, crumb, symbol)
	if err != nil {
		log.Printf("worker: warning: could not fetch fundamentals for %s: %v", symbol, err)
		return
	}
	if err := cfg.Store.SetFundamentals(symbol, fundamentals); err != nil {
		log.Printf("worker: warning: could not store fundamentals for %s: %v", symbol, err)
		return
	}
	log.Printf("worker: stored fundamentals for %s", symbol)
}

func refreshCompanyData(cfg Config, client *http.Client, crumb, symbol string) {
	_, fetchedAt, ok, err := cfg.Store.CompanyData(symbol)
	if err != nil {
		log.Printf("worker: checking stored company data for %s: %v", symbol, err)
		return
	}
	if ok && time.Since(fetchedAt) <= staleAfter {
		return
	}

	companyData, err := yahoo.FetchCompanyDataWithClient(client, crumb, symbol, cfg.Peers[symbol])
	if err != nil {
		log.Printf("worker: warning: could not fetch company data for %s: %v", symbol, err)
		return
	}
	if err := cfg.Store.SetCompanyData(symbol, companyData); err != nil {
		log.Printf("worker: warning: could not store company data for %s: %v", symbol, err)
		return
	}
	if name := companyData.OwnName(symbol); name != "" {
		if err := cfg.Store.SetSymbolName(symbol, name); err != nil {
			log.Printf("worker: warning: could not store company name for %s: %v", symbol, err)
		}
	}
	log.Printf("worker: stored company data for %s", symbol)
}

// refreshSymbolName backfills a symbol's company name when it's still
// unknown, independent of whether the heavier company data fetch above ran
// this cycle (e.g. because it was already fresh). It first checks whether
// the name can be pulled out of company data already stored for the symbol,
// falling back to a lightweight quote lookup only if that data isn't there.
func refreshSymbolName(cfg Config, client *http.Client, crumb, symbol string) {
	name, err := cfg.Store.SymbolName(symbol)
	if err != nil {
		log.Printf("worker: checking stored name for %s: %v", symbol, err)
		return
	}
	if name != "" {
		return
	}

	if storedName, err := nameFromStoredCompanyData(cfg.Store, symbol); err != nil {
		log.Printf("worker: checking stored company data for %s: %v", symbol, err)
	} else if storedName != "" {
		if err := cfg.Store.SetSymbolName(symbol, storedName); err != nil {
			log.Printf("worker: warning: could not store name for %s: %v", symbol, err)
			return
		}
		log.Printf("worker: stored name %q for %s (from cached company data)", storedName, symbol)
		return
	}

	quotes, err := yahoo.FetchPeerQuotes(client, crumb, symbol, nil)
	if err != nil || len(quotes) == 0 {
		log.Printf("worker: warning: could not fetch name for %s: %v", symbol, err)
		return
	}
	if err := cfg.Store.SetSymbolName(symbol, quotes[0].Name); err != nil {
		log.Printf("worker: warning: could not store name for %s: %v", symbol, err)
		return
	}
	log.Printf("worker: stored name %q for %s", quotes[0].Name, symbol)
}
