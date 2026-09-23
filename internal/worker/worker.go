// Package worker runs the background fetch cycle that keeps bars,
// fundamentals, and company data fresh for every tracked symbol, decoupled
// from the HTTP request path.
package worker

import (
	"context"
	"io"
	"log"
	"net/http"
	"sort"
	"time"

	"supplydemand/internal/openinsider"
	"supplydemand/internal/store"
	"supplydemand/internal/yahoo"
)

const (
	retryBaseDelay = 5 * time.Minute
	retryMaxDelay  = 6 * time.Hour
)

var activity = log.New(io.Discard, "", log.LstdFlags)

func SetActivityLog(w io.Writer) {
	activity.SetOutput(w)
}

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
		activity.Printf("worker: stored name %q for %s (from cached company data)", storedName, symbol)
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

type task struct {
	symbol    string
	name      string
	updatedAt time.Time
	run       func() outcome
}

// Run keeps refreshing tracked data until ctx is cancelled, always working on
// whatever was updated longest ago first. Call it in its own goroutine.
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

		tasks, err := oldestFirst(cfg, client, crumb, symbols)
		if err != nil {
			log.Printf("worker: listing fetch times: %v", err)
			if !sleep(ctx, time.Minute) {
				return
			}
			continue
		}

		var fetches, failures, deferred int
		for _, t := range tasks {
			key := t.symbol + "/" + t.name
			if !pending.ready(key, time.Now()) {
				deferred++
				continue
			}

			switch t.run() {
			case skipped:
				continue
			case fetched:
				fetches++
				pending.succeed(key)
			case failed:
				failures++
				attempts, delay := pending.fail(key, time.Now())
				log.Printf("worker: %s for %s failed (attempt %d), retrying in %s", t.name, t.symbol, attempts, delay)
			}

			if !sleep(ctx, cfg.Pacing) {
				return
			}
		}
		activity.Printf("worker: cycle done across %d symbols: %d fetched, %d failed, %d waiting to retry", len(symbols), fetches, failures, deferred)

		if !sleep(ctx, time.Minute) {
			return
		}
	}
}

func oldestFirst(cfg Config, client *http.Client, crumb string, symbols []string) ([]task, error) {
	times, err := cfg.Store.ListFetchTimes()
	if err != nil {
		return nil, err
	}

	tracked := make(map[string]bool, len(symbols))
	for _, symbol := range symbols {
		tracked[symbol] = true
	}

	var tasks []task
	for _, ft := range times {
		if !tracked[ft.Symbol] {
			continue
		}
		symbol := ft.Symbol
		if ft.Name == "" {
			tasks = append(tasks, task{symbol, "name", time.Time{}, func() outcome { return refreshSymbolName(cfg, client, crumb, symbol) }})
		}
		tasks = append(tasks,
			task{symbol, "bars", ft.Bars, func() outcome { return refreshBars(cfg, symbol) }},
			task{symbol, "fundamentals", ft.Fundamentals, func() outcome { return refreshFundamentals(cfg, client, crumb, symbol) }},
			task{symbol, "company data", ft.CompanyData, func() outcome { return refreshCompanyData(cfg, client, crumb, symbol) }},
			task{symbol, "insider trades", ft.Insider, func() outcome { return refreshInsiderTrades(cfg, symbol) }},
		)
	}

	sort.SliceStable(tasks, func(i, j int) bool {
		return tasks[i].updatedAt.Before(tasks[j].updatedAt)
	})
	return tasks, nil
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

var incrementalRanges = []struct {
	maxGap time.Duration
	name   string
}{
	{4 * 24 * time.Hour, "5d"},
	{25 * 24 * time.Hour, "1mo"},
	{85 * 24 * time.Hour, "3mo"},
	{175 * 24 * time.Hour, "6mo"},
	{360 * 24 * time.Hour, "1y"},
}

func barsRange(cfg Config, symbol string) (string, error) {
	latest, err := cfg.Store.LatestBars(symbol, 1)
	if err != nil || len(latest) == 0 {
		return cfg.FetchRange, err
	}
	gap := time.Since(latest[0].Time)
	for _, r := range incrementalRanges {
		if gap < r.maxGap {
			return r.name, nil
		}
	}
	return cfg.FetchRange, nil
}

func refreshBars(cfg Config, symbol string) outcome {
	rangeStr, err := barsRange(cfg, symbol)
	if err != nil {
		log.Printf("worker: checking latest bar for %s: %v", symbol, err)
	}

	bars, err := yahoo.FetchDaily(symbol, rangeStr)
	if err != nil {
		log.Printf("worker: warning: could not fetch bars for %s: %v", symbol, err)
		return failed
	}
	if err := cfg.Store.UpsertBars(symbol, bars); err != nil {
		log.Printf("worker: warning: could not store bars for %s: %v", symbol, err)
		return failed
	}
	activity.Printf("worker: stored %d bars for %s (range %s)", len(bars), symbol, rangeStr)
	return fetched
}

func refreshFundamentals(cfg Config, client *http.Client, crumb, symbol string) outcome {
	fundamentals, err := yahoo.FetchFundamentalsWithClient(client, crumb, symbol)
	if err != nil {
		log.Printf("worker: warning: could not fetch fundamentals for %s: %v", symbol, err)
		return failed
	}
	if err := cfg.Store.SetFundamentals(symbol, fundamentals); err != nil {
		log.Printf("worker: warning: could not store fundamentals for %s: %v", symbol, err)
		return failed
	}
	activity.Printf("worker: stored fundamentals for %s", symbol)
	return fetched
}

func refreshCompanyData(cfg Config, client *http.Client, crumb, symbol string) outcome {
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
	activity.Printf("worker: stored company data for %s", symbol)
	return fetched
}

func refreshInsiderTrades(cfg Config, symbol string) outcome {
	trades, err := openinsider.Fetch(symbol, openinsider.WindowDays)
	if err != nil {
		log.Printf("worker: warning: could not fetch insider trades for %s: %v", symbol, err)
		return failed
	}
	if err := cfg.Store.SetInsiderTrades(symbol, trades); err != nil {
		log.Printf("worker: warning: could not store insider trades for %s: %v", symbol, err)
		return failed
	}
	activity.Printf("worker: stored %d insider trades for %s", len(trades), symbol)
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
		activity.Printf("worker: stored name %q for %s (from cached company data)", storedName, symbol)
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
	activity.Printf("worker: stored name %q for %s", quotes[0].Name, symbol)
	return fetched
}
