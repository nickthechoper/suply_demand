package tic

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	tableURL  = "https://ticdata.treasury.gov/resource-center/data-chart-center/tic/Documents/slt_table4.txt"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	cacheTTL  = 6 * time.Hour
	attempts  = 3
)

const (
	KindTotal   = "total"
	KindRegion  = "region"
	KindGroup   = "group"
	KindCountry = "country"
)

var (
	monthRe    = regexp.MustCompile(`^\d{4}-\d{2}$`)
	canadaCode = 29998
	regions    = map[int]string{
		19992: "Europe",
		29998: "Canada",
		39942: "Latin America",
		34401: "Caribbean",
		49999: "Asia",
		59994: "Africa",
		69906: "Other countries",
		79995: "International and regional orgs",
	}
	groups = map[int]string{
		16713: "Euro area",
		16721: "European Union",
	}
	renames = map[string]string{
		"China, Mainland":      "China",
		"Korea, South":         "South Korea",
		"United Arab Emirates": "UAE",
	}
	columns = []string{"country", "country_code", "date", "us_lt_govt_bond_pur", "us_lt_corp_bond_pur", "us_lt_govt_bond_sale", "us_lt_corp_bond_sale"}
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

func USForeignBondFlows() (*Flows, error) {
	mu.Lock()
	defer mu.Unlock()
	if cached != nil && time.Since(cachedAt) < cacheTTL {
		return cached, nil
	}

	var body []byte
	var err error
	for i := 0; i < attempts; i++ {
		if body, err = get(tableURL); err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("fetching TIC SLT table 4: %w", err)
	}

	flows, err := Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	flows.FetchedAt = time.Now().Unix()
	cached, cachedAt = flows, time.Now()
	return flows, nil
}

func classify(code int, name string) []Row {
	key := strconv.Itoa(code)
	switch {
	case code == 99996:
		return []Row{{Code: key, Name: "All countries", Kind: KindTotal}}
	case code == canadaCode:
		return []Row{{Code: key, Name: "Canada", Kind: KindCountry}, {Code: key + "R", Name: regions[code], Kind: KindRegion}}
	case regions[code] != "":
		return []Row{{Code: key, Name: regions[code], Kind: KindRegion}}
	case groups[code] != "":
		return []Row{{Code: key, Name: groups[code], Kind: KindGroup}}
	case code >= 69995:
		return nil
	}
	if r := renames[name]; r != "" {
		name = r
	}
	region := "Other countries"
	switch {
	case code < 19992:
		region = "Europe"
	case code < 34401:
		region = "Latin America"
	case code < 39942:
		region = "Caribbean"
	case code < 50000:
		region = "Asia"
	case code < 60000:
		region = "Africa"
	}
	return []Row{{Code: key, Name: name, Kind: KindCountry, Region: region}}
}

func Parse(r io.Reader) (*Flows, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)

	idx := map[string]int{}
	type entry struct {
		row    Row
		values map[string]float64
	}
	var order []string
	entries := map[string]*entry{}
	monthSet := map[string]bool{}

	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		if len(idx) == 0 {
			if len(fields) > 0 && strings.TrimSpace(fields[0]) == "country" {
				for i, f := range fields {
					idx[strings.TrimSpace(f)] = i
				}
				for _, c := range columns {
					if _, ok := idx[c]; !ok {
						return nil, fmt.Errorf("TIC table is missing column %q", c)
					}
				}
			}
			continue
		}
		get := func(col string) string {
			if i := idx[col]; i < len(fields) {
				return strings.TrimSpace(fields[i])
			}
			return ""
		}
		month := get("date")
		if !monthRe.MatchString(month) {
			continue
		}
		code, err := strconv.Atoi(get("country_code"))
		if err != nil {
			continue
		}
		rows := classify(code, get("country"))
		if len(rows) == 0 {
			continue
		}

		var net float64
		reported := false
		for i, col := range columns[3:] {
			v, err := strconv.ParseFloat(strings.ReplaceAll(strings.Trim(get(col), `"`), ",", ""), 64)
			if err != nil {
				continue
			}
			reported = true
			if i < 2 {
				net += v
			} else {
				net -= v
			}
		}
		if !reported {
			continue
		}

		for _, row := range rows {
			e := entries[row.Code]
			if e == nil {
				e = &entry{row: row, values: map[string]float64{}}
				entries[row.Code] = e
				order = append(order, row.Code)
			}
			e.values[month] = net
		}
		monthSet[month] = true
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading TIC table: %w", err)
	}
	if len(idx) == 0 {
		return nil, fmt.Errorf("TIC table header not found")
	}
	if len(monthSet) == 0 {
		return nil, fmt.Errorf("TIC table has no monthly data")
	}

	months := make([]string, 0, len(monthSet))
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)

	out := &Flows{
		Months: months,
		Unit:   "bn usd",
		Source: "U.S. Treasury, Treasury International Capital (TIC) SLT table 4, U.S. purchases minus sales of foreign government and corporate long-term bonds, by country of issuer",
	}
	for _, key := range order {
		e := entries[key]
		values := make([]*float64, len(months))
		for i, m := range months {
			if v, ok := e.values[m]; ok {
				bn := math.Round(v/100) / 10
				values[i] = &bn
			}
		}
		e.row.Values = values
		out.Rows = append(out.Rows, e.row)
	}
	return out, nil
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
		return nil, fmt.Errorf("TIC returned status %d", resp.StatusCode)
	}
	return body, nil
}
