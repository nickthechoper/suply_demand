package credit

import (
	"fmt"
	"net/url"
	"regexp"
	"time"
)

type IssuerRating struct {
	RatingType            string `json:"ratingType"`
	Rating                string `json:"rating"`
	RatingDate            string `json:"ratingDate"`
	LastReviewDate        string `json:"lastReviewDate"`
	RegulatoryIdentifiers string `json:"regulatoryIdentifiers"`
	Outlook               string `json:"outlook"`
	OutlookDate           string `json:"outlookDate"`
}

type SPData struct {
	EntityName string         `json:"entityName"`
	EntityID   string         `json:"entityId"`
	SourceURL  string         `json:"sourceUrl"`
	FetchedAt  time.Time      `json:"fetchedAt"`
	Ratings    []IssuerRating `json:"ratings"`
	Rating     string         `json:"rating"`
	Outlook    string         `json:"outlook"`
	Source     string         `json:"source"`
}

type Candidate struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Discovery struct {
	Status     string      `json:"status"`
	URL        string      `json:"url,omitempty"`
	Name       string      `json:"name,omitempty"`
	CheckedAt  time.Time   `json:"checkedAt"`
	Candidates []Candidate `json:"candidates,omitempty"`
}

type FetchState struct {
	AttemptedAt time.Time `json:"attemptedAt"`
	RetryAt     time.Time `json:"retryAt,omitempty"`
	Failures    int       `json:"failures"`
	LastError   string    `json:"lastError,omitempty"`
}

var issuerPath = regexp.MustCompile(`^/ratings/en/regulatory/(?:entity/)?org-details/sectorCode/[A-Z]+/entityId/([0-9]+)/?$`)

func EntityID(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "www.spglobal.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid S&P issuer URL")
	}
	m := issuerPath.FindStringSubmatch(u.Path)
	if m == nil {
		return "", fmt.Errorf("invalid S&P issuer URL path")
	}
	return m[1], nil
}

func (d *SPData) Normalize() error {
	id, err := EntityID(d.SourceURL)
	if err != nil || id != d.EntityID || d.EntityName == "" || d.FetchedAt.IsZero() || d.FetchedAt.After(time.Now().Add(5*time.Minute)) {
		return fmt.Errorf("invalid S&P issuer identity or fetch timestamp")
	}
	for _, accept := range []func(string) bool{
		func(rating string) bool { return RankOf(rating) > 0 },
		func(rating string) bool { return rating == "NR" || rating == "WR" },
	} {
		for _, kind := range []string{"Foreign Currency LT", "Local Currency LT"} {
			for _, r := range d.Ratings {
				if r.RatingType == kind && accept(r.Rating) {
					d.Rating, d.Outlook, d.Source = r.Rating, r.Outlook, "S&P Global"
					return nil
				}
			}
		}
	}
	return fmt.Errorf("no supported long-term issuer rating found")
}

func (d SPData) AsRating() Rating {
	return Rating{Rating: d.Rating, Outlook: d.Outlook, Rank: RankOf(d.Rating), Source: "S&P Global"}
}
