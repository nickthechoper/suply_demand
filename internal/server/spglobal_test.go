package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"supplydemand/internal/credit"
	"supplydemand/internal/store"
)

func TestCompanySelectsDirectSPRatingWithDetailsAndManualOverride(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertSymbol("GRAB"); err != nil {
		t.Fatal(err)
	}
	url := "https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/682461"
	data := &credit.SPData{EntityName: "Grab Holdings Ltd.", EntityID: "682461", SourceURL: url, FetchedAt: time.Now().UTC(), Ratings: []credit.IssuerRating{{RatingType: "Foreign Currency LT", Rating: "BB", Outlook: "Positive"}}}
	if err := st.SetSPCredit("GRAB", data, credit.Discovery{Status: "verified", URL: url, CheckedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	srv := New(st, Config{Symbols: []string{"GRAB"}})
	read := func() creditDTO {
		t.Helper()
		rr := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rr, httptest.NewRequest("GET", "/api/company?symbol=GRAB", nil))
		if rr.Code != 200 {
			t.Fatalf("company status %d: %s", rr.Code, rr.Body.String())
		}
		var result struct {
			Credit creditDTO `json:"credit"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Credit
	}
	direct := read()
	if direct.Rating != "BB" || direct.Source != "S&P Global" || direct.Details == nil || direct.Details.EntityID != "682461" {
		t.Fatalf("direct credit: %+v", direct)
	}
	csv := filepath.Join(t.TempDir(), "credit.csv")
	if err := os.WriteFile(csv, []byte("Symbol,Rating,Outlook\nGRAB,BBB,Stable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.config.Credit = credit.NewSource(csv)
	manual := read()
	if manual.Rating != "BBB" || manual.Source != "manual" || manual.Details != nil {
		t.Fatalf("manual credit: %+v", manual)
	}
}
