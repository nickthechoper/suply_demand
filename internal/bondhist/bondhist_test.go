package bondhist

import (
	"path/filepath"
	"testing"
	"time"

	"supplydemand/internal/finra"
	"supplydemand/internal/store"
)

func TestSyncBackfillsThenTopsUp(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var froms []string
	fetch := func(points ...finra.DailyYield) func(finra.Bond, time.Time) ([]finra.DailyYield, error) {
		return func(_ finra.Bond, from time.Time) ([]finra.DailyYield, error) {
			froms = append(froms, from.Format(time.DateOnly))
			return points, nil
		}
	}
	bond := finra.Bond{Symbol: "TEST1"}
	now := time.Now().UTC()

	history, err := sync(st, bond, now, fetch(
		finra.DailyYield{Date: "2026-10-13", YieldPct: 12, Trades: 3},
		finra.DailyYield{Date: "2026-10-14", YieldPct: 13, Trades: 4},
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || froms[0] != now.AddDate(0, 0, -BackfillDays).Format(time.DateOnly) {
		t.Fatalf("backfill: got %d days from %v; want 2 days from %d days back", len(history), froms, BackfillDays)
	}

	if _, err := sync(st, bond, now.Add(time.Minute), fetch()); err != nil {
		t.Fatal(err)
	}
	if len(froms) != 1 {
		t.Fatalf("fetched again a minute later: %v", froms)
	}

	history, err = sync(st, bond, now.Add(time.Hour), fetch(
		finra.DailyYield{Date: "2026-10-14", YieldPct: 13.5, Trades: 9},
		finra.DailyYield{Date: "2026-10-15", YieldPct: 15, Trades: 6},
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(froms) != 2 || froms[1] != now.AddDate(0, 0, -overlapDays).Format(time.DateOnly) {
		t.Fatalf("top-up fetched from %v; want %d days before the last fetch", froms, overlapDays)
	}
	if len(history) != 3 || history[1].YieldPct != 13.5 || history[2].Date != "2026-10-15" {
		t.Fatalf("after top-up got %+v", history)
	}

	if got := Since(history, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)); len(got) != 2 {
		t.Errorf("Since kept %d days, want 2", len(got))
	}
}

func TestWeeklyChange(t *testing.T) {
	points := []finra.DailyYield{
		{Date: "2026-10-30", YieldPct: 9},
		{Date: "2026-11-02", YieldPct: 10},
		{Date: "2026-11-05", YieldPct: 10.5},
		{Date: "2026-11-09", YieldPct: 11.25},
	}
	got := WeeklyChange(points)
	if got == nil || *got != 1.25 {
		t.Fatalf("got %v; want 1.25 (vs 2026-11-02)", got)
	}
	if WeeklyChange(points[2:]) != nil {
		t.Fatal("want nil when no point is a week old")
	}
}
