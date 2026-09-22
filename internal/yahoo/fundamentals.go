package yahoo

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Fundamentals holds a handful of per-company metrics from Yahoo Finance's
// quoteSummary API. Fields are nil when Yahoo doesn't report a value for
// that symbol (e.g. no analyst PEG estimate).
type Fundamentals struct {
	PERatio          *float64 // trailing P/E
	PEGRatio         *float64
	RevenueGrowthPct *float64 // YoY revenue growth, as a percentage
	NetMarginPct     *float64 // trailing net profit margin, as a percentage
}

type fundamentalsResponse struct {
	QuoteSummary struct {
		Result []struct {
			SummaryDetail struct {
				TrailingPE quoteSummaryFloat `json:"trailingPE"`
			} `json:"summaryDetail"`
			DefaultKeyStatistics struct {
				PegRatio quoteSummaryFloat `json:"pegRatio"`
			} `json:"defaultKeyStatistics"`
			FinancialData struct {
				RevenueGrowth quoteSummaryFloat `json:"revenueGrowth"`
				ProfitMargins quoteSummaryFloat `json:"profitMargins"`
			} `json:"financialData"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteSummary"`
}

// FetchFundamentals retrieves P/E, PEG, revenue growth, and net margin for
// symbol from Yahoo Finance's quoteSummary API.
func FetchFundamentals(symbol string) (Fundamentals, error) {
	client, crumb, err := newAuthedClient()
	if err != nil {
		return Fundamentals{}, err
	}

	dataURL := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=defaultKeyStatistics,financialData,summaryDetail&crumb=%s",
		symbol, url.QueryEscape(crumb),
	)
	body, err := get(client, dataURL)
	if err != nil {
		return Fundamentals{}, fmt.Errorf("fetching yahoo fundamentals: %w", err)
	}

	var parsed fundamentalsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Fundamentals{}, fmt.Errorf("decoding yahoo quoteSummary response: %w", err)
	}
	if parsed.QuoteSummary.Error != nil {
		return Fundamentals{}, fmt.Errorf("yahoo quoteSummary api error: %s", parsed.QuoteSummary.Error.Description)
	}
	if len(parsed.QuoteSummary.Result) == 0 {
		return Fundamentals{}, fmt.Errorf("yahoo quoteSummary api returned no data for %s", symbol)
	}

	result := parsed.QuoteSummary.Result[0]
	f := Fundamentals{
		PERatio:  result.SummaryDetail.TrailingPE.Raw,
		PEGRatio: result.DefaultKeyStatistics.PegRatio.Raw,
	}
	if v := result.FinancialData.RevenueGrowth.Raw; v != nil {
		pct := *v * 100
		f.RevenueGrowthPct = &pct
	}
	if v := result.FinancialData.ProfitMargins.Raw; v != nil {
		pct := *v * 100
		f.NetMarginPct = &pct
	}

	return f, nil
}
