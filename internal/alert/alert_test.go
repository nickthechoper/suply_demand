package alert

import (
	"path/filepath"
	"testing"
	"time"

	"supplydemand/internal/mail"
	"supplydemand/internal/store"
	"supplydemand/internal/yahoo"
	"supplydemand/internal/zones"
)

type fakeMailer struct {
	subjects []string
}

func (f *fakeMailer) Send(m mail.Message) error {
	f.subjects = append(f.subjects, m.Subject)
	return nil
}

func rangeBars(closes ...float64) []yahoo.Bar {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := make([]yahoo.Bar, len(closes))
	for i, c := range closes {
		bars[i] = yahoo.Bar{
			Time:   start.AddDate(0, 0, i),
			Open:   c,
			High:   c + 0.5,
			Low:    c - 0.5,
			Close:  c,
			Volume: 1000,
		}
	}
	return bars
}

func TestCheckAlertsOncePerVisit(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertSymbol("TEST"); err != nil {
		t.Fatal(err)
	}

	mailer := &fakeMailer{}
	checker := New(Config{
		Store:    st,
		Mailer:   mailer,
		To:       []string{"me@example.com"},
		Lookback: 500,
		Params:   zones.Params{ThresholdPct: 10, Resolution: 50},
		Depth:    0.25,
	})

	history := make([]float64, 0, 100)
	for i := 0; i < 100; i++ {
		history = append(history, 10+float64(i%20))
	}

	step := func(last float64, wantMails int) {
		t.Helper()
		bars := rangeBars(append(append([]float64{}, history...), last)...)
		if err := st.UpsertBars("TEST", bars); err != nil {
			t.Fatal(err)
		}
		checker.Check("TEST")
		if len(mailer.subjects) != wantMails {
			t.Fatalf("after close %.2f: got %d mails, want %d", last, len(mailer.subjects), wantMails)
		}
	}

	step(20, 0)
	step(9.6, 1)
	step(9.7, 1)
	step(30, 1)
	step(9.6, 2)
}

func TestEvaluateNoData(t *testing.T) {
	if _, ok := evaluate(nil, zones.Params{ThresholdPct: 10, Resolution: 50}, 0.25); ok {
		t.Fatal("expected no status without bars")
	}
}
