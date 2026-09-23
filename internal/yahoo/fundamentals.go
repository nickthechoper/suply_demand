package yahoo

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"time"
)

// Fundamentals holds a handful of per-company metrics from Yahoo Finance's
// quoteSummary API. Fields are nil when Yahoo doesn't report a value for
// that symbol (e.g. no analyst PEG estimate).
type Fundamentals struct {
	PERatio            *float64 // trailing P/E
	ForwardPERatio     *float64 // forward P/E, based on analyst EPS estimates
	PEGRatio           *float64
	PSRatio            *float64 // price/sales, trailing twelve months
	RevenueGrowthPct   *float64 // trailing-twelve-month revenue vs. TTM one year ago, as a percentage
	NetMarginPct       *float64 // trailing net profit margin, as a percentage
	OperatingMarginPct *float64 // trailing (TTM) operating margin, as a percentage
	ROICPct            *float64 // return on invested capital, TTM, as a percentage (approximated, see computeROIC)
}

type ttmRevenueTimeseriesResponse struct {
	Timeseries struct {
		Result []struct {
			TrailingTotalRevenue []struct {
				ReportedValue quoteSummaryFloat `json:"reportedValue"`
			} `json:"trailingTotalRevenue"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"timeseries"`
}

type fundamentalsResponse struct {
	QuoteSummary struct {
		Result []struct {
			SummaryDetail struct {
				TrailingPE                   quoteSummaryFloat `json:"trailingPE"`
				PriceToSalesTrailing12Months quoteSummaryFloat `json:"priceToSalesTrailing12Months"`
			} `json:"summaryDetail"`
			DefaultKeyStatistics struct {
				PegRatio          quoteSummaryFloat `json:"pegRatio"`
				ForwardPE         quoteSummaryFloat `json:"forwardPE"`
				BookValue         quoteSummaryFloat `json:"bookValue"`
				SharesOutstanding quoteSummaryFloat `json:"sharesOutstanding"`
			} `json:"defaultKeyStatistics"`
			FinancialData struct {
				RevenueGrowth    quoteSummaryFloat `json:"revenueGrowth"`
				ProfitMargins    quoteSummaryFloat `json:"profitMargins"`
				OperatingMargins quoteSummaryFloat `json:"operatingMargins"`
				TotalCash        quoteSummaryFloat `json:"totalCash"`
				TotalDebt        quoteSummaryFloat `json:"totalDebt"`
			} `json:"financialData"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteSummary"`
}

// assumedTaxRate approximates NOPAT for the ROIC calculation below. Yahoo's
// free quoteSummary API doesn't expose an effective tax rate, so this uses
// the flat US statutory corporate rate as a stand-in.
const assumedTaxRate = 0.21

// FetchFundamentals retrieves P/E, PEG, revenue growth, and net margin for
// symbol from Yahoo Finance's quoteSummary API.
func FetchFundamentals(symbol string) (Fundamentals, error) {
	client, crumb, err := NewAuthedClient()
	if err != nil {
		return Fundamentals{}, err
	}
	return FetchFundamentalsWithClient(client, crumb, symbol)
}

// FetchFundamentalsWithClient is FetchFundamentals against an already-authed
// client/crumb pair, for callers fetching many symbols in a batch who want
// to reuse one session instead of re-authenticating per symbol.
func FetchFundamentalsWithClient(client *http.Client, crumb string, symbol string) (Fundamentals, error) {
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
		PERatio:        result.SummaryDetail.TrailingPE.Raw,
		ForwardPERatio: result.DefaultKeyStatistics.ForwardPE.Raw,
		PEGRatio:       result.DefaultKeyStatistics.PegRatio.Raw,
		PSRatio:        result.SummaryDetail.PriceToSalesTrailing12Months.Raw,
	}
	if v := result.FinancialData.ProfitMargins.Raw; v != nil {
		pct := *v * 100
		f.NetMarginPct = &pct
	}
	if v := result.FinancialData.OperatingMargins.Raw; v != nil {
		pct := *v * 100
		f.OperatingMarginPct = &pct
	}

	oldestRevenue, latestRevenue, err := fetchTTMRevenueSeries(client, crumb, symbol)
	if err == nil && oldestRevenue != nil && latestRevenue != nil && *oldestRevenue != 0 {
		pct := (*latestRevenue - *oldestRevenue) / math.Abs(*oldestRevenue) * 100
		f.RevenueGrowthPct = &pct
	} else if v := result.FinancialData.RevenueGrowth.Raw; v != nil {
		pct := *v * 100
		f.RevenueGrowthPct = &pct
	}

	f.ROICPct = computeROIC(
		f.OperatingMarginPct,
		latestRevenue,
		result.FinancialData.TotalDebt.Raw,
		result.FinancialData.TotalCash.Raw,
		result.DefaultKeyStatistics.BookValue.Raw,
		result.DefaultKeyStatistics.SharesOutstanding.Raw,
	)

	return f, nil
}

// fetchTTMRevenueSeries returns trailing-twelve-month revenue from one year
// ago (oldest) and now (latest), the same methodology charting platforms
// like TradingView use for YoY growth. This differs from Yahoo's own
// financialData.revenueGrowth field, which compares only the most recent
// single quarter against the same quarter a year ago.
func fetchTTMRevenueSeries(client *http.Client, crumb string, symbol string) (oldest, latest *float64, err error) {
	period2 := time.Now().Unix()
	period1 := time.Now().AddDate(-2, 0, 0).Unix()
	dataURL := fmt.Sprintf(
		"https://query2.finance.yahoo.com/ws/fundamentals-timeseries/v1/finance/timeseries/%s?type=trailingTotalRevenue&period1=%d&period2=%d&crumb=%s",
		symbol, period1, period2, url.QueryEscape(crumb),
	)
	body, err := get(client, dataURL)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching yahoo TTM revenue timeseries: %w", err)
	}

	var parsed ttmRevenueTimeseriesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, nil, fmt.Errorf("decoding yahoo TTM revenue timeseries response: %w", err)
	}
	if parsed.Timeseries.Error != nil {
		return nil, nil, fmt.Errorf("yahoo timeseries api error: %s", parsed.Timeseries.Error.Description)
	}
	if len(parsed.Timeseries.Result) == 0 {
		return nil, nil, nil
	}

	for _, point := range parsed.Timeseries.Result[0].TrailingTotalRevenue {
		if point.ReportedValue.Raw == nil {
			continue
		}
		if oldest == nil {
			oldest = point.ReportedValue.Raw
		}
		latest = point.ReportedValue.Raw
	}
	if oldest == latest {
		return nil, nil, nil
	}

	return oldest, latest, nil
}

// computeROIC approximates return on invested capital (TTM) as
// NOPAT / invested capital, where NOPAT = EBIT * (1 - assumedTaxRate), EBIT
// is derived from the operating margin and TTM revenue, and invested capital
// is total debt plus total equity minus cash. Total equity is approximated
// from book value per share * shares outstanding, since Yahoo's free API
// doesn't expose a balance sheet equity line item directly.
func computeROIC(operatingMarginPct, revenue, totalDebt, totalCash, bookValue, sharesOutstanding *float64) *float64 {
	if operatingMarginPct == nil || revenue == nil || totalDebt == nil || totalCash == nil || bookValue == nil || sharesOutstanding == nil {
		return nil
	}

	ebit := *operatingMarginPct / 100 * *revenue
	nopat := ebit * (1 - assumedTaxRate)
	totalEquity := *bookValue * *sharesOutstanding
	investedCapital := *totalDebt + totalEquity - *totalCash
	if investedCapital <= 0 {
		return nil
	}

	roic := nopat / investedCapital * 100
	return &roic
}
