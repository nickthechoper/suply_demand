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

const (
	staleAfter     = 20 * time.Hour
	retryBaseDelay = 5 * time.Minute
	retryMaxDelay  = 6 * time.Hour
)

type outcome int

const (
	skipped outcome = iota
	fetched
	failed
)

type backoff struct {
	failures int
	retryAt  time.Time
}

type retries map[string]backoff

func (r retries) ready(key string, now time.Time) bool {
	return !now.Before(r[key].retryAt)
}

func (r retries) succeed(key string) {
	delete(r, key)
}

func (r retries) fail(key string, now time.Time) (int, time.Duration) {
	b := r[key]
	b.failures++
	delay := retryMaxDelay
	if b.failures <= 8 {
		delay = min(retryBaseDelay<<(b.failures-1), retryMaxDelay)
	}
	b.retryAt = now.Add(delay)
	r[key] = b
	return b.failures, delay
}

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
	pending := retries{}

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

		var fetches, failures, deferred int
		for _, symbol := range symbols {
			steps := []struct {
				name string
				run  func() outcome
			}{
				{"bars", func() outcome { return refreshBars(cfg, symbol) }},
				{"fundamentals", func() outcome { return refreshFundamentals(cfg, client, crumb, symbol) }},
				{"company data", func() outcome { return refreshCompanyData(cfg, client, crumb, symbol) }},
				{"name", func() outcome { return refreshSymbolName(cfg, client, crumb, symbol) }},
			}
			for _, step := range steps {
				key := symbol + "/" + step.name
				if !pending.ready(key, time.Now()) {
					deferred++
					continue
				}

				switch step.run() {
				case skipped:
					continue
				case fetched:
					fetches++
					pending.succeed(key)
				case failed:
					failures++
					attempts, delay := pending.fail(key, time.Now())
					log.Printf("worker: %s for %s failed (attempt %d), retrying in %s", step.name, symbol, attempts, delay)
				}

				if !sleep(ctx, cfg.Pacing) {
					return
				}
			}
		}
		log.Printf("worker: cycle done across %d symbols: %d fetched, %d failed, %d waiting to retry", len(symbols), fetches, failures, deferred)

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

func refreshBars(cfg Config, symbol string) outcome {
	latest, err := cfg.Store.LatestTimestamp(symbol)
	if err != nil {
		log.Printf("worker: checking latest bar for %s: %v", symbol, err)
		return skipped
	}
	if time.Since(latest) <= staleAfter {
		return skipped
	}

	bars, err := yahoo.FetchDaily(symbol, cfg.FetchRange)
	if err != nil {
		log.Printf("worker: warning: could not fetch bars for %s: %v", symbol, err)
		return failed
	}
	if err := cfg.Store.UpsertBars(symbol, bars); err != nil {
		log.Printf("worker: warning: could not store bars for %s: %v", symbol, err)
		return failed
	}
	log.Printf("worker: stored %d bars for %s", len(bars), symbol)
	return fetched
}

func refreshFundamentals(cfg Config, client *http.Client, crumb, symbol string) outcome {
	_, fetchedAt, ok, err := cfg.Store.Fundamentals(symbol)
	if err != nil {
		log.Printf("worker: checking stored fundamentals for %s: %v", symbol, err)
		return skipped
	}
	if ok && time.Since(fetchedAt) <= staleAfter {
		return skipped
	}

	fundamentals, err := yahoo.FetchFundamentalsWithClient(client, crumb, symbol)
	if err != nil {
		log.Printf("worker: warning: could not fetch fundamentals for %s: %v", symbol, err)
		return failed
	}
	if err := cfg.Store.SetFundamentals(symbol, fundamentals); err != nil {
		log.Printf("worker: warning: could not store fundamentals for %s: %v", symbol, err)
		return failed
	}
	log.Printf("worker: stored fundamentals for %s", symbol)
	return fetched
}

func refreshCompanyData(cfg Config, client *http.Client, crumb, symbol string) outcome {
	_, fetchedAt, ok, err := cfg.Store.CompanyData(symbol)
	if err != nil {
		log.Printf("worker: checking stored company data for %s: %v", symbol, err)
		return skipped
	}
	if ok && time.Since(fetchedAt) <= staleAfter {
		return skipped
	}

	companyData, err := yahoo.FetchCompanyDataWithClient(client, crumb, symbol, cfg.Peers[symbol])
	if err != nil {
		log.Printf("worker: warning: could not fetch company data for %s: %v", symbol, err)
		return failed
	}
	if err := cfg.Store.SetCompanyData(symbol, companyData); err != nil {
		log.Printf("worker: warning: could not store company data for %s: %v", symbol, err)
		return failed
	}
	if name := companyData.OwnName(symbol); name != "" {
		if err := cfg.Store.SetSymbolName(symbol, name); err != nil {
			log.Printf("worker: warning: could not store company name for %s: %v", symbol, err)
		}
	}
	log.Printf("worker: stored company data for %s", symbol)
	return fetched
}

func refreshSymbolName(cfg Config, client *http.Client, crumb, symbol string) outcome {
	name, err := cfg.Store.SymbolName(symbol)
	if err != nil {
		log.Printf("worker: checking stored name for %s: %v", symbol, err)
		return skipped
	}
	if name != "" {
		return skipped
	}

	if storedName, err := nameFromStoredCompanyData(cfg.Store, symbol); err != nil {
		log.Printf("worker: checking stored company data for %s: %v", symbol, err)
	} else if storedName != "" {
		if err := cfg.Store.SetSymbolName(symbol, storedName); err != nil {
			log.Printf("worker: warning: could not store name for %s: %v", symbol, err)
			return skipped
		}
		log.Printf("worker: stored name %q for %s (from cached company data)", storedName, symbol)
		return skipped
	}

	quotes, err := yahoo.FetchPeerQuotes(client, crumb, symbol, nil)
	if err != nil || len(quotes) == 0 {
		log.Printf("worker: warning: could not fetch name for %s: %v", symbol, err)
		return failed
	}
	if err := cfg.Store.SetSymbolName(symbol, quotes[0].Name); err != nil {
		log.Printf("worker: warning: could not store name for %s: %v", symbol, err)
		return failed
	}
	log.Printf("worker: stored name %q for %s", quotes[0].Name, symbol)
	return fetched
}
