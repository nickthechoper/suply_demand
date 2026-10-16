package boj

import (
	"fmt"
	"strings"
	"testing"
)

func fixture(months int, value func(code string, i int) string) string {
	var b strings.Builder
	b.WriteString(",,,,201312")
	for i := 0; i < months; i++ {
		fmt.Fprintf(&b, ",%d%02d", 2014+i/12, i%12+1)
	}
	b.WriteString(",209901\n")
	b.WriteString(`BPPI6D3N9XX,"BPM6","Other",unit,,1` + strings.Repeat(",1", months-1) + ",\n")
	for _, c := range append(append([]Country{US}, Featured...), OtherEuroArea...) {
		fmt.Fprintf(&b, `%s%s,"BPM6","%s/Net","100 million Yen",`, seriesCode, c.Code, c.Name)
		for i := 0; i < months; i++ {
			b.WriteString("," + value(c.Code, i))
		}
		b.WriteString(",\n")
	}
	return b.String()
}

func TestParseCSVTrimsEmptyColumns(t *testing.T) {
	months, data, err := ParseCSV(strings.NewReader(fixture(14, func(string, int) string { return "1000" })))
	if err != nil {
		t.Fatal(err)
	}
	if len(months) != 14 || months[0] != "201401" || months[13] != "201502" {
		t.Fatalf("months = %v", months)
	}
	if len(data) != 13 || len(data["DE"]) != 14 {
		t.Fatalf("got %d series, DE has %d values", len(data), len(data["DE"]))
	}
}

func TestBuildRollingSumsAndEuroAreaTotal(t *testing.T) {
	csv := fixture(14, func(code string, i int) string {
		switch code {
		case "US":
			return "-500"
		case "DE":
			return fmt.Sprint(100 * (i + 1))
		}
		return "250"
	})
	months, data, err := ParseCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	f := Build(months, data)

	if len(f.Months) != 3 || f.Months[0] != "2014-12" || f.Months[2] != "2015-02" {
		t.Fatalf("months = %v", f.Months)
	}
	if got := *f.US.Values[0]; got != -6 {
		t.Errorf("US 12m sum = %v, want -6", got)
	}
	if got := *f.EuroArea[0].Values[0]; got != 7.8 {
		t.Errorf("DE first 12m sum = %v, want 7.8", got)
	}
	if got := *f.EuroArea[0].Values[2]; got != 10.2 {
		t.Errorf("DE last 12m sum = %v, want 10.2", got)
	}
	other := f.EuroArea[len(f.EuroArea)-1]
	if other.Key != "OTHER" || *other.Values[0] != 21 {
		t.Errorf("other EA = %s %v, want OTHER 21", other.Key, *other.Values[0])
	}
	if got := *f.TotalEA.Values[0]; got != 7.8+12+21 {
		t.Errorf("total EA = %v, want %v", got, 7.8+12+21)
	}
}

func TestBuildLeavesGapsWhenMonthMissing(t *testing.T) {
	csv := fixture(13, func(code string, i int) string {
		if code == "IT" && i == 5 {
			return ""
		}
		return "100"
	})
	months, data, err := ParseCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	f := Build(months, data)
	if f.EuroArea[3].Values[0] != nil || f.TotalEA.Values[0] != nil {
		t.Error("expected nil when a month in the window is missing")
	}
	if f.EuroArea[0].Values[0] == nil {
		t.Error("DE should be unaffected by an IT gap")
	}
}
