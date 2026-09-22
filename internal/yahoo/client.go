package yahoo

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
)

// quoteSummaryFloat unwraps Yahoo's {"raw": ..., "fmt": "..."} number encoding.
type quoteSummaryFloat struct {
	Raw *float64 `json:"raw"`
}

type quoteSummaryDate struct {
	Fmt string `json:"fmt"`
}

// newAuthedClient performs the two-step handshake Yahoo's quoteSummary API
// requires (prime a session cookie, then exchange it for a crumb token) and
// returns an http.Client whose cookie jar is ready to use alongside that
// crumb on subsequent requests.
func newAuthedClient() (*http.Client, string, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, "", err
	}
	client := &http.Client{Jar: jar}

	// Step 1: hit a Yahoo domain to receive session cookies. The response
	// itself is irrelevant (often a 404); only the Set-Cookie headers matter.
	primeReq, err := http.NewRequest(http.MethodGet, "https://fc.yahoo.com", nil)
	if err != nil {
		return nil, "", err
	}
	primeReq.Header.Set("User-Agent", userAgent)
	if resp, err := client.Do(primeReq); err == nil {
		resp.Body.Close()
	}

	// Step 2: exchange the session cookie for a crumb token.
	crumbReq, err := http.NewRequest(http.MethodGet, "https://query2.finance.yahoo.com/v1/test/getcrumb", nil)
	if err != nil {
		return nil, "", err
	}
	crumbReq.Header.Set("User-Agent", userAgent)
	crumbResp, err := client.Do(crumbReq)
	if err != nil {
		return nil, "", fmt.Errorf("fetching yahoo crumb: %w", err)
	}
	crumbBody, err := io.ReadAll(crumbResp.Body)
	crumbResp.Body.Close()
	if err != nil {
		return nil, "", err
	}
	if crumbResp.StatusCode != http.StatusOK || len(crumbBody) == 0 {
		return nil, "", fmt.Errorf("yahoo crumb request returned status %d", crumbResp.StatusCode)
	}

	return client, string(crumbBody), nil
}

// get issues an authenticated GET against rawURL (which must already include
// the crumb query param) and returns the response body.
func get(client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yahoo api returned status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}
