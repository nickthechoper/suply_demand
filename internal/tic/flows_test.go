package tic

import (
	"strings"
	"testing"
)

const fixture = "Table 4: U.S. Purchases and Sales\tx\n" +
	"Millions of dollars\n" +
	"Country\tCountry Code\tDate\tUS Govt Sale\tUS Corp Sale\tUS Govt Pur\tUS Corp Pur\n" +
	"country\tcountry_code\tdate\tus_lt_govt_bond_sale\tus_lt_corp_bond_sale\tus_lt_govt_bond_pur\tus_lt_corp_bond_pur\n" +
	"Turkey\t12807\t2026-08\t100\t50\t300\t81\n" +
	"Turkey\t12807\t2026-07\t10\t0\t5\t0\n" +
	"Monaco\t12009\t2026-08\tn.a.\t20\tn.a.\t70\n" +
	"Memo: Euro Area\t16713\t2026-08\t1000\t0\t400\t0\n" +
	"Total Europe\t19992\t2026-08\t\"1,000\"\t0\t\"3,500\"\t0\n" +
	"Canada\t29998\t2026-08\t0\t0\t900\t0\n" +
	"Cayman Islands\t36137\t2026-08\t0\t0\t0\t10\n" +
	"China, Mainland\t41408\t2026-08\t0\t0\t0\t0\n" +
	"All Countries\t69995\t2026-08\t1\t1\t1\t1\n" +
	"Grand Total\t99996\t2026-08\t0\t0\t7000\t0\n" +
	"Of Which: Foreign Official\t99990\t2026-08\t0\t0\t1\t0\n" +
	"Of Which: Fund Shares\t84018\t2026-08\tn.a.\tn.a.\tn.a.\tn.a.\n" +
	"1/ Footnote text\n"

func rowByCode(f *Flows, code string) *Row {
	for i := range f.Rows {
		if f.Rows[i].Code == code {
			return &f.Rows[i]
		}
	}
	return nil
}

func TestParseComputesNetPurchasesInBillions(t *testing.T) {
	f, err := Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Months) != 2 || f.Months[0] != "2026-07" || f.Months[1] != "2026-08" {
		t.Fatalf("months = %v", f.Months)
	}
	tr := rowByCode(f, "12807")
	if tr == nil || tr.Name != "Turkey" || tr.Region != "Europe" || tr.Kind != KindCountry {
		t.Fatalf("Turkey row = %+v", tr)
	}
	if got := *tr.Values[1]; got != 0.2 {
		t.Errorf("Turkey Aug = %v, want 0.2 ($231m bought - $150m sold)", got)
	}
	if got := *tr.Values[0]; got != 0 {
		t.Errorf("Turkey Jul = %v, want 0 (-$5m rounds to 0.0bn)", got)
	}
	if got := *rowByCode(f, "12009").Values[1]; got != 0.1 {
		t.Errorf("Monaco = %v, want 0.1 from corporate bonds when government is n.a.", got)
	}
	if got := *rowByCode(f, "19992").Values[1]; got != 2.5 {
		t.Errorf("Europe = %v, want 2.5 with thousands separators parsed", got)
	}
}

func TestParseClassifiesRows(t *testing.T) {
	f, err := Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		code, name, kind, region string
	}{
		{"99996", "All countries", KindTotal, ""},
		{"19992", "Europe", KindRegion, ""},
		{"16713", "Euro area", KindGroup, ""},
		{"29998", "Canada", KindCountry, ""},
		{"29998R", "Canada", KindRegion, ""},
		{"36137", "Cayman Islands", KindCountry, "Caribbean"},
		{"41408", "China", KindCountry, "Asia"},
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
	for _, skipped := range []string{"69995", "99990", "84018"} {
		if rowByCode(f, skipped) != nil {
			t.Errorf("row %s should be skipped", skipped)
		}
	}
	if rowByCode(f, "12807").Values[0] == nil || rowByCode(f, "41408").Values[0] != nil {
		t.Error("missing months should be nil only where a row has no data")
	}
}

func TestParseRejectsFileWithoutHeader(t *testing.T) {
	if _, err := Parse(strings.NewReader("Turkey\t12807\t2026-08\t1\t1\t1\t1\n")); err == nil {
		t.Fatal("expected an error without the column header row")
	}
}
