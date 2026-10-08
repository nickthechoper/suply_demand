package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"supplydemand/internal/credit"
	"supplydemand/internal/store"
)

func TestPrintCandidatesListsOnlyUnresolvedCompaniesWithCandidates(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, symbol := range []string{"AMB", "NONE"} {
		if err := st.UpsertSymbol(symbol); err != nil {
			t.Fatal(err)
		}
		if err := st.SetSymbolName(symbol, symbol+" Corp"); err != nil {
			t.Fatal(err)
		}
	}
	url := "https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/9"
	if err := st.SetSPCredit("AMB", nil, credit.Discovery{Status: "ambiguous", CheckedAt: time.Now().UTC(), Candidates: []credit.Candidate{{Name: "Amb Holdings", URL: url}}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSPCredit("NONE", nil, credit.Discovery{Status: "unmatched", CheckedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := printCandidates(&out, st); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "AMB (AMB Corp) ambiguous\n  Amb Holdings  "+url+"\n") || strings.Contains(got, "NONE") {
		t.Fatalf("output:\n%s", got)
	}
}
