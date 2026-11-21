package openinsider

import (
	"context"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	cacheTTL  = 10 * time.Minute
	attempts  = 2
	latestTTL = 24 * time.Hour

	LatestWindow = 30
	smallDays    = 3

	WindowDays = 730
)

type Trade struct {
	Ticker     string   `json:"ticker,omitempty"`
	Company    string   `json:"company,omitempty"`
	Flags      string   `json:"flags"`
	Filed      string   `json:"filed,omitempty"`
	TradeDate  string   `json:"tradeDate"`
	FilingDate string   `json:"filingDate"`
	Insider    string   `json:"insider"`
	Title      string   `json:"title"`
	Type       string   `json:"type"`
	Code       string   `json:"code"`
	Price      *float64 `json:"price"`
	Qty        *float64 `json:"qty"`
	Owned      *float64 `json:"owned"`
	OwnedChg   string   `json:"ownedChange"`
	Value      *float64 `json:"value"`
	FilingURL  string   `json:"filingUrl"`
	InsiderURL string   `json:"insiderUrl"`
}

var (
	tableRe  = regexp.MustCompile(`(?s)class="tinytable">(.*?)<tbody>(.*?)</tbody>`)
	headRe   = regexp.MustCompile(`(?s)<th[^>]*>(.*?)</th>`)
	rowRe    = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	cellRe   = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	tagRe    = regexp.MustCompile(`<[^>]+>`)
	secRe    = regexp.MustCompile(`href="([^"]*sec\.gov[^"]*)"`)
	personRe = regexp.MustCompile(`href="(/insider/[^"]*)"`)
	tickerRe = regexp.MustCompile(`href="/([A-Za-z0-9.\-]+)"`)
	numRe    = regexp.MustCompile(`[$,+%]`)

	client = &http.Client{Timeout: 45 * time.Second}

	mu    sync.Mutex
	cache = map[string]cacheEntry{}

	latestMu    sync.Mutex
	latestFull  cacheEntry
	latestSmall = map[int]cacheEntry{}
	latestBusy  bool
	latestSave  func([]Trade) error
)

type cacheEntry struct {
	at     time.Time
	trades []Trade
}

func FetchTrades(symbol string, days int) ([]Trade, error) {
	key := fmt.Sprintf("%s/%d", symbol, days)
	mu.Lock()
	if e, ok := cache[key]; ok && time.Since(e.at) < cacheTTL {
		mu.Unlock()
		return e.trades, nil
	}
	mu.Unlock()

	trades, err := Fetch(symbol, days)
	if err != nil {
		return nil, err
	}

	mu.Lock()
	cache[key] = cacheEntry{at: time.Now(), trades: trades}
	mu.Unlock()
	return trades, nil
}

func Fetch(symbol string, days int) ([]Trade, error) {
	url := fmt.Sprintf("http://openinsider.com/screener?s=%s&fd=0&td=%d&cnt=5000&xp=1&xs=1&xa=1&xd=1&xg=1&xf=1&xm=1&xx=1&xc=1&xw=1", symbol, days)
	var body []byte
	var err error
	for i := 0; i < attempts; i++ {
		if body, err = get(url); err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("fetching openinsider for %s: %w", symbol, err)
	}

	return parse(string(body)), nil
}

func UseLatestStore(load func() ([]Trade, time.Time, bool), save func([]Trade) error) {
	latestMu.Lock()
	defer latestMu.Unlock()
	latestSave = save
	if load == nil {
		return
	}
	if trades, at, ok := load(); ok {
		latestFull = cacheEntry{at: at, trades: trades}
	}
}

func StartLatestRefresh(ctx context.Context) {
	go func() {
		latestMu.Lock()
		stale := latestFull.at.IsZero() || time.Since(latestFull.at) >= latestTTL
		latestMu.Unlock()
		if stale {
			refreshLatest()
		}
		ticker := time.NewTicker(latestTTL)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refreshLatest()
			}
		}
	}()
}

func Latest(days int) ([]Trade, time.Time, error) {
	latestMu.Lock()
	if !latestFull.at.IsZero() {
		e := latestFull
		latestMu.Unlock()
		return filedWithin(e.trades, days), e.at, nil
	}
	if !latestBusy {
		go refreshLatest()
	}
	if e, ok := latestSmall[days]; ok && time.Since(e.at) < cacheTTL {
		latestMu.Unlock()
		return e.trades, e.at, nil
	}
	latestMu.Unlock()

	if days <= smallDays {
		trades, err := fetchLatest(days)
		if err != nil {
			return nil, time.Time{}, err
		}
		e := cacheEntry{at: time.Now(), trades: trades}
		latestMu.Lock()
		latestSmall[days] = e
		latestMu.Unlock()
		return e.trades, e.at, nil
	}

	for {
		time.Sleep(200 * time.Millisecond)
		latestMu.Lock()
		e := latestFull
		busy := latestBusy
		latestMu.Unlock()
		if !e.at.IsZero() {
			return filedWithin(e.trades, days), e.at, nil
		}
		if !busy {
			return nil, time.Time{}, fmt.Errorf("fetching latest openinsider trades failed")
		}
	}
}

func refreshLatest() {
	latestMu.Lock()
	if latestBusy {
		latestMu.Unlock()
		return
	}
	latestBusy = true
	latestMu.Unlock()

	trades, err := fetchLatest(LatestWindow)

	latestMu.Lock()
	latestBusy = false
	save := latestSave
	if err == nil {
		latestFull = cacheEntry{at: time.Now(), trades: trades}
	}
	latestMu.Unlock()

	if err != nil {
		log.Printf("warning: could not refresh latest insider trades: %v", err)
		return
	}
	if save != nil {
		if err := save(trades); err != nil {
			log.Printf("warning: could not store latest insider trades: %v", err)
		}
	}
}

func fetchLatest(days int) ([]Trade, error) {
	url := fmt.Sprintf("http://openinsider.com/screener?s=&fd=%d&td=0&cnt=5000&xp=1&xs=1", days)
	var body []byte
	var err error
	for i := 0; i < attempts; i++ {
		if body, err = get(url); err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("fetching latest openinsider trades: %w", err)
	}
	return parse(string(body)), nil
}

func filedWithin(trades []Trade, days int) []Trade {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")
	out := []Trade{}
	for _, t := range trades {
		if len(t.Filed) < 10 || t.Filed[:10] >= cutoff {
			out = append(out, t)
		}
	}
	return out
}

func Within(trades []Trade, days int) []Trade {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")
	out := []Trade{}
	for _, t := range trades {
		if t.TradeDate >= cutoff {
			out = append(out, t)
		}
	}
	return out
}

func NetOpenMarket(trades []Trade) float64 {
	var net float64
	for _, t := range trades {
		if (t.Code == "P" || t.Code == "S") && t.Value != nil {
			net += *t.Value
		}
	}
	return net
}

func get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
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
		return nil, fmt.Errorf("openinsider returned status %d", resp.StatusCode)
	}
	return body, nil
}

func parse(page string) []Trade {
	table := tableRe.FindStringSubmatch(page)
	if table == nil {
		return []Trade{}
	}
	col := map[string]int{}
	for i, h := range headRe.FindAllStringSubmatch(table[1], -1) {
		col[strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(h[1], ""))), " ")] = i
	}
	insider, ok := col["Insider Name"]
	if !ok {
		insider = 4
	}
	at := func(cells []string, name string, fallback int) string {
		i, ok := col[name]
		if !ok {
			i = fallback
		}
		if i < 0 || i >= len(cells) {
			return ""
		}
		return cells[i]
	}

	trades := []Trade{}
	for _, row := range rowRe.FindAllStringSubmatch(table[2], -1) {
		raw := cellRe.FindAllStringSubmatch(row[1], -1)
		if len(raw) < insider+8 {
			continue
		}
		cells := make([]string, len(raw))
		for i, c := range raw {
			cells[i] = strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(c[1], "")))
		}
		filed := at(cells, "Filing Date", 1)
		t := Trade{
			Company:    at(cells, "Company Name", -1),
			Flags:      at(cells, "X", 0),
			TradeDate:  at(cells, "Trade Date", 2),
			FilingDate: prefix(filed, 10),
			Insider:    cells[insider],
			Title:      at(cells, "Title", insider+1),
			Type:       at(cells, "Trade Type", insider+2),
			Price:      number(at(cells, "Price", insider+3)),
			Qty:        number(at(cells, "Qty", insider+4)),
			Owned:      number(at(cells, "Owned", insider+5)),
			OwnedChg:   at(cells, "ΔOwn", insider+6),
			Value:      number(at(cells, "Value", insider+7)),
		}
		t.Code = prefix(t.Type, 1)
		if i, ok := col["Ticker"]; ok && i < len(raw) {
			if m := tickerRe.FindStringSubmatch(raw[i][1]); m != nil {
				t.Ticker = m[1]
			}
		}
		if len(filed) > 10 {
			t.Filed = filed
		}
		if m := secRe.FindStringSubmatch(raw[col["Filing Date"]][1]); m != nil {
			t.FilingURL = m[1]
		}
		if m := personRe.FindStringSubmatch(raw[insider][1]); m != nil {
			t.InsiderURL = "http://openinsider.com" + m[1]
		}
		trades = append(trades, t)
	}
	return trades
}

func prefix(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func number(s string) *float64 {
	v, err := strconv.ParseFloat(numRe.ReplaceAllString(s, ""), 64)
	if err != nil {
		return nil
	}
	return &v
}
