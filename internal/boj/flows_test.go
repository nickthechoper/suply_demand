package boj

import (
	"fmt"
	"strings"
	"testing"
)

type series struct {
	code, label string
}

var fixtureSeries = []series{
	{seriesCode + "AS", "Asia"},
	{seriesCode + "CN", "P.R. China"},
	{seriesCode + "NA", "North America"},
	{seriesCode + "US", "U.S.A."},
	{seriesCode + "EP", "Europe"},
	{seriesCode + "DE", "Germany"},
	{seriesCode + "FR", "France"},
	{seriesCode + "NL", "Netherlands"},
	{seriesCode + "IT", "Italy"},
	{seriesCode + "BE", "Belgium"},
	{seriesCode + "LX", "Luxembourg"},
	{seriesCode + "CH", "Switzerland"},
	{seriesCode + "ES", "Spain"},
	{seriesCode + "IE", "Ireland"},
	{seriesCode + "AT", "Austria"},
	{seriesCode + "FI", "Finland"},
	{seriesCode + "PT", "Portugal"},
	{seriesCode + "GR", "Greece"},
	{seriesCode + "EU", "EU"},
}

func fixture(months int, value func(code string, i int) string) string {
	var b strings.Builder
	b.WriteString(",,,,201312")
	for i := 0; i < months; i++ {
		fmt.Fprintf(&b, ",%d%02d", 2014+i/12, i%12+1)
	}
	b.WriteString(",209901\n")
	fmt.Fprintf(&b, `%s,"BPM6","Portfolio Investment Assets/Long-term debt securities/Net","100 million Yen",`, totalCode)
	for i := 0; i < months; i++ {
		b.WriteString("," + value("ALL", i))
	}
	b.WriteString(",\n")
	b.WriteString(`BPPI6D3P9US,"BPM6","Portfolio Investment Assets/Long-term debt securities/U.S.A./Acquisition","100 million Yen",,1` + strings.Repeat(",1", months-1) + ",\n")
	for _, s := range fixtureSeries {
		fmt.Fprintf(&b, `%s,"BPM6","Portfolio Investment Assets/Long-term debt securities/%s/Net","100 million Yen",`, s.code, s.label)
		for i := 0; i < months; i++ {
			b.WriteString("," + value(strings.TrimPrefix(s.code, seriesCode), i))
		}
		b.WriteString(",\n")
	}
	return b.String()
}

func rowByCode(f *Flows, code string) *Row {
	for i := range f.Rows {
		if f.Rows[i].Code == code {
			return &f.Rows[i]
		}
	}
	return nil
}

func TestParseCSVTrimsEmptyMonthsAndConvertsToBillions(t *testing.T) {
	f, err := ParseCSV(strings.NewReader(fixture(14, func(string, int) string { return "1234" })))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Months) != 14 || f.Months[0] != "2014-01" || f.Months[13] != "2015-02" {
		t.Fatalf("months = %v", f.Months)
	}
	us := rowByCode(f, "US")
	if us == nil || len(us.Values) != 14 || *us.Values[0] != 123.4 {
		t.Fatalf("US row = %+v", us)
	}
}

func TestParseCSVClassifiesRowsAndAssignsRegions(t *testing.T) {
	f, err := ParseCSV(strings.NewReader(fixture(2, func(string, int) string { return "10" })))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		code, name, kind, region string
	}{
		{"ALL", "All countries", KindTotal, ""},
		{"AS", "Asia", KindRegion, ""},
		{"CN", "China", KindCountry, "Asia"},
		{"US", "United States", KindCountry, "North America"},
		{"CH", "Switzerland", KindCountry, "Europe"},
		{"EU", "EU", KindGroup, ""},
		{"EA", "Euro area (12)", KindGroup, ""},
	}
	for _, c := range cases {
		r := rowByCode(f, c.code)
		if r == nil {
			t.Errorf("missing row %s", c.code)
			continue
		}
		if r.Name != c.name || r.Kind != c.kind || r.Region != c.region {
			t.Errorf("%s = {%q %q %q}, want {%q %q %q}", c.code, r.Name, r.Kind, r.Region, c.name, c.kind, c.region)
		}
	}
	if rowByCode(f, "P9US") != nil || len(f.Rows) != len(fixtureSeries)+2 {
		t.Errorf("got %d rows, want only net series plus total and euro area", len(f.Rows))
	}
}

func TestEuroAreaSumsMembersAndLeavesGaps(t *testing.T) {
	f, err := ParseCSV(strings.NewReader(fixture(3, func(code string, i int) string {
		if code == "IT" && i == 1 {
			return ""
		}
		if code == "CH" {
			return "9990"
		}
		return "100"
	})))
	if err != nil {
		t.Fatal(err)
	}
	ea := rowByCode(f, "EA")
	if got := *ea.Values[0]; got != 120 {
		t.Errorf("euro area = %v, want 120 (12 members x 10bn, Switzerland excluded)", got)
	}
	if ea.Values[1] != nil {
		t.Error("euro area should be empty when a member is missing that month")
	}
}
