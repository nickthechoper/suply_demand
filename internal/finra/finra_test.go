package finra

import (
	"math"
	"testing"
	"time"
)

func date(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr(s string) *string { return &s }

func TestYieldToMaturityMatchesFinra(t *testing.T) {
	settle := date("2026-10-15")
	cases := []struct {
		price, coupon float64
		maturity      string
		want          float64
	}{
		{80.75, 9, "2031-02-01", 15.264155},
		{86.875, 9.25, "2030-06-01", 13.976959},
		{79.5, 9.625, "2032-07-15", 15.062444},
		{82, 9.75, "2031-10-01", 15.023662},
	}
	for _, c := range cases {
		got, ok := YieldToMaturity(c.price, c.coupon, date(c.maturity), settle)
		if !ok || math.Abs(got-c.want) > 0.05 {
			t.Errorf("YieldToMaturity(%v, %v, %s) = %.4f, %v; want %.4f", c.price, c.coupon, c.maturity, got, ok, c.want)
		}
	}
}

func TestYieldToMaturityAtPar(t *testing.T) {
	got, ok := YieldToMaturity(100, 6, date("2030-04-15"), date("2026-04-15"))
	if !ok || math.Abs(got-6) > 1e-6 {
		t.Errorf("par bond yield = %.6f, %v; want 6", got, ok)
	}
}

func TestConvertDedupesAndFillsYield(t *testing.T) {
	now := date("2026-10-15")
	bonds := convert([]record{
		{Symbol: "CRWV1", Coupon: ptr("9.0"), Maturity: ptr("2031-02-01"), Convertible: ptr("N"), Price: ptr("80.75"), Yield: ptr("15.26"), LastTrade: ptr("2026-10-14")},
		{Symbol: "CRWV2", Coupon: ptr("9.0"), Maturity: ptr("2031-02-01"), Convertible: ptr("N"), Price: ptr("81.88"), LastTrade: ptr("2026-10-15")},
		{Symbol: "CRWV3", Coupon: ptr("9.25"), Maturity: ptr("2030-06-01"), Convertible: ptr("N"), Price: ptr("86.875"), PriceChange: ptr("-1"), LastTrade: ptr("2026-10-15")},
		{Symbol: "CRWV4", Coupon: ptr("5"), Maturity: ptr("2025-01-01"), Convertible: ptr("N"), Price: ptr("100"), LastTrade: ptr("2024-12-01")},
	}, now)

	if len(bonds) != 2 {
		t.Fatalf("got %d bonds, want 2: %+v", len(bonds), bonds)
	}
	if bonds[0].Symbol != "CRWV3" || bonds[1].Symbol != "CRWV1" {
		t.Errorf("got order %s, %s; want CRWV3, CRWV1", bonds[0].Symbol, bonds[1].Symbol)
	}
	if bonds[0].YieldPct == nil || math.Abs(*bonds[0].YieldPct-13.98) > 0.05 {
		t.Errorf("computed yield = %v, want ~13.98", bonds[0].YieldPct)
	}
	if bonds[0].YieldChange == nil || *bonds[0].YieldChange <= 0 {
		t.Errorf("yield change = %v, want positive after a price drop", bonds[0].YieldChange)
	}
}

func TestMainIssuerDropsOtherCompaniesUnderSamePrefix(t *testing.T) {
	records := []record{
		{Symbol: "BDX1", Issuer: "BECTON DICKINSON & CO"},
		{Symbol: "BDX2", Issuer: "BECTON DICKINSON & CO"},
		{Symbol: "BDX3", Issuer: "EMBECTA CORP"},
		{Symbol: "F1", Issuer: "FORD MTR CO DEL"},
	}
	if got := mainIssuer(records); len(got) != 2 || got[0].Symbol != "BDX1" || got[1].Symbol != "BDX2" {
		t.Errorf("mainIssuer = %+v; want BDX1, BDX2", got)
	}

	ford := []record{
		{Symbol: "F.AA", Issuer: "FORD MTR CO DEL"},
		{Symbol: "F4", Issuer: "FORD MTR CR CO LLC MEDIUM TERM NTS BOOK ENTRY"},
		{Symbol: "F.GC", Issuer: "Ford Holdings Inc"},
	}
	if got := mainIssuer(ford); len(got) != 3 {
		t.Errorf("mainIssuer kept %d Ford bonds; want 3", len(got))
	}
}

func TestHeadlineSkipsConvertiblesStaleAndShortBonds(t *testing.T) {
	now := date("2026-10-15")
	y := func(v float64) *float64 { return &v }
	bonds := []Bond{
		{Symbol: "conv", Convertible: true, YieldPct: y(30), Maturity: "2031-01-01", LastTrade: "2026-10-15"},
		{Symbol: "stale", YieldPct: y(25), Maturity: "2031-01-01", LastTrade: "2026-08-01"},
		{Symbol: "short", YieldPct: y(20), Maturity: "2026-11-01", LastTrade: "2026-10-15"},
		{Symbol: "low", YieldPct: y(8), Maturity: "2030-01-01", LastTrade: "2026-10-15"},
		{Symbol: "high", YieldPct: y(15.3), Maturity: "2031-02-01", LastTrade: "2026-10-14"},
	}
	got, ok := Headline(bonds, now)
	if !ok || got.Symbol != "high" {
		t.Errorf("Headline = %q, %v; want high", got.Symbol, ok)
	}
	if _, ok := Headline(nil, now); ok {
		t.Error("Headline(nil) reported a bond")
	}
}

func TestSameCompany(t *testing.T) {
	cases := []struct {
		company, issuer string
		want            bool
	}{
		{"Globus Medical, Inc.", "GENERACION MEDITERRANEA S A / CENT TERMICA ROCA S A", false},
		{"CoreWeave, Inc.", "COREWEAVE INC", true},
		{"Coca-Cola Company (The)", "COCA COLA CO", true},
		{"Home Depot, Inc. (The)", "HOME DEPOT INC", true},
		{"McDonald's Corporation", "MCDONALDS CORP MEDIUM TERM NTS BOOK ENTRY", true},
		{"AT&T Inc.", "AT&T INC", true},
		{"International Business Machines", "INTERNATIONAL BUSINESS MACHS CORP", true},
		{"Walmart Inc.", "WAL-MART STORES INC", true},
		{"Ford Motor Company", "FORD MTR CR CO LLC", true},
		{"", "ANYTHING CORP", true},
	}
	for _, c := range cases {
		if got := SameCompany(c.company, c.issuer); got != c.want {
			t.Errorf("SameCompany(%q, %q) = %v, want %v", c.company, c.issuer, got, c.want)
		}
	}
}
