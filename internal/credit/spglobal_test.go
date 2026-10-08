package credit

import (
	"testing"
	"time"
)

func TestSPNormalizesForeignLongTermBeforeLocalAndShortTerm(t *testing.T) {
	d := SPData{EntityName: "Grab Holdings Ltd.", EntityID: "682461", SourceURL: "https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/682461", FetchedAt: time.Now().UTC(), Ratings: []IssuerRating{
		{RatingType: "Local Currency LT", Rating: "BBB", Outlook: "Stable"},
		{RatingType: "Foreign Currency ST", Rating: "A-1"},
		{RatingType: "Foreign Currency LT", Rating: "BB", Outlook: "Positive"},
	}}
	if err := d.Normalize(); err != nil {
		t.Fatal(err)
	}
	if d.Rating != "BB" || d.Outlook != "Positive" || d.AsRating().Origin() != "S&P Global" {
		t.Fatalf("rating: %+v", d)
	}
	d.EntityID = "123"
	if d.Normalize() == nil {
		t.Fatal("accepted mismatched entity ID")
	}
}

func TestSPNormalizePrefersRatedLongTermOverNotRatedOrWithdrawn(t *testing.T) {
	base := SPData{EntityName: "Grab Holdings Ltd.", EntityID: "682461", SourceURL: "https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/682461", FetchedAt: time.Now().UTC()}
	for name, tc := range map[string]struct {
		ratings []IssuerRating
		want    string
	}{
		"foreign NR yields to local rated": {[]IssuerRating{{RatingType: "Foreign Currency LT", Rating: "NR"}, {RatingType: "Local Currency LT", Rating: "A", Outlook: "Stable"}}, "A"},
		"foreign WR yields to local rated": {[]IssuerRating{{RatingType: "Local Currency LT", Rating: "BBB-", Outlook: "Negative"}, {RatingType: "Foreign Currency LT", Rating: "WR"}}, "BBB-"},
		"foreign rated still wins":         {[]IssuerRating{{RatingType: "Local Currency LT", Rating: "AA"}, {RatingType: "Foreign Currency LT", Rating: "A"}}, "A"},
		"only NR is accepted":              {[]IssuerRating{{RatingType: "Local Currency LT", Rating: "NR"}}, "NR"},
		"foreign NR before local WR":       {[]IssuerRating{{RatingType: "Local Currency LT", Rating: "WR"}, {RatingType: "Foreign Currency LT", Rating: "NR"}}, "NR"},
	} {
		d := base
		d.Ratings = tc.ratings
		if err := d.Normalize(); err != nil || d.Rating != tc.want {
			t.Errorf("%s: rating=%q err=%v", name, d.Rating, err)
		}
	}
	d := base
	d.Ratings = []IssuerRating{{RatingType: "Foreign Currency ST", Rating: "A-1"}}
	if d.Normalize() == nil {
		t.Fatal("accepted short-term only")
	}
}
