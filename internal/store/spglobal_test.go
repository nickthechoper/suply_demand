package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"supplydemand/internal/credit"
	"supplydemand/internal/yahoo"
)

func TestCreditSurvivesYahooRefreshAndKeepsIndependentTimestamp(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertSymbol("GRAB"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSymbolName("GRAB", "Grab Holdings Limited"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCompanyData("GRAB", yahoo.CompanyData{Profile: yahoo.CompanyProfile{Sector: "Technology"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE company_data SET fetched_at = 123 WHERE symbol = 'GRAB'`); err != nil {
		t.Fatal(err)
	}
	url := "https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/682461"
	data := &credit.SPData{EntityName: "Grab Holdings Ltd.", EntityID: "682461", SourceURL: url, FetchedAt: time.Now().UTC(), Ratings: []credit.IssuerRating{{RatingType: "Foreign Currency LT", Rating: "BB", Outlook: "Positive"}}}
	state := credit.Discovery{Status: "verified", URL: url, Name: data.EntityName, CheckedAt: time.Now().UTC()}
	if err := st.SetSPCredit("GRAB", data, state); err != nil {
		t.Fatal(err)
	}
	fetchState := credit.FetchState{AttemptedAt: time.Now().UTC(), Failures: 2, RetryAt: time.Now().Add(time.Hour), LastError: "temporary error"}
	if err := st.SetSPFetchState("GRAB", fetchState); err != nil {
		t.Fatal(err)
	}
	got, fetchedAt, ok, err := st.CompanyData("GRAB")
	if err != nil || !ok {
		t.Fatalf("company: %v %v", ok, err)
	}
	if fetchedAt.Unix() != 123 || got.Profile.Sector != "Technology" || got.Credit.Rating != "BB" {
		t.Fatalf("credit merge: %+v %v", got, fetchedAt)
	}
	if err := st.SetCompanyData("GRAB", yahoo.CompanyData{Profile: yahoo.CompanyProfile{Sector: "Updated"}}); err != nil {
		t.Fatal(err)
	}
	got, _, _, err = st.CompanyData("GRAB")
	if err != nil {
		t.Fatal(err)
	}
	if got.Credit == nil || got.Credit.Rating != "BB" || got.CreditDiscovery.URL != url || got.Profile.Sector != "Updated" {
		t.Fatalf("Yahoo overwrote credit: %+v", got)
	}
	if got.CreditFetch == nil || got.CreditFetch.Failures != 2 || !got.CreditFetch.AttemptedAt.Equal(fetchState.AttemptedAt) || !got.CreditFetch.RetryAt.Equal(fetchState.RetryAt) {
		t.Fatalf("Yahoo overwrote fetch progress: %+v", got.CreditFetch)
	}
	if err := st.SetSPCredit("GRAB", nil, credit.Discovery{Status: "unmatched", CheckedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	got, _, _, _ = st.CompanyData("GRAB")
	if got.Credit == nil || got.Credit.Rating != "BB" {
		t.Fatal("unmatched discovery erased last good credit")
	}
	ratings, err := st.SPCreditRatings()
	if err != nil || ratings["GRAB"].Rank != 11 {
		t.Fatalf("ratings: %v %v", ratings, err)
	}
}

func TestCreditOnlyRowDoesNotMarkYahooDataFresh(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertSymbol("UNRATED"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSPCredit("UNRATED", nil, credit.Discovery{Status: "unmatched", CheckedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	var blob string
	var timestamp int64
	if err := st.db.QueryRow(`SELECT data, fetched_at FROM company_data WHERE symbol = 'UNRATED'`).Scan(&blob, &timestamp); err != nil {
		t.Fatal(err)
	}
	if timestamp != 0 || !json.Valid([]byte(blob)) {
		t.Fatalf("credit-only row: %s %d", blob, timestamp)
	}
	times, err := st.ListFetchTimes()
	if err != nil || len(times) != 1 || !times[0].CompanyData.IsZero() {
		t.Fatalf("Yahoo fetch time: %v %v", times, err)
	}
}

func TestSPDiscoveryCandidatesPersistAndRatingsQueryMatchesFullDecode(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, symbol := range []string{"RATED", "UNRATED", "EMPTY"} {
		if err := st.UpsertSymbol(symbol); err != nil {
			t.Fatal(err)
		}
	}
	candidates := []credit.Candidate{{Name: "Unrated Holdings", URL: "https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/5"}}
	if err := st.SetSPCredit("UNRATED", nil, credit.Discovery{Status: "ambiguous", CheckedAt: time.Now().UTC(), Candidates: candidates}); err != nil {
		t.Fatal(err)
	}
	url := "https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/7"
	data := &credit.SPData{EntityName: "Rated Inc", EntityID: "7", SourceURL: url, FetchedAt: time.Now().UTC(), Ratings: []credit.IssuerRating{{RatingType: "Foreign Currency LT", Rating: "A-"}}}
	if err := st.SetSPCredit("RATED", data, credit.Discovery{Status: "verified", URL: url, Name: "Rated Inc", CheckedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSPFetchState("EMPTY", credit.FetchState{AttemptedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	companies, err := st.SPCompanies()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range companies {
		switch c.Symbol {
		case "UNRATED":
			if len(c.Discovery.Candidates) != 1 || c.Discovery.Candidates[0] != candidates[0] || c.Credit != nil {
				t.Fatalf("candidates: %+v", c)
			}
		case "RATED":
			if len(c.Discovery.Candidates) != 0 {
				t.Fatalf("verified discovery kept candidates: %+v", c.Discovery)
			}
		}
	}
	ratings, err := st.SPCreditRatings()
	if err != nil || len(ratings) != 1 {
		t.Fatalf("ratings: %v %v", ratings, err)
	}
	if got := ratings["RATED"]; got.Rating != "A-" || got.Outlook != "" || got.Rank != 16 || got.Source != "S&P Global" {
		t.Fatalf("rating: %+v", got)
	}
}
