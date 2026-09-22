package yahoo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
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

type quoteSummaryResponse struct {
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

type quoteSummaryFloat struct {
	Raw *float64 `json:"raw"`
}

// FetchFundamentals retrieves P/E, PEG, revenue growth, and net margin for
// symbol from Yahoo Finance's quoteSummary API. That endpoint requires a
// session cookie plus a matching "crumb" token, so this performs the
// two-step handshake (prime cookies, fetch crumb) before the real request.
func FetchFundamentals(symbol string) (Fundamentals, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return Fundamentals{}, err
	}
	client := &http.Client{Jar: jar}

	// Step 1: hit a Yahoo domain to receive session cookies. The response
	// itself is irrelevant (often a 404); only the Set-Cookie headers matter.
	primeReq, err := http.NewRequest(http.MethodGet, "https://fc.yahoo.com", nil)
	if err != nil {
		return Fundamentals{}, err
	}
	primeReq.Header.Set("User-Agent", userAgent)
	if resp, err := client.Do(primeReq); err == nil {
		resp.Body.Close()
	}

	// Step 2: exchange the session cookie for a crumb token.
	crumbReq, err := http.NewRequest(http.MethodGet, "https://query2.finance.yahoo.com/v1/test/getcrumb", nil)
	if err != nil {
		return Fundamentals{}, err
	}
	crumbReq.Header.Set("User-Agent", userAgent)
	crumbResp, err := client.Do(crumbReq)
	if err != nil {
		return Fundamentals{}, fmt.Errorf("fetching yahoo crumb: %w", err)
	}
	crumbBody, err := io.ReadAll(crumbResp.Body)
	crumbResp.Body.Close()
	if err != nil {
		return Fundamentals{}, err
	}
	if crumbResp.StatusCode != http.StatusOK || len(crumbBody) == 0 {
		return Fundamentals{}, fmt.Errorf("yahoo crumb request returned status %d", crumbResp.StatusCode)
	}
	crumb := string(crumbBody)

	// Step 3: the actual fundamentals request, authenticated with the cookie
	// jar (attached automatically by the client) plus the crumb query param.
	dataURL := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=defaultKeyStatistics,financialData,summaryDetail&crumb=%s",
		symbol, url.QueryEscape(crumb),
	)
	dataReq, err := http.NewRequest(http.MethodGet, dataURL, nil)
	if err != nil {
		return Fundamentals{}, err
	}
	dataReq.Header.Set("User-Agent", userAgent)
	dataResp, err := client.Do(dataReq)
	if err != nil {
		return Fundamentals{}, fmt.Errorf("fetching yahoo fundamentals: %w", err)
	}
	defer dataResp.Body.Close()

	body, err := io.ReadAll(dataResp.Body)
	if err != nil {
		return Fundamentals{}, err
	}
	if dataResp.StatusCode != http.StatusOK {
		return Fundamentals{}, fmt.Errorf("yahoo quoteSummary api returned status %d: %s", dataResp.StatusCode, string(body))
	}

	var parsed quoteSummaryResponse
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
