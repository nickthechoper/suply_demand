package alert

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"supplydemand/internal/finra"
	"supplydemand/internal/store"
)

var bondNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func yieldSeries(last time.Time, yields ...float64) []finra.DailyYield {
	out := make([]finra.DailyYield, len(yields))
	for i, y := range yields {
		date := last.AddDate(0, 0, i-len(yields)+1)
		out[i] = finra.DailyYield{Date: date.Format(time.DateOnly), YieldPct: y, Trades: 5}
	}
	return out
}

func choppy(days int, base, swing float64) []float64 {
	out := make([]float64, days)
	for i := range out {
		out[i] = base
		if i%2 == 1 {
			out[i] += swing
		}
	}
	return out
}

func newBondChecker(t *testing.T, history *[]finra.DailyYield) (*BondChecker, *fakeMailer) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.UpsertSymbol("TEST"); err != nil {
		t.Fatal(err)
	}
	coupon, yield := 9.0, 12.0
	if err := st.SetBonds("TEST", []finra.Bond{{
		Symbol:    "TEST1",
		CouponPct: &coupon,
		Maturity:  "2031-02-01",
		YieldPct:  &yield,
		LastTrade: "2026-10-14",
	}}); err != nil {
		t.Fatal(err)
	}

	mailer := &fakeMailer{}
	checker := NewBond(BondConfig{
		Store:       st,
		Mailer:      mailer,
		To:          []string{"me@example.com"},
		Sigma:       3,
		MinMoveBp:   50,
		HistoryDays: 180,
		History:     func(finra.Bond, int) ([]finra.DailyYield, error) { return *history, nil },
		Now:         func() time.Time { return bondNow },
	})
	return checker, mailer
}

func TestBondCheckAlertsOncePerSharpMove(t *testing.T) {
	var history []finra.DailyYield
	checker, mailer := newBondChecker(t, &history)
	base := choppy(60, 12, 0.05)
	lastDay := bondNow.AddDate(0, 0, -1)

	step := func(latest float64, wantMails int, wantInSubject string) {
		t.Helper()
		history = yieldSeries(lastDay, append(append([]float64{}, base...), latest)...)
		checker.Check("TEST")
		if len(mailer.subjects) != wantMails {
			t.Fatalf("after latest yield %.2f: got %d mails %q, want %d", latest, len(mailer.subjects), mailer.subjects, wantMails)
		}
		if wantInSubject != "" && !strings.Contains(mailer.subjects[len(mailer.subjects)-1], wantInSubject) {
			t.Fatalf("subject %q does not mention %q", mailer.subjects[len(mailer.subjects)-1], wantInSubject)
		}
	}

	step(12.05, 0, "")
	step(13.50, 1, "jumped +145 bp")
	step(13.50, 1, "")
	step(12.05, 1, "")
	step(13.60, 2, "jumped")
	step(10.40, 3, "dropped")
}

func TestBondCheckIgnoresSmallAndStaleMoves(t *testing.T) {
	var history []finra.DailyYield
	checker, mailer := newBondChecker(t, &history)

	history = yieldSeries(bondNow.AddDate(0, 0, -1), append(choppy(60, 5, 0.01), 5.3)...)
	checker.Check("TEST")
	if len(mailer.subjects) != 0 {
		t.Fatalf("a 30 bp move below the 50 bp floor sent %q", mailer.subjects)
	}

	history = yieldSeries(bondNow.AddDate(0, 0, -10), append(choppy(60, 12, 0.05), 14)...)
	checker.Check("TEST")
	if len(mailer.subjects) != 0 {
		t.Fatalf("a move from 10 days ago sent %q", mailer.subjects)
	}
}

func TestSharpestMoveCatchesMultiDayRamp(t *testing.T) {
	yields := append(choppy(60, 12, 0.10), 12.15, 12.30, 12.45, 12.60, 12.75)
	move, ok := sharpestMove(yieldSeries(bondNow, yields...), 3, 50)
	if !ok {
		t.Fatal("a 75 bp climb over 5 days was not flagged")
	}
	if move.Horizon != 5 || move.ChangeBp < 60 {
		t.Errorf("got horizon %d change %.0f bp; want the 5-day move", move.Horizon, move.ChangeBp)
	}

	if _, ok := sharpestMove(yieldSeries(bondNow, 12, 13, 14), 3, 50); ok {
		t.Error("flagged a move with too little history")
	}
}
