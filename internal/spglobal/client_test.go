package spglobal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"supplydemand/internal/credit"
)

const testURL = "https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/"

func verified(symbol, id, name string, ratings ...credit.IssuerRating) Result {
	url := testURL + id
	return Result{Symbol: symbol, Status: "verified", URL: url, Name: name, CheckedAt: time.Now().UTC(), Data: &credit.SPData{
		EntityName: name, EntityID: id, SourceURL: url, FetchedAt: time.Now().UTC(), Ratings: ratings,
	}}
}

func lines(t *testing.T, results ...Result) string {
	t.Helper()
	var b strings.Builder
	for _, r := range results {
		blob, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(blob)
		b.WriteByte('\n')
	}
	return b.String()
}

func collect(t *testing.T, input string, jobs []Job) (map[string]Result, int, error) {
	t.Helper()
	got := map[string]Result{}
	n, err := readResults(strings.NewReader(input), jobs, func(r Result) error {
		got[r.Symbol] = r
		return nil
	})
	return got, n, err
}

func TestReadResultsRejectsOneCompanyWithoutAbortingTheBatch(t *testing.T) {
	jobs := []Job{
		{Symbol: "OK", Name: "Good Corp"},
		{Symbol: "SHORT", Name: "Short Only Inc"},
		{Symbol: "NAME", Name: "Expected Name Ltd"},
		{Symbol: "URL", Name: "Pinned Co", URL: testURL + "9"},
		{Symbol: "LAST", Name: "Last Plc"},
	}
	lt := credit.IssuerRating{RatingType: "Foreign Currency LT", Rating: "A", Outlook: "Stable"}
	input := lines(t,
		verified("OK", "1", "Good Corporation", lt),
		verified("SHORT", "2", "Short Only Inc", credit.IssuerRating{RatingType: "Foreign Currency ST", Rating: "A-1"}),
		verified("NAME", "3", "Another Name Ltd", lt),
		verified("URL", "4", "Pinned Co", lt),
		verified("LAST", "5", "Last", lt),
	)
	got, n, err := collect(t, input, jobs)
	if err != nil || n != len(jobs) {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got["OK"].Status != "verified" || got["LAST"].Status != "verified" || got["LAST"].Data.Rating != "A" {
		t.Fatalf("valid companies: %+v %+v", got["OK"], got["LAST"])
	}
	for symbol, want := range map[string]string{"SHORT": "long-term", "NAME": "does not match", "URL": "URL mismatch"} {
		r := got[symbol]
		if r.Status != "error" || r.Data != nil || !strings.Contains(r.Error, want) {
			t.Errorf("%s: %+v", symbol, r)
		}
	}
}

func TestReadResultsRejectsMissingRatingsAsCompanyError(t *testing.T) {
	jobs := []Job{{Symbol: "A", Name: "A"}, {Symbol: "B", Name: "B"}}
	input := lines(t, Result{Symbol: "A", Status: "verified", URL: testURL + "1"}, Result{Symbol: "B", Status: "unmatched"})
	got, n, err := collect(t, input, jobs)
	if err != nil || n != 2 || got["A"].Status != "error" || got["B"].Status != "unmatched" {
		t.Fatalf("n=%d err=%v got=%+v", n, err, got)
	}
}

func TestReadResultsAcceptsAccentedIssuerNameForTrackedCompany(t *testing.T) {
	lt := credit.IssuerRating{RatingType: "Foreign Currency LT", Rating: "AA", Outlook: "Stable"}
	got, _, err := collect(t, lines(t, verified("NSRGY", "7", "Nestle S.A.", lt)), []Job{{Symbol: "NSRGY", Name: "Nestlé S.A."}})
	if err != nil || got["NSRGY"].Status != "verified" {
		t.Fatalf("err=%v got=%+v", err, got["NSRGY"])
	}
}

func TestReadResultsAbortsOnProtocolViolations(t *testing.T) {
	jobs := []Job{{Symbol: "A", Name: "A"}, {Symbol: "B", Name: "B"}}
	for name, input := range map[string]string{
		"unknown symbol":   lines(t, Result{Symbol: "X", Status: "unmatched"}),
		"duplicate symbol": lines(t, Result{Symbol: "A", Status: "unmatched"}, Result{Symbol: "A", Status: "unmatched"}),
		"unknown status":   lines(t, Result{Symbol: "A", Status: "mystery"}),
		"malformed json":   "{not json}\n",
	} {
		if _, _, err := collect(t, input, jobs); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestReadResultsStopsWhenApplyFailsAndReportsPartialCount(t *testing.T) {
	jobs := []Job{{Symbol: "A", Name: "A"}, {Symbol: "B", Name: "B"}}
	boom := errors.New("boom")
	calls := 0
	n, err := readResults(strings.NewReader(lines(t, Result{Symbol: "A", Status: "unmatched"}, Result{Symbol: "B", Status: "unmatched"})), jobs, func(Result) error {
		calls++
		if calls == 2 {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestReadResultsReportsTruncatedOutputByCount(t *testing.T) {
	jobs := []Job{{Symbol: "A", Name: "A"}, {Symbol: "B", Name: "B"}}
	_, n, err := collect(t, lines(t, Result{Symbol: "A", Status: "unmatched"}), jobs)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestReadResultsKeepsOnlyValidCappedCandidatesForUnresolvedCompanies(t *testing.T) {
	var candidates []credit.Candidate
	for i := 1; i <= 14; i++ {
		candidates = append(candidates, credit.Candidate{Name: "Other", URL: testURL + strconv.Itoa(i)})
	}
	candidates = append([]credit.Candidate{{Name: "Evil", URL: "https://evil.test/x"}, {Name: "", URL: testURL + "99"}}, candidates...)
	jobs := []Job{{Symbol: "A", Name: "A"}, {Symbol: "B", Name: "B"}}
	got, _, err := collect(t, lines(t, Result{Symbol: "A", Status: "unmatched", Candidates: candidates}, Result{Symbol: "B", Status: "ambiguous", Candidates: candidates[:3]}), jobs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["A"].Candidates) != maxCandidates || got["A"].Candidates[0].URL != testURL+"1" {
		t.Fatalf("A: %+v", got["A"].Candidates)
	}
	if len(got["B"].Candidates) != 1 || got["B"].Candidates[0].URL != testURL+"1" {
		t.Fatalf("B: %+v", got["B"].Candidates)
	}
}

func fakeNode(t *testing.T, script string) *Client {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	path := filepath.Join(t.TempDir(), "fake.cjs")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Client{Config: Config{Node: node, Script: path}, timeout: 5 * time.Second, pacing: time.Second}
}

func TestRefreshReportsAccessExitCodeAsBlocked(t *testing.T) {
	c := fakeNode(t, "console.error('S&P login expired'); process.exitCode = 3;")
	err := c.Refresh(context.Background(), []Job{{Symbol: "A", Name: "A"}}, func(Result) error { return nil })
	if !errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "login expired") {
		t.Fatalf("err=%v", err)
	}
	c = fakeNode(t, "console.error('boom'); process.exitCode = 1;")
	err = c.Refresh(context.Background(), []Job{{Symbol: "A", Name: "A"}}, func(Result) error { return nil })
	if err == nil || errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
}

func TestRefreshAppliesStreamedResultsFromProcess(t *testing.T) {
	c := fakeNode(t, `let input = ''; process.stdin.on('data', d => input += d).on('end', () => { for (const job of JSON.parse(input).jobs) console.log(JSON.stringify({symbol: job.symbol, status: 'unmatched', checkedAt: new Date().toISOString()})); });`)
	var symbols []string
	err := c.Refresh(context.Background(), []Job{{Symbol: "A", Name: "A"}, {Symbol: "B", Name: "B"}}, func(r Result) error {
		symbols = append(symbols, r.Symbol)
		return nil
	})
	if err != nil || strings.Join(symbols, ",") != "A,B" {
		t.Fatalf("symbols=%v err=%v", symbols, err)
	}
}

func processAlive(t *testing.T, node string, pid string) bool {
	t.Helper()
	return exec.Command(node, "-e", "process.kill(Number(process.argv[1]), 0)", pid).Run() == nil
}

func TestRefreshAbortStopsTheBrowserProcessToo(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	c := fakeNode(t, `
const { spawn } = require('node:child_process');
const child = spawn(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], { stdio: 'ignore' });
require('node:fs').writeFileSync(process.env.CHILD_PID_FILE, String(child.pid));
process.on('SIGTERM', () => { child.kill(); process.exit(0); });
console.log('not json');
setInterval(() => {}, 1000);
`)
	t.Setenv("CHILD_PID_FILE", pidFile)
	if err := c.Refresh(context.Background(), []Job{{Symbol: "A", Name: "A"}}, func(Result) error { return nil }); err == nil {
		t.Fatal("expected protocol error")
	}
	blob, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid := strings.TrimSpace(string(blob))
	deadline := time.Now().Add(10 * time.Second)
	for processAlive(t, c.Config.Node, pid) {
		if time.Now().After(deadline) {
			exec.Command(c.Config.Node, "-e", "process.kill(Number(process.argv[1]))", pid).Run()
			t.Fatalf("child process %s survived cancellation", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
