package finra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	dataURL   = "https://services-dynarep.ddwa.finra.org/public/reporting/v2/data/group/FixedIncomeMarket/name/"
	endpoint  = dataURL + "CorporateAndAgencySecurities"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	pageSize  = 1000
	maxPages  = 10

	HeadlineMaxAge      = 14 * 24 * time.Hour
	HeadlineMinMaturity = 90 * 24 * time.Hour
)

type Bond struct {
	Symbol      string   `json:"symbol"`
	CUSIP       string   `json:"cusip"`
	Issuer      string   `json:"issuer"`
	CouponPct   *float64 `json:"couponPct"`
	Maturity    string   `json:"maturity"`
	Callable    bool     `json:"callable"`
	Convertible bool     `json:"convertible"`
	Grade       string   `json:"grade"`
	Moodys      string   `json:"moodys"`
	SP          string   `json:"sp"`
	Price       *float64 `json:"price"`
	PriceChange *float64 `json:"priceChange"`
	YieldPct    *float64 `json:"yieldPct"`
	YieldChange *float64 `json:"yieldChange"`
	LastTrade   string   `json:"lastTrade"`
}

func (b Bond) Label() string {
	coupon := "?"
	if b.CouponPct != nil {
		coupon = strconv.FormatFloat(*b.CouponPct, 'f', -1, 64)
	}
	label := fmt.Sprintf("%s%% %s", coupon, b.Maturity)
	if b.Convertible {
		label += " conv."
	}
	return label
}

type record struct {
	Symbol      string  `json:"issueSymbolIdentifier"`
	Issuer      string  `json:"issuerName"`
	CUSIP       *string `json:"cusip"`
	Coupon      *string `json:"couponRate"`
	Maturity    *string `json:"maturityDate"`
	Callable    *string `json:"isCallable"`
	Convertible *string `json:"isConvertible"`
	Grade       *string `json:"traceGradeCode"`
	Moodys      *string `json:"moodysRating"`
	SP          *string `json:"standardAndPoorsRating"`
	Price       *string `json:"lastSalePrice"`
	PriceChange *string `json:"priceChangeNumber"`
	Yield       *string `json:"lastSaleYield"`
	LastTrade   *string `json:"lastTradeDate"`
}

var fields = []string{
	"issueSymbolIdentifier", "issuerName", "cusip", "couponRate", "maturityDate", "isCallable", "isConvertible",
	"traceGradeCode", "moodysRating", "standardAndPoorsRating", "lastSalePrice", "priceChangeNumber", "lastSaleYield", "lastTradeDate",
}

var (
	endpointURL, _ = url.Parse(endpoint)
	jar, _         = cookiejar.New(nil)
	client         = &http.Client{Timeout: 60 * time.Second, Jar: jar}
)

func Fetch(ticker, company string) ([]Bond, error) {
	prefix := strings.ToUpper(strings.TrimSpace(ticker))
	if i := strings.IndexAny(prefix, "-."); i >= 0 {
		prefix = prefix[:i]
	}
	if prefix == "" {
		return nil, fmt.Errorf("empty ticker")
	}

	var records []record
	for page := 0; page < maxPages; page++ {
		rows, err := query(prefix, page*pageSize)
		if err != nil {
			return nil, fmt.Errorf("fetching finra bonds for %s: %w", ticker, err)
		}
		done := len(rows) < pageSize
		for _, r := range rows {
			rest := strings.TrimPrefix(r.Symbol, prefix)
			if rest == "" || rest == r.Symbol {
				continue
			}
			if c := rest[0]; c == '.' || c >= '0' && c <= '9' {
				if SameCompany(company, r.Issuer) {
					records = append(records, r)
				}
			} else if c > '9' {
				done = true
				break
			}
		}
		if done {
			break
		}
	}
	return convert(records, time.Now().UTC()), nil
}

func Headline(bonds []Bond, now time.Time) (Bond, bool) {
	var best Bond
	found := false
	for _, b := range bonds {
		if b.Convertible || b.YieldPct == nil {
			continue
		}
		traded, err := time.Parse(time.DateOnly, b.LastTrade)
		if err != nil || now.Sub(traded) > HeadlineMaxAge {
			continue
		}
		maturity, err := time.Parse(time.DateOnly, b.Maturity)
		if err != nil || maturity.Sub(now) < HeadlineMinMaturity {
			continue
		}
		if !found || *b.YieldPct > *best.YieldPct {
			best, found = b, true
		}
	}
	return best, found
}

func query(prefix string, offset int) ([]record, error) {
	var rows []record
	err := fetchRows(endpoint, map[string]any{
		"fields":         fields,
		"compareFilters": []map[string]string{{"fieldName": "issueSymbolIdentifier", "fieldValue": prefix, "compareType": "BEGINS_WITH"}},
		"sortFields":     []string{"+issueSymbolIdentifier"},
		"limit":          pageSize,
		"offset":         offset,
	}, &rows)
	return rows, err
}

func fetchRows(url string, request map[string]any, rows any) error {
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}

	resp, err := post(url, body)
	if err != nil {
		return err
	}
	var envelope struct {
		Status        string `json:"status"`
		StatusMessage string `json:"statusMessage"`
		ReturnBody    struct {
			Data string `json:"data"`
		} `json:"returnBody"`
	}
	if err := json.Unmarshal(resp, &envelope); err != nil {
		return fmt.Errorf("decoding finra response: %w", err)
	}
	if envelope.Status != "success" {
		return fmt.Errorf("finra returned %q: %s", envelope.Status, envelope.StatusMessage)
	}
	if err := json.Unmarshal([]byte(envelope.ReturnBody.Data), rows); err != nil {
		return fmt.Errorf("decoding finra rows: %w", err)
	}
	return nil
}

func post(url string, body []byte) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Origin", "https://www.finra.org")
		req.Header.Set("Referer", "https://www.finra.org/")
		if token := xsrfToken(); token != "" {
			req.Header.Set("X-XSRF-TOKEN", token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && attempt == 0 {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("finra returned status %d", resp.StatusCode)
		}
		return data, nil
	}
}

func xsrfToken() string {
	for _, c := range jar.Cookies(endpointURL) {
		if c.Name == "XSRF-TOKEN" {
			return c.Value
		}
	}
	return ""
}

func nameWords(name string) []string {
	var words []string
	for _, w := range strings.Fields(strings.ToUpper(name)) {
		w = strings.Map(func(r rune) rune {
			if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, w)
		if w == "" || len(words) == 0 && w == "THE" {
			continue
		}
		words = append(words, w)
	}
	return words
}

func issuerWord(name string) string {
	if words := nameWords(name); len(words) > 0 {
		return words[0]
	}
	return ""
}

func SameCompany(company, issuer string) bool {
	companyWords, issuerWords := nameWords(company), nameWords(issuer)
	if len(companyWords) == 0 || len(issuerWords) == 0 {
		return true
	}
	return strings.HasPrefix(strings.Join(issuerWords, ""), companyWords[0]) ||
		strings.HasPrefix(strings.Join(companyWords, ""), issuerWords[0])
}

func mainIssuer(records []record) []record {
	counts := map[string]int{}
	main := ""
	for _, r := range records {
		w := issuerWord(r.Issuer)
		counts[w]++
		if counts[w] > counts[main] || counts[w] == counts[main] && w < main {
			main = w
		}
	}
	out := records[:0:0]
	for _, r := range records {
		if issuerWord(r.Issuer) == main {
			out = append(out, r)
		}
	}
	return out
}

func convert(records []record, now time.Time) []Bond {
	type key struct{ coupon, maturity, convertible string }
	picked := map[key]record{}
	var order []key
	for _, r := range mainIssuer(records) {
		k := key{str(r.Coupon), str(r.Maturity), str(r.Convertible)}
		prev, seen := picked[k]
		if !seen {
			order = append(order, k)
			picked[k] = r
			continue
		}
		if (r.Yield != nil) != (prev.Yield != nil) {
			if r.Yield != nil {
				picked[k] = r
			}
			continue
		}
		if str(r.LastTrade) > str(prev.LastTrade) {
			picked[k] = r
		}
	}

	today := now.Format(time.DateOnly)
	bonds := []Bond{}
	for _, k := range order {
		r := picked[k]
		if k.maturity != "" && k.maturity < today {
			continue
		}
		b := Bond{
			Symbol:      r.Symbol,
			CUSIP:       str(r.CUSIP),
			Issuer:      r.Issuer,
			CouponPct:   num(r.Coupon),
			Maturity:    k.maturity,
			Callable:    str(r.Callable) == "Y",
			Convertible: k.convertible == "Y",
			Grade:       str(r.Grade),
			Moodys:      str(r.Moodys),
			SP:          str(r.SP),
			Price:       num(r.Price),
			PriceChange: num(r.PriceChange),
			YieldPct:    num(r.Yield),
			LastTrade:   str(r.LastTrade),
		}
		fillYields(&b, now)
		bonds = append(bonds, b)
	}
	sort.SliceStable(bonds, func(i, j int) bool { return bonds[i].Maturity < bonds[j].Maturity })
	return bonds
}

func fillYields(b *Bond, now time.Time) {
	maturity, err := time.Parse(time.DateOnly, b.Maturity)
	if err != nil || b.Price == nil {
		return
	}
	coupon := 0.0
	if b.CouponPct != nil {
		coupon = *b.CouponPct
	}
	current, ok := YieldToMaturity(*b.Price, coupon, maturity, now)
	if !ok {
		return
	}
	if b.YieldPct == nil {
		b.YieldPct = &current
	}
	if b.PriceChange == nil {
		return
	}
	if previous, ok := YieldToMaturity(*b.Price-*b.PriceChange, coupon, maturity, now); ok {
		change := current - previous
		b.YieldChange = &change
	}
}

func YieldToMaturity(price, couponPct float64, maturity, settle time.Time) (float64, bool) {
	if price <= 0 || !maturity.After(settle) {
		return 0, false
	}

	periods := 1
	prev := maturity.AddDate(0, -6, 0)
	for prev.After(settle) {
		periods++
		prev = maturity.AddDate(0, -6*periods, 0)
	}
	next := maturity.AddDate(0, -6*(periods-1), 0)
	w := next.Sub(settle).Hours() / next.Sub(prev).Hours()

	c := couponPct / 2
	dirty := price + c*(1-w)
	pv := func(y float64) float64 {
		r := 1 + y/200
		total := 100 / math.Pow(r, w+float64(periods-1))
		for k := 0; k < periods; k++ {
			total += c / math.Pow(r, w+float64(k))
		}
		return total
	}

	lo, hi := -99.0, 1000.0
	if pv(lo) < dirty || pv(hi) > dirty {
		return 0, false
	}
	for i := 0; i < 200; i++ {
		mid := (lo + hi) / 2
		if pv(mid) > dirty {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2, true
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func num(s *string) *float64 {
	if s == nil {
		return nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(*s), 64)
	if err != nil {
		return nil
	}
	return &v
}
