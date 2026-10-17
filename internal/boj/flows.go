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
	seriesCode  = "BPPI6D3N9"
	totalCode   = "BPPI6D3N5A"
	firstData   = 4
)

const (
	KindTotal   = "total"
	KindRegion  = "region"
	KindGroup   = "group"
	KindCountry = "country"
)

var (
	regions = map[string]string{
		"AS": "Asia",
		"NA": "North America",
		"CS": "Central and South America",
		"OC": "Oceania",
		"EP": "Europe",
		"ME": "Middle East",
		"AF": "Africa",
		"OT": "Rest of the World",
		"II": "International Organization",
		"RW": "Unallocated",
	}
	groups = map[string]string{
		"OE": "OECD countries",
		"AA": "ASEAN",
		"EU": "EU",
		"EE": "Eastern Europe, Russia, etc.",
	}
	renames = map[string]string{
		"US": "United States",
		"GB": "United Kingdom",
		"KR": "South Korea",
		"CN": "China",
		"ZA": "South Africa",
		"AE": "UAE",
	}
	EuroArea = []string{"DE", "FR", "NL", "IT", "ES", "BE", "LX", "IE", "AT", "FI", "PT", "GR"}
)

type Row struct {
	Code   string     `json:"code"`
	Name   string     `json:"name"`
	Kind   string     `json:"kind"`
	Region string     `json:"region,omitempty"`
	Values []*float64 `json:"values"`
}

type Flows struct {
	Months    []string `json:"months"`
	Rows      []Row    `json:"rows"`
	Unit      string   `json:"unit"`
	FetchedAt int64    `json:"fetchedAt"`
	Source    string   `json:"source"`
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

	flows, err := parseFlatFile(body)
	if err != nil {
		return nil, err
	}
	flows.FetchedAt = time.Now().Unix()
	cached, cachedAt = flows, time.Now()
	return flows, nil
}

func parseFlatFile(zipped []byte) (*Flows, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	if err != nil {
		return nil, fmt.Errorf("opening BOJ zip: %w", err)
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return ParseCSV(rc)
	}
	return nil, fmt.Errorf("no CSV in BOJ zip")
}

type rawRow struct {
	Row
	cells []string
}

func ParseCSV(r io.Reader) (*Flows, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("reading BOJ header: %w", err)
	}
	if len(header) <= firstData {
		return nil, fmt.Errorf("unexpected BOJ header with %d columns", len(header))
	}

	var raws []rawRow
	region := ""
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading BOJ CSV: %w", err)
		}
		if len(rec) <= firstData {
			continue
		}
		if rec[0] == totalCode {
			raws = append(raws, rawRow{Row: Row{Code: "ALL", Name: "All countries", Kind: KindTotal}, cells: rec[firstData:]})
			continue
		}
		code, ok := strings.CutPrefix(rec[0], seriesCode)
		if !ok || code == "" {
			continue
		}
		row := Row{Code: code, Name: seriesName(rec[2], code)}
		switch {
		case regions[code] != "":
			row.Kind, row.Name = KindRegion, regions[code]
			region = row.Name
		case groups[code] != "":
			row.Kind, row.Name = KindGroup, groups[code]
			region = ""
		default:
			row.Kind, row.Region = KindCountry, region
		}
		raws = append(raws, rawRow{Row: row, cells: rec[firstData:]})
	}

	months := header[firstData:]
	first, last := -1, -1
	for i := range months {
		for _, raw := range raws {
			if raw.Kind == KindCountry && i < len(raw.cells) && strings.TrimSpace(raw.cells[i]) != "" {
				if first < 0 {
					first = i
				}
				last = i
				break
			}
		}
	}
	if first < 0 {
		return nil, fmt.Errorf("BOJ CSV has no long-term debt securities data by country")
	}

	out := &Flows{
		Months: formatMonths(months[first : last+1]),
		Unit:   "bn yen",
		Source: "Bank of Japan / Ministry of Finance, Balance of Payments (BPM6), portfolio investment assets, long-term debt securities, net, by country of issuer",
	}
	byCode := map[string][]*float64{}
	for _, raw := range raws {
		values := make([]*float64, last-first+1)
		for i := first; i <= last && i < len(raw.cells); i++ {
			if v, err := strconv.ParseFloat(strings.TrimSpace(raw.cells[i]), 64); err == nil {
				bn := round(v / 10)
				values[i-first] = &bn
			}
		}
		raw.Values = values
		byCode[raw.Code] = values
		out.Rows = append(out.Rows, raw.Row)
	}

	var members [][]*float64
	for _, code := range EuroArea {
		if v, ok := byCode[code]; ok {
			members = append(members, v)
		}
	}
	if len(members) == len(EuroArea) {
		out.Rows = append(out.Rows, Row{Code: "EA", Name: "Euro area (12)", Kind: KindGroup, Values: add(members)})
	}
	return out, nil
}

func seriesName(label, code string) string {
	if name := renames[code]; name != "" {
		return name
	}
	parts := strings.Split(label, "/")
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[len(parts)-2])
	}
	return code
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
			if i >= len(s) || s[i] == nil {
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
	return math.Round(v*10) / 10
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
