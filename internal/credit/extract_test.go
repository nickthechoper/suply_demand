package credit

import "testing"

func TestExtract(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		wantOK      bool
		wantRating  string
		wantOutlook string
	}{
		{
			name:        "sentence with outlook",
			text:        "Our senior notes are rated BBB- by S&P Global Ratings with a stable outlook, and Baa2 by Moody's.",
			wantOK:      true,
			wantRating:  "BBB-",
			wantOutlook: "Stable",
		},
		{
			name:        "agency first then rating",
			text:        "In July 2026, S&P Global Ratings lowered our long-term issuer credit rating to BBB- with a stable outlook.",
			wantOK:      true,
			wantRating:  "BBB-",
			wantOutlook: "Stable",
		},
		{
			name:       "downgrade phrase takes the new rating",
			text:       "S&P downgraded our rating from BBB to BBB-.",
			wantOK:     true,
			wantRating: "BBB-",
		},
		{
			name:        "table layout",
			text:        "Credit Ratings Moody's Baa1 S&P BBB+ Fitch BBB+ Outlook Stable",
			wantOK:      true,
			wantRating:  "BBB+",
			wantOutlook: "Stable",
		},
		{
			name:       "unicode minus",
			text:       "Standard & Poor's rated our debt BBB− in the quarter.",
			wantOK:     true,
			wantRating: "BBB-",
		},
		{
			name:        "plain single letter before agency",
			text:        "Our long-term debt is rated A by S&P and A2 by Moody's. The rating outlook is stable.",
			wantOK:      true,
			wantRating:  "A",
			wantOutlook: "Stable",
		},
		{
			name:        "plain single letter directly after rating word",
			text:        "S&P Global Ratings assigned a long-term issuer credit rating of A with a negative outlook.",
			wantOK:      true,
			wantRating:  "A",
			wantOutlook: "Negative",
		},
		{
			name:   "short-term ratings are ignored",
			text:   "Our commercial paper is rated A-2 by S&P and P-2 by Moody's.",
			wantOK: false,
		},
		{
			name:   "stock index mention is not a rating",
			text:   "The graph compares our stock performance with the S&P 500 Index. Class A common stock.",
			wantOK: false,
		},
		{
			name:   "moody's only",
			text:   "Moody's rated our notes Baa2 with a stable outlook.",
			wantOK: false,
		},
		{
			name:       "most frequent candidate wins",
			text:       "S&P rating of BBB+ was affirmed. Rated BBB+ by S&P. S&P rating BBB in 2019 historically.",
			wantOK:     true,
			wantRating: "BBB+",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Extract(tt.text)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (got %+v)", ok, tt.wantOK, got)
			}
			if !ok {
				return
			}
			if got.Rating != tt.wantRating {
				t.Errorf("rating = %q, want %q", got.Rating, tt.wantRating)
			}
			if got.Outlook != tt.wantOutlook {
				t.Errorf("outlook = %q, want %q", got.Outlook, tt.wantOutlook)
			}
			if got.Rank != RankOf(tt.wantRating) {
				t.Errorf("rank = %d, want %d", got.Rank, RankOf(tt.wantRating))
			}
		})
	}
}
