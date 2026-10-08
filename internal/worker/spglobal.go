package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sort"
	"time"

	"supplydemand/internal/credit"
	"supplydemand/internal/spglobal"
	"supplydemand/internal/store"
)

func SPJobs(companies []store.SPCompany, client *spglobal.Client, now time.Time) []spglobal.Job {
	companies = slices.Clone(companies)
	sort.SliceStable(companies, func(i, j int) bool {
		return spCheckedAt(companies[i]).Before(spCheckedAt(companies[j]))
	})
	var jobs []spglobal.Job
	for _, c := range companies {
		if c.Fetch != nil && now.Before(c.Fetch.RetryAt) {
			continue
		}
		url := client.Config.Entities[c.Symbol]
		if url == "" && c.Discovery != nil && c.Discovery.Status == "verified" {
			url = c.Discovery.URL
		}
		if c.Name == "" {
			continue
		}
		if url != "" {
			if c.Credit != nil && c.Credit.SourceURL == url && now.Sub(c.Credit.FetchedAt) < client.RefreshInterval {
				continue
			}
		} else if c.Discovery != nil && now.Sub(c.Discovery.CheckedAt) < client.DiscoveryInterval {
			continue
		}
		jobs = append(jobs, spglobal.Job{Symbol: c.Symbol, Name: c.Name, URL: url})
	}
	return jobs
}

func spCheckedAt(c store.SPCompany) time.Time {
	var checked time.Time
	if c.Credit != nil {
		checked = c.Credit.FetchedAt
	}
	if c.Discovery != nil && c.Discovery.CheckedAt.After(checked) {
		checked = c.Discovery.CheckedAt
	}
	if c.Fetch != nil && c.Fetch.AttemptedAt.After(checked) {
		checked = c.Fetch.AttemptedAt
	}
	return checked
}

func RunSPCredit(ctx context.Context, st *store.Store, client *spglobal.Client) {
	if client == nil {
		return
	}
	readyLogged := false
	for ctx.Err() == nil {
		if !client.Ready() {
			if !readyLogged {
				log.Printf("S&P daily refresh waiting for saved session %s or SP_USERNAME/SP_PASSWORD", client.Config.Session)
				readyLogged = true
			}
			if !sleep(ctx, time.Minute) {
				return
			}
			continue
		}
		readyLogged = false
		ran, err := refreshSPCredit(ctx, st, client, client.Refresh)
		if err != nil {
			log.Printf("S&P refresh: %v", err)
		}
		if !sleep(ctx, spLoopDelay(client, ran, err)) {
			return
		}
	}
}

func spLoopDelay(client *spglobal.Client, ran int, err error) time.Duration {
	switch {
	case errors.Is(err, spglobal.ErrBlocked):
		return client.BlockedPause
	case err != nil:
		return retryBaseDelay
	case ran > 0:
		return max(client.BatchInterval, time.Minute)
	}
	return time.Minute
}

func RefreshSPCredit(ctx context.Context, st *store.Store, client *spglobal.Client, symbols ...string) error {
	_, err := refreshSPCredit(ctx, st, client, client.Refresh, symbols...)
	return err
}

func refreshSPCredit(ctx context.Context, st *store.Store, client *spglobal.Client, fetch func(context.Context, []spglobal.Job, func(spglobal.Result) error) error, symbols ...string) (int, error) {
	companies, err := st.SPCompanies()
	if err != nil {
		return 0, err
	}
	var jobs []spglobal.Job
	states := map[string]credit.FetchState{}
	previous := map[string]credit.FetchState{}
	for _, company := range companies {
		if company.Fetch != nil {
			states[company.Symbol] = *company.Fetch
			previous[company.Symbol] = *company.Fetch
		}
	}
	selected := map[string]bool{}
	for _, symbol := range symbols {
		selected[symbol] = true
	}
	for _, job := range SPJobs(companies, client, time.Now()) {
		if len(selected) > 0 && !selected[job.Symbol] {
			continue
		}
		jobs = append(jobs, job)
		if len(jobs) >= client.Config.BatchSize {
			break
		}
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	for _, job := range jobs {
		state := states[job.Symbol]
		state.AttemptedAt = time.Now().UTC()
		if err := st.SetSPFetchState(job.Symbol, state); err != nil {
			return 0, err
		}
		states[job.Symbol] = state
	}
	recordFailure := func(symbol, message string) error {
		state := states[symbol]
		state.Failures++
		delay := retryDelay(state.Failures)
		state.RetryAt = time.Now().UTC().Add(delay)
		state.LastError = message
		if err := st.SetSPFetchState(symbol, state); err != nil {
			return err
		}
		states[symbol] = state
		log.Printf("S&P %s failed, retry in %s: %s", symbol, delay, message)
		return nil
	}
	activity.Printf("S&P fetching %d companies, oldest attempt first: %s", len(jobs), jobs[0].Symbol)
	seen := map[string]bool{}
	failures := 0
	err = fetch(ctx, jobs, func(result spglobal.Result) error {
		if result.Status == "error" {
			failures++
			if err := recordFailure(result.Symbol, result.Error); err != nil {
				return err
			}
			seen[result.Symbol] = true
			return nil
		}
		state := credit.Discovery{Status: result.Status, URL: result.URL, Name: result.Name, CheckedAt: time.Now().UTC(), Candidates: result.Candidates}
		if err := st.SetSPCredit(result.Symbol, result.Data, state); err != nil {
			return err
		}
		if err := st.SetSPFetchState(result.Symbol, credit.FetchState{AttemptedAt: states[result.Symbol].AttemptedAt}); err != nil {
			return err
		}
		seen[result.Symbol] = true
		if result.Data != nil {
			activity.Printf("S&P stored %s %s for %s", result.Data.Rating, result.Data.Outlook, result.Symbol)
		} else {
			activity.Printf("S&P discovery %s: %s", result.Symbol, result.Status)
		}
		return nil
	})
	if err != nil {
		blocked := errors.Is(err, spglobal.ErrBlocked)
		for _, job := range jobs {
			if seen[job.Symbol] {
				continue
			}
			var saveErr error
			if blocked {
				saveErr = st.SetSPFetchState(job.Symbol, previous[job.Symbol])
			} else {
				saveErr = recordFailure(job.Symbol, err.Error())
			}
			if saveErr != nil {
				return len(seen), fmt.Errorf("%w; storing S&P failure: %v", err, saveErr)
			}
		}
	}
	if err == nil && failures > 0 {
		return len(jobs), fmt.Errorf("%d S&P companies failed; last successful ratings retained", failures)
	}
	return len(seen), err
}
