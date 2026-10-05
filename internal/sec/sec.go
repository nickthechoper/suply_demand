package sec

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	tickersURL     = "https://www.sec.gov/files/company_tickers.json"
	submissionsURL = "https://data.sec.gov/submissions/CIK%010d.json"
	archiveURL     = "https://www.sec.gov/Archives/edgar/data/%d/%s/%s"
	tickersTTL     = 24 * time.Hour
	minGap         = 200 * time.Millisecond
	maxDocBytes    = 40 << 20
)

var ErrNotFound = errors.New("symbol not found in SEC company tickers")

type Filing struct {
	Form      string
	Date      string
	Accession string
	URL       string
}

type Client struct {
	userAgent string
	http      *http.Client

	mu        sync.Mutex
	lastReq   time.Time
	ciks      map[string]int
	ciksStamp time.Time
}

func LoadUserAgent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var cfg struct {
		UserAgent string `json:"userAgent"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("decoding %s: %w", path, err)
	}
	return strings.TrimSpace(cfg.UserAgent), nil
}

func NewClient(userAgent string) *Client {
	return &Client{userAgent: userAgent, http: &http.Client{Timeout: 90 * time.Second}}
}

func (c *Client) get(url string, limit int64) ([]byte, error) {
	c.mu.Lock()
	if wait := minGap - time.Since(c.lastReq); wait > 0 {
		time.Sleep(wait)
	}
	c.lastReq = time.Now()
	c.mu.Unlock()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func (c *Client) cik(symbol string) (int, error) {
	c.mu.Lock()
	cached := c.ciks != nil && time.Since(c.ciksStamp) < tickersTTL
	ciks := c.ciks
	c.mu.Unlock()

	if !cached {
		body, err := c.get(tickersURL, 8<<20)
		if err != nil {
			return 0, err
		}
		var raw map[string]struct {
			CIK    int    `json:"cik_str"`
			Ticker string `json:"ticker"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return 0, fmt.Errorf("decoding company tickers: %w", err)
		}
		ciks = make(map[string]int, len(raw))
		for _, entry := range raw {
			ciks[strings.ToUpper(entry.Ticker)] = entry.CIK
		}
		c.mu.Lock()
		c.ciks, c.ciksStamp = ciks, time.Now()
		c.mu.Unlock()
	}

	cik, ok := ciks[strings.ToUpper(symbol)]
	if !ok {
		return 0, ErrNotFound
	}
	return cik, nil
}

var (
	annualForms = map[string]bool{"10-K": true, "20-F": true, "40-F": true}
	hiddenRe    = regexp.MustCompile(`(?is)<ix:header>.*?</ix:header>`)
	scriptRe    = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe     = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	tagRe       = regexp.MustCompile(`<[^>]+>`)
	spaceRe     = regexp.MustCompile(`\s+`)
)

func (c *Client) LatestFilings(symbol string) ([]Filing, error) {
	cik, err := c.cik(symbol)
	if err != nil {
		return nil, err
	}

	body, err := c.get(fmt.Sprintf(submissionsURL, cik), 32<<20)
	if err != nil {
		return nil, err
	}
	var sub struct {
		Filings struct {
			Recent struct {
				Accession  []string `json:"accessionNumber"`
				Date       []string `json:"filingDate"`
				Form       []string `json:"form"`
				PrimaryDoc []string `json:"primaryDocument"`
			} `json:"recent"`
		} `json:"filings"`
	}
	if err := json.Unmarshal(body, &sub); err != nil {
		return nil, fmt.Errorf("decoding submissions for %s: %w", symbol, err)
	}

	recent := sub.Filings.Recent
	var filings []Filing
	var haveAnnual, haveQuarterly bool
	for i, form := range recent.Form {
		if i >= len(recent.Accession) || i >= len(recent.Date) || i >= len(recent.PrimaryDoc) {
			break
		}
		switch {
		case annualForms[form] && !haveAnnual:
			haveAnnual = true
		case form == "10-Q" && !haveQuarterly:
			haveQuarterly = true
		default:
			continue
		}
		filings = append(filings, Filing{
			Form:      form,
			Date:      recent.Date[i],
			Accession: recent.Accession[i],
			URL:       fmt.Sprintf(archiveURL, cik, strings.ReplaceAll(recent.Accession[i], "-", ""), recent.PrimaryDoc[i]),
		})
		if haveAnnual && haveQuarterly {
			break
		}
	}
	return filings, nil
}

func (c *Client) Text(filing Filing) (string, error) {
	body, err := c.get(filing.URL, maxDocBytes)
	if err != nil {
		return "", err
	}
	return htmlToText(string(body)), nil
}

func htmlToText(doc string) string {
	doc = hiddenRe.ReplaceAllString(doc, " ")
	doc = scriptRe.ReplaceAllString(doc, " ")
	doc = styleRe.ReplaceAllString(doc, " ")
	doc = tagRe.ReplaceAllString(doc, " ")
	doc = html.UnescapeString(doc)
	return spaceRe.ReplaceAllString(strings.ReplaceAll(doc, " ", " "), " ")
}
