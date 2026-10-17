package boj

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	flatFileURL = "https://www.stat-search.boj.or.jp/info/bp_m_en.zip"
	userAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	cacheTTL    = 6 * time.Hour
	attempts    = 3
	window      = 12
	seriesCode  = "BPPI6D3N9"
)

type Country struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

var (
	Featured = []Country{
		{"DE", "Germany"},
		{"FR", "France"},
		{"NL", "Netherlands"},
		{"IT", "Italy"},
		{"ES", "Spain"},
	}
	OtherEuroArea = []Country{
		{"BE", "Belgium"},
		{"LX", "Luxembourg"},
		{"IE", "Ireland"},
		{"AT", "Austria"},
		{"FI", "Finland"},
		{"PT", "Portugal"},
		{"GR", "Greece"},
	}
	US = Country{"US", "United States"}
)

type Series struct {
	Key    string     `json:"key"`
	Name   string     `json:"name"`
	Values []*float64 `json:"values"`
}

type Flows struct {
	Months     []string `json:"months"`
	EuroArea   []Series `json:"euroArea"`
	TotalEA    Series   `json:"totalEA"`
	US         Series   `json:"us"`
	OtherEA    []Series `json:"otherEA"`
	Cumulative []Series `json:"cumulative"`
	Start      string   `json:"start"`
	Unit       string   `json:"unit"`
	Window     int      `json:"window"`
	FetchedAt  int64    `json:"fetchedAt"`
	Source     string   `json:"source"`
}

var (
	client = &http.Client{Timeout: 90 * time.Second}

	mu       sync.Mutex
	cached   *Flows
	cachedAt time.Time
)

func JapaneseDebtFlows() (*Flows, error) {
	mu.Lock()
	defer mu.Unlock()
	if cached != nil && time.Since(cachedAt) < cacheTTL {
		return cached, nil
	}

	var body []byte
	var err error
	for i := 0; i < attempts; i++ {
		if body, err = get(flatFileURL); err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("fetching BOJ balance of payments: %w", err)
	}

	months, monthly, err := parseFlatFile(body)
	if err != nil {
		return nil, err
	}
	flows := Build(months, monthly)
	flows.FetchedAt = time.Now().Unix()
	cached, cachedAt = flows, time.Now()
	return flows, nil
}

func Build(months []string, monthly map[string][]*float64) *Flows {
	sums := map[string][]*float64{}
	for code, values := range monthly {
		sums[code] = rollingSum(values, window)
	}

	start := window - 1
	if start > len(months) {
		start = len(months)
	}
	out := &Flows{
		Months: formatMonths(months[start:]),
		Unit:   "100bn yen",
		Window: window,
		Source: "Bank of Japan / Ministry of Finance, Balance of Payments (BPM6), portfolio investment assets, long-term debt securities, net",
	}
	trim := func(v []*float64) []*float64 {
		if v == nil {
			return make([]*float64, len(out.Months))
		}
		return v[start:]
	}

	var parts [][]*float64
	for _, c := range Featured {
		v := trim(sums[c.Code])
		parts = append(parts, v)
		out.EuroArea = append(out.EuroArea, Series{Key: c.Code, Name: c.Name, Values: v})
	}
	var others [][]*float64
	for _, c := range OtherEuroArea {
		v := trim(sums[c.Code])
		others = append(others, v)
		out.OtherEA = append(out.OtherEA, Series{Key: c.Code, Name: c.Name, Values: v})
	}
	other := add(others)
	parts = append(parts, other)
	out.EuroArea = append(out.EuroArea, Series{Key: "OTHER", Name: "Other EA", Values: other})
	out.TotalEA = Series{Key: "EA", Name: "Total EA", Values: add(parts)}
	out.US = Series{Key: US.Code, Name: US.Name, Values: trim(sums[US.Code])}
	for _, c := range append(append([]Country{US}, Featured...), OtherEuroArea...) {
		out.Cumulative = append(out.Cumulative, Series{Key: c.Code, Name: c.Name, Values: trim(runningSum(monthly[c.Code]))})
	}
	if len(months) > 0 {
		out.Start = formatMonths(months[:1])[0]
	}
	return out
}

func runningSum(values []*float64) []*float64 {
	if values == nil {
		return nil
	}
	out := make([]*float64, len(values))
	var sum float64
	for i, v := range values {
		if v == nil {
			break
		}
		sum += *v
		s := round(sum / 1000)
		out[i] = &s
	}
	return out
}

func rollingSum(values []*float64, n int) []*float64 {
	out := make([]*float64, len(values))
	for i := n - 1; i < len(values); i++ {
		var sum float64
		complete := true
		for _, v := range values[i-n+1 : i+1] {
			if v == nil {
				complete = false
				break
			}
			sum += *v
		}
		if complete {
			s := round(sum / 1000)
			out[i] = &s
		}
	}
	return out
}

func add(series [][]*float64) []*float64 {
	if len(series) == 0 {
		return nil
	}
	out := make([]*float64, len(series[0]))
	for i := range out {
		var sum float64
		complete := true
		for _, s := range series {
			if s[i] == nil {
				complete = false
				break
			}
			sum += *s[i]
		}
		if complete {
			s := round(sum)
			out[i] = &s
		}
	}
	return out
}

func round(v float64) float64 {
	return math.Round(v*100) / 100
}

func formatMonths(months []string) []string {
	out := make([]string, len(months))
	for i, m := range months {
		if len(m) == 6 {
			out[i] = m[:4] + "-" + m[4:]
		} else {
			out[i] = m
		}
	}
	return out
}

func parseFlatFile(zipped []byte) ([]string, map[string][]*float64, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	if err != nil {
		return nil, nil, fmt.Errorf("opening BOJ zip: %w", err)
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, nil, err
		}
		defer rc.Close()
		return ParseCSV(rc)
	}
	return nil, nil, fmt.Errorf("no CSV in BOJ zip")
}

func ParseCSV(r io.Reader) ([]string, map[string][]*float64, error) {
	wanted := map[string]bool{}
	for _, c := range append(append([]Country{US}, Featured...), OtherEuroArea...) {
		wanted[seriesCode+c.Code] = true
	}

	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	header, err := cr.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("reading BOJ header: %w", err)
	}
	const firstData = 4
	if len(header) <= firstData {
		return nil, nil, fmt.Errorf("unexpected BOJ header with %d columns", len(header))
	}

	raw := map[string][]string{}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("reading BOJ CSV: %w", err)
		}
		if len(rec) > firstData && wanted[rec[0]] {
			raw[strings.TrimPrefix(rec[0], seriesCode)] = rec[firstData:]
		}
	}
	if len(raw) != len(wanted) {
		return nil, nil, fmt.Errorf("BOJ CSV has %d of %d expected series", len(raw), len(wanted))
	}

	months := header[firstData:]
	first, last := -1, -1
	for i := range months {
		for _, rec := range raw {
			if i < len(rec) && strings.TrimSpace(rec[i]) != "" {
				if first < 0 {
					first = i
				}
				last = i
				break
			}
		}
	}
	if first < 0 {
		return nil, nil, fmt.Errorf("BOJ CSV has no data for the expected series")
	}

	out := map[string][]*float64{}
	for code, rec := range raw {
		values := make([]*float64, last-first+1)
		for i := first; i <= last && i < len(rec); i++ {
			if v, err := strconv.ParseFloat(strings.TrimSpace(rec[i]), 64); err == nil {
				values[i-first] = &v
			}
		}
		out[code] = values
	}
	return months[first : last+1], out, nil
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
		return nil, fmt.Errorf("BOJ returned status %d", resp.StatusCode)
	}
	return body, nil
}
