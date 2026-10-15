package finra

import (
	"fmt"
	"sort"
	"time"
)

const (
	historyURL      = dataURL + "CorporateAndAgencyTradeHistory"
	historyPageSize = 5000
	historyMaxPages = 10
)

type DailyYield struct {
	Date     string  `json:"date"`
	YieldPct float64 `json:"yieldPct"`
	Trades   int     `json:"trades"`
}

type trade struct {
	Date  string  `json:"tradeExecutionDate"`
	Price *string `json:"lastSalePrice"`
	Yield *string `json:"lastSaleYield"`
}

func FetchYieldHistory(b Bond, from time.Time) ([]DailyYield, error) {
	now := time.Now().UTC()
	var trades []trade
	for page := 0; page < historyMaxPages; page++ {
		var rows []trade
		err := fetchRows(historyURL, map[string]any{
			"fields":           []string{"tradeExecutionDate", "lastSalePrice", "lastSaleYield"},
			"compareFilters":   []map[string]string{{"fieldName": "issueSymbolIdentifier", "fieldValue": b.Symbol, "compareType": "EQUAL"}},
			"dateRangeFilters": []map[string]string{{"fieldName": "tradeExecutionDate", "startDate": from.Format(time.DateOnly), "endDate": now.Format(time.DateOnly)}},
			"sortFields":       []string{"-tradeExecutionDate", "-tradeExecutionTime"},
			"limit":            historyPageSize,
			"offset":           page * historyPageSize,
		}, &rows)
		if err != nil {
			return nil, fmt.Errorf("fetching finra trade history for %s: %w", b.Symbol, err)
		}
		trades = append(trades, rows...)
		if len(rows) < historyPageSize {
			break
		}
	}
	return dailyYields(trades, b), nil
}

func dailyYields(trades []trade, b Bond) []DailyYield {
	maturity, maturityErr := time.Parse(time.DateOnly, b.Maturity)
	coupon := 0.0
	if b.CouponPct != nil {
		coupon = *b.CouponPct
	}

	byDate := map[string][]float64{}
	for _, t := range trades {
		if y := num(t.Yield); y != nil {
			byDate[t.Date] = append(byDate[t.Date], *y)
			continue
		}
		price := num(t.Price)
		day, err := time.Parse(time.DateOnly, t.Date)
		if price == nil || err != nil || maturityErr != nil {
			continue
		}
		if y, ok := YieldToMaturity(*price, coupon, maturity, day); ok {
			byDate[t.Date] = append(byDate[t.Date], y)
		}
	}

	out := make([]DailyYield, 0, len(byDate))
	for date, yields := range byDate {
		out = append(out, DailyYield{Date: date, YieldPct: median(yields), Trades: len(yields)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}
