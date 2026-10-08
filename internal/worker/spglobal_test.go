package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"supplydemand/internal/credit"
	"supplydemand/internal/spglobal"
	"supplydemand/internal/store"
)

func TestSPJobsDailyRefreshAndDiscoveryCooldown(t *testing.T) {
	now := time.Now().UTC()
	url := "https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/682461"
	client := &spglobal.Client{Config: spglobal.Config{Entities: map[string]string{"OVERRIDE": url}}, RefreshInterval: 24 * time.Hour, DiscoveryInterval: 7 * 24 * time.Hour}
	companies := []store.SPCompany{
		{Symbol: "FRESH", Name: "Fresh", Credit: &credit.SPData{SourceURL: url, FetchedAt: now.Add(-time.Hour)}, Discovery: &credit.Discovery{Status: "verified", URL: url}},
		{Symbol: "STALE", Name: "Stale", Credit: &credit.SPData{SourceURL: url, FetchedAt: now.Add(-25 * time.Hour)}, Discovery: &credit.Discovery{Status: "verified", URL: url}},
		{Symbol: "UNKNOWN", Name: "Unknown"},
		{Symbol: "NO_MATCH", Name: "No Match", Discovery: &credit.Discovery{Status: "unmatched", CheckedAt: now.Add(-24 * time.Hour)}},
		{Symbol: "RETRY_DISCOVERY", Name: "Retry", Discovery: &credit.Discovery{Status: "ambiguous", CheckedAt: now.Add(-8 * 24 * time.Hour)}},
		{Symbol: "OVERRIDE", Name: "Override", Discovery: &credit.Discovery{Status: "unmatched", CheckedAt: now}},
		{Symbol: "NO_NAME"},
	}
	jobs := SPJobs(companies, client, now)
	got := map[string]spglobal.Job{}
	for _, j := range jobs {
		got[j.Symbol] = j
	}
	if len(got) != 4 {
		t.Fatalf("jobs: %+v", jobs)
	}
	var order []string
	for _, job := range jobs {
		order = append(order, job.Symbol)
	}
	if !reflect.DeepEqual(order, []string{"UNKNOWN", "RETRY_DISCOVERY", "STALE", "OVERRIDE"}) {
		t.Fatalf("oldest first: %v", order)
	}
	for _, symbol := range []string{"STALE", "UNKNOWN", "RETRY_DISCOVERY", "OVERRIDE"} {
		if _, ok := got[symbol]; !ok {
			t.Errorf("missing %s", symbol)
		}
	}
	if got["STALE"].URL != url || got["OVERRIDE"].URL != url || got["UNKNOWN"].URL != "" {
		t.Fatalf("mapping: %+v", got)
	}
}

func TestSPJobsOrdersFailedAttemptsByAgeAndHonorsPersistentRetries(t *testing.T) {
	now := time.Now().UTC()
	client := &spglobal.Client{RefreshInterval: 24 * time.Hour, DiscoveryInterval: 7 * 24 * time.Hour}
	companies := []store.SPCompany{
		{Symbol: "A", Name: "A", Fetch: &credit.FetchState{AttemptedAt: now.Add(-time.Hour), Failures: 1}},
		{Symbol: "B", Name: "B", Fetch: &credit.FetchState{AttemptedAt: now.Add(-3 * time.Hour), RetryAt: now.Add(time.Hour), Failures: 2}},
		{Symbol: "M", Name: "M", Fetch: &credit.FetchState{AttemptedAt: now.Add(-2 * time.Hour), Failures: 1}},
		{Symbol: "Z", Name: "Z"},
	}
	jobs := SPJobs(companies, client, now)
	var order []string
	for _, j := range jobs {
		order = append(order, j.Symbol)
	}
	if !reflect.DeepEqual(order, []string{"Z", "M", "A"}) {
		t.Fatalf("got %v", order)
	}
}

func TestSPBatchesContinueAfterRestartAndDoNotRepeatFailedFirstSymbol(t *testing.T) {
	db := filepath.Join(t.TempDir(), "queue.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { st.Close() }()
	for _, symbol := range []string{"A", "M", "Z"} {
		if err := st.UpsertSymbol(symbol); err != nil {
			t.Fatal(err)
		}
		if err := st.SetSymbolName(symbol, symbol); err != nil {
			t.Fatal(err)
		}
	}
	client := &spglobal.Client{Config: spglobal.Config{BatchSize: 1}, RefreshInterval: 24 * time.Hour, DiscoveryInterval: 7 * 24 * time.Hour}
	var order []string
	fetch := func(ctx context.Context, jobs []spglobal.Job, apply func(spglobal.Result) error) error {
		job := jobs[0]
		order = append(order, job.Symbol)
		if job.Symbol == "A" {
			return errors.New("browser batch interrupted")
		}
		return apply(spglobal.Result{Symbol: job.Symbol, Status: "unmatched"})
	}
	if _, err := refreshSPCredit(context.Background(), st, client, fetch); err == nil {
		t.Fatal("expected browser error")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	companies, err := st.SPCompanies()
	if err != nil {
		t.Fatal(err)
	}
	state := companies[0].Fetch
	if state == nil || state.AttemptedAt.IsZero() || state.Failures != 1 || !state.RetryAt.After(time.Now()) || state.LastError == "" {
		t.Fatalf("retry state did not survive restart: %+v", state)
	}
	state.RetryAt = time.Now().Add(-time.Minute)
	if err := st.SetSPFetchState("A", *state); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := refreshSPCredit(context.Background(), st, client, fetch); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(order, []string{"A", "M", "Z"}) {
		t.Fatalf("restarted from the beginning: %v", order)
	}
	if _, err := refreshSPCredit(context.Background(), st, client, fetch); err == nil {
		t.Fatal("expected another A failure")
	}
	companies, err = st.SPCompanies()
	if err != nil {
		t.Fatal(err)
	}
	if companies[0].Fetch.Failures != 2 || time.Until(companies[0].Fetch.RetryAt) < 9*time.Minute {
		t.Fatalf("retry count reset: %+v", companies[0].Fetch)
	}
	state = companies[0].Fetch
	state.RetryAt = time.Now().Add(-time.Minute)
	if err := st.SetSPFetchState("A", *state); err != nil {
		t.Fatal(err)
	}
	succeed := func(ctx context.Context, jobs []spglobal.Job, apply func(spglobal.Result) error) error {
		return apply(spglobal.Result{Symbol: jobs[0].Symbol, Status: "unmatched"})
	}
	if _, err := refreshSPCredit(context.Background(), st, client, succeed); err != nil {
		t.Fatal(err)
	}
	companies, err = st.SPCompanies()
	if err != nil {
		t.Fatal(err)
	}
	state = companies[0].Fetch
	if state.Failures != 0 || !state.RetryAt.IsZero() || state.LastError != "" {
		t.Fatalf("success did not reset retries: %+v", state)
	}
}

func openSPStore(t *testing.T, symbols ...string) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "sp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, symbol := range symbols {
		if err := st.UpsertSymbol(symbol); err != nil {
			t.Fatal(err)
		}
		if err := st.SetSymbolName(symbol, symbol); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestSPBlockedBatchPausesWithoutChargingUnattemptedCompanies(t *testing.T) {
	st := openSPStore(t, "A", "M", "Z")
	earlier := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	prior := credit.FetchState{AttemptedAt: earlier, Failures: 1, RetryAt: earlier.Add(time.Hour), LastError: "old"}
	if err := st.SetSPFetchState("M", prior); err != nil {
		t.Fatal(err)
	}
	client := &spglobal.Client{Config: spglobal.Config{BatchSize: 3}, RefreshInterval: 24 * time.Hour, DiscoveryInterval: 7 * 24 * time.Hour}
	fetch := func(ctx context.Context, jobs []spglobal.Job, apply func(spglobal.Result) error) error {
		if err := apply(spglobal.Result{Symbol: jobs[0].Symbol, Status: "unmatched"}); err != nil {
			return err
		}
		return fmt.Errorf("%w: S&P requires browser verification", spglobal.ErrBlocked)
	}
	ran, err := refreshSPCredit(context.Background(), st, client, fetch)
	if !errors.Is(err, spglobal.ErrBlocked) || ran != 1 {
		t.Fatalf("ran=%d err=%v", ran, err)
	}
	companies, err := st.SPCompanies()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]store.SPCompany{}
	for _, c := range companies {
		byName[c.Symbol] = c
	}
	if byName["A"].Discovery == nil || byName["A"].Discovery.Status != "unmatched" {
		t.Fatalf("completed company not stored: %+v", byName["A"])
	}
	m := byName["M"].Fetch
	if m == nil || m.Failures != 1 || !m.AttemptedAt.Equal(prior.AttemptedAt) || !m.RetryAt.Equal(prior.RetryAt) || m.LastError != "old" {
		t.Fatalf("blocked batch changed M: %+v", m)
	}
	z := byName["Z"].Fetch
	if z != nil && (z.Failures != 0 || !z.RetryAt.IsZero() || z.LastError != "") {
		t.Fatalf("blocked batch charged Z: %+v", z)
	}
}

func TestSPOrdinaryBatchErrorStillChargesUnattemptedCompanies(t *testing.T) {
	st := openSPStore(t, "A", "Z")
	client := &spglobal.Client{Config: spglobal.Config{BatchSize: 2}, RefreshInterval: 24 * time.Hour, DiscoveryInterval: 7 * 24 * time.Hour}
	fetch := func(context.Context, []spglobal.Job, func(spglobal.Result) error) error { return errors.New("crashed") }
	if _, err := refreshSPCredit(context.Background(), st, client, fetch); err == nil {
		t.Fatal("expected error")
	}
	companies, _ := st.SPCompanies()
	for _, c := range companies {
		if c.Fetch == nil || c.Fetch.Failures != 1 {
			t.Fatalf("%s: %+v", c.Symbol, c.Fetch)
		}
	}
}

func TestSPUnresolvedCandidatesAreStoredWithDiscovery(t *testing.T) {
	st := openSPStore(t, "A", "B")
	client := &spglobal.Client{Config: spglobal.Config{BatchSize: 2}, RefreshInterval: 24 * time.Hour, DiscoveryInterval: 7 * 24 * time.Hour}
	candidates := []credit.Candidate{{Name: "A Holdings", URL: "https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/1"}}
	fetch := func(ctx context.Context, jobs []spglobal.Job, apply func(spglobal.Result) error) error {
		if err := apply(spglobal.Result{Symbol: "A", Status: "ambiguous", Candidates: candidates}); err != nil {
			return err
		}
		return apply(spglobal.Result{Symbol: "B", Status: "unmatched"})
	}
	ran, err := refreshSPCredit(context.Background(), st, client, fetch)
	if err != nil || ran != 2 {
		t.Fatalf("ran=%d err=%v", ran, err)
	}
	companies, _ := st.SPCompanies()
	if !reflect.DeepEqual(companies[0].Discovery.Candidates, candidates) || len(companies[1].Discovery.Candidates) != 0 {
		t.Fatalf("candidates: %+v %+v", companies[0].Discovery, companies[1].Discovery)
	}
}

func TestRefreshSPCreditReportsNoWorkWhenNothingIsDue(t *testing.T) {
	st := openSPStore(t)
	client := &spglobal.Client{Config: spglobal.Config{BatchSize: 5}}
	ran, err := refreshSPCredit(context.Background(), st, client, nil)
	if ran != 0 || err != nil {
		t.Fatalf("ran=%d err=%v", ran, err)
	}
}

func TestSPLoopDelayPausesOnBlockSpacesBatchesAndPollsWhenIdle(t *testing.T) {
	client := &spglobal.Client{BatchInterval: 5 * time.Minute, BlockedPause: time.Hour}
	for name, tc := range map[string]struct {
		ran  int
		err  error
		want time.Duration
	}{
		"blocked":           {1, fmt.Errorf("%w: x", spglobal.ErrBlocked), time.Hour},
		"other error":       {0, errors.New("x"), retryBaseDelay},
		"batch ran":         {25, nil, 5 * time.Minute},
		"idle":              {0, nil, time.Minute},
		"zero batch gap":    {3, nil, time.Minute},
		"blocked after run": {7, fmt.Errorf("%w: x", spglobal.ErrBlocked), time.Hour},
	} {
		c := *client
		if name == "zero batch gap" {
			c.BatchInterval = 0
		}
		if got := spLoopDelay(&c, tc.ran, tc.err); got != tc.want {
			t.Errorf("%s: %s, want %s", name, got, tc.want)
		}
	}
}

func TestSPJobsDoesNotReorderCallersSlice(t *testing.T) {
	now := time.Now().UTC()
	client := &spglobal.Client{RefreshInterval: 24 * time.Hour, DiscoveryInterval: 7 * 24 * time.Hour}
	companies := []store.SPCompany{
		{Symbol: "A", Name: "A", Fetch: &credit.FetchState{AttemptedAt: now.Add(-time.Hour)}},
		{Symbol: "B", Name: "B", Fetch: &credit.FetchState{AttemptedAt: now.Add(-2 * time.Hour)}},
	}
	jobs := SPJobs(companies, client, now)
	if jobs[0].Symbol != "B" || companies[0].Symbol != "A" || companies[1].Symbol != "B" {
		t.Fatalf("jobs=%+v companies=%+v", jobs, companies)
	}
}
