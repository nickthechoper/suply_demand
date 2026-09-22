// Package yahoo fetches daily OHLCV bars from Yahoo Finance's public chart API.
package yahoo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Bar struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

type chartResponse struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*float64 `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// FetchDaily retrieves daily OHLCV bars for symbol over the given range
// (e.g. "2y", "5y") from Yahoo Finance's chart endpoint.
func FetchDaily(symbol, rangeStr string) ([]Bar, error) {
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=%s&interval=1d",
		symbol, rangeStr,
	)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; supplydemand-dashboard/1.0)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching yahoo chart data: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yahoo chart api returned status %d: %s", resp.StatusCode, string(body))
	}

	var parsed chartResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decoding yahoo chart response: %w", err)
	}
	if parsed.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo chart api error: %s", parsed.Chart.Error.Description)
	}
	if len(parsed.Chart.Result) == 0 || len(parsed.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, fmt.Errorf("yahoo chart api returned no data for %s", symbol)
	}

	result := parsed.Chart.Result[0]
	quote := result.Indicators.Quote[0]

	bars := make([]Bar, 0, len(result.Timestamp))
	for i, ts := range result.Timestamp {
		if i >= len(quote.Open) || i >= len(quote.High) || i >= len(quote.Low) ||
			i >= len(quote.Close) || i >= len(quote.Volume) {
			continue
		}
		// Skip bars with missing fields (e.g. holidays reported with nulls).
		if quote.Open[i] == nil || quote.High[i] == nil || quote.Low[i] == nil ||
			quote.Close[i] == nil || quote.Volume[i] == nil {
			continue
		}
		bars = append(bars, Bar{
			Time:   time.Unix(ts, 0).UTC(),
			Open:   *quote.Open[i],
			High:   *quote.High[i],
			Low:    *quote.Low[i],
			Close:  *quote.Close[i],
			Volume: *quote.Volume[i],
		})
	}

	return bars, nil
}
