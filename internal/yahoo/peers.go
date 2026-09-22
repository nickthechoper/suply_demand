package yahoo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type PeerQuote struct {
	Symbol     string   `json:"symbol"`
	Name       string   `json:"name"`
	TrailingPE *float64 `json:"trailingPE"`
	ForwardPE  *float64 `json:"forwardPE"`
	MarketCap  *float64 `json:"marketCap"`
}

type quoteBatchResponse struct {
	QuoteResponse struct {
		Result []struct {
			Symbol     string   `json:"symbol"`
			ShortName  string   `json:"shortName"`
			TrailingPE *float64 `json:"trailingPE"`
			ForwardPE  *float64 `json:"forwardPE"`
			MarketCap  *float64 `json:"marketCap"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteResponse"`
}

// FetchPeerQuotes retrieves trailing/forward P/E and market cap for symbol
// plus peerSymbols in a single batched request, via Yahoo's quote endpoint
// (a different, lighter-weight API than quoteSummary — plain numbers, no
// {raw,fmt} wrapping). Results preserve the order: symbol first, then peers.
func FetchPeerQuotes(client *http.Client, crumb string, symbol string, peerSymbols []string) ([]PeerQuote, error) {
	all := append([]string{symbol}, peerSymbols...)
	dataURL := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v7/finance/quote?symbols=%s&crumb=%s",
		url.QueryEscape(strings.Join(all, ",")), url.QueryEscape(crumb),
	)
	body, err := get(client, dataURL)
	if err != nil {
		return nil, fmt.Errorf("fetching yahoo peer quotes: %w", err)
	}

	var parsed quoteBatchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decoding yahoo peer quotes response: %w", err)
	}
	if parsed.QuoteResponse.Error != nil {
		return nil, fmt.Errorf("yahoo quote api error: %s", parsed.QuoteResponse.Error.Description)
	}

	quotes := make([]PeerQuote, 0, len(parsed.QuoteResponse.Result))
	for _, r := range parsed.QuoteResponse.Result {
		quotes = append(quotes, PeerQuote{
			Symbol:     r.Symbol,
			Name:       r.ShortName,
			TrailingPE: r.TrailingPE,
			ForwardPE:  r.ForwardPE,
			MarketCap:  r.MarketCap,
		})
	}
	return quotes, nil
}
