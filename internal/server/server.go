// Package server exposes the dashboard's HTTP API and static frontend.
package server

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"supplydemand/internal/category"
	"supplydemand/internal/openinsider"
	"supplydemand/internal/score"
	"supplydemand/internal/store"
	"supplydemand/internal/yahoo"
	"supplydemand/internal/zones"
)

type Config struct {
	Symbols      []string // tracked tickers; the first is the default
	Lookback     int      // number of most recent bars used to compute supply/demand zones
	ChartBars    int      // number of most recent bars served for display (>= Lookback, gives range buttons like 2Y room to work)
	ThresholdPct float64
	Resolution   int
	WebDir       string
	Peers        map[string][]string // peer tickers used for P/E comparison, keyed by symbol
	DeepDemand   float64             // fraction of the demand zone, measured up from its bottom, that counts as its low end
}

type Server struct {
	store  *store.Store
	config Config
}

func New(st *store.Store, cfg Config) *Server {
	return &Server{store: st, config: cfg}
}

type barDTO struct {
	Time   int64   `json:"time"` // unix seconds, UTC
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

type profileBinDTO struct {
	Low    float64 `json:"low"`
	High   float64 `json:"high"`
	Volume float64 `json:"volume"`
}

type zonesDTO struct {
	Max float64 `json:"max"`
	Min float64 `json:"min"`

	SupplyReached bool    `json:"supplyReached"`
	SupplyTop     float64 `json:"supplyTop"`
	SupplyBtm     float64 `json:"supplyBtm"`
	SupplyAvg     float64 `json:"supplyAvg"`
	SupplyWavg    float64 `json:"supplyWavg"`

	DemandReached bool    `json:"demandReached"`
	DemandTop     float64 `json:"demandTop"`
	DemandBtm     float64 `json:"demandBtm"`
	DemandAvg     float64 `json:"demandAvg"`
	DemandWavg    float64 `json:"demandWavg"`

	EquilibriumAvg  float64 `json:"equilibriumAvg"`
	EquilibriumWavg float64 `json:"equilibriumWavg"`

	Profile []profileBinDTO `json:"profile"`
}

type fundamentalsDTO struct {
	PERatio            *float64 `json:"peRatio"`
	ForwardPERatio     *float64 `json:"forwardPERatio"`
	PEGRatio           *float64 `json:"pegRatio"`
	RevenueGrowthPct   *float64 `json:"revenueGrowthPct"`
	NetMarginPct       *float64 `json:"netMarginPct"`
	OperatingMarginPct *float64 `json:"operatingMarginPct"`
}

type dataResponse struct {
	Symbol       string          `json:"symbol"`
	Bars         []barDTO        `json:"bars"`
	LookbackBars int             `json:"lookbackBars"`
	Zones        zonesDTO        `json:"zones"`
	Fundamentals fundamentalsDTO `json:"fundamentals"`
	AITheme      string          `json:"aiTheme"`
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/symbols", s.handleSymbols)
	mux.HandleFunc("/api/data", s.handleData)
	mux.HandleFunc("/api/company", s.handleCompany)
	mux.HandleFunc("/api/refresh", s.handleRefresh)
	mux.HandleFunc("/api/screener", s.handleScreener)
	mux.HandleFunc("/api/insider", s.handleInsider)
	mux.HandleFunc("/api/note", s.handleNote)
	files := http.FileServer(http.Dir(s.config.WebDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			http.Redirect(w, r, "/list.html", http.StatusFound)
			return
		}
		files.ServeHTTP(w, r)
	})
	return mux
}

// zonesFromBars computes supply/demand/equilibrium zones from the trailing
// Lookback bars of the given (oldest-to-newest) bar slice.
func (s *Server) zonesFromBars(bars []yahoo.Bar) zones.Result {
	zoneSourceBars := bars
	if len(zoneSourceBars) > s.config.Lookback {
		zoneSourceBars = zoneSourceBars[len(zoneSourceBars)-s.config.Lookback:]
	}
	zoneBars := make([]zones.Bar, len(zoneSourceBars))
	for i, b := range zoneSourceBars {
		zoneBars[i] = zones.Bar{High: b.High, Low: b.Low, Volume: b.Volume}
	}
	return zones.Compute(zoneBars, zones.Params{
		ThresholdPct: s.config.ThresholdPct,
		Resolution:   s.config.Resolution,
	})
}

// symbolFor resolves the requested ticker from the "symbol" query param
// against the configured symbol list, defaulting to the first configured
// symbol when the param is absent. ok is false if an unknown symbol was requested.
func (s *Server) symbolFor(r *http.Request) (symbol string, ok bool) {
	requested := r.URL.Query().Get("symbol")
	if requested == "" {
		return s.config.Symbols[0], true
	}
	for _, sym := range s.config.Symbols {
		if sym == requested {
			return sym, true
		}
	}
	return "", false
}

type symbolDTO struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

func (s *Server) handleSymbols(w http.ResponseWriter, r *http.Request) {
	infos, err := s.store.ListSymbolInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	symbols := make([]symbolDTO, len(infos))
	for i, info := range infos {
		symbols[i] = symbolDTO{Symbol: info.Symbol, Name: info.Name}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"symbols": symbols})
}

// handleCompany returns the stored company profile, analyst targets,
// ownership, financials, filings, and peer comparison for the requested
// symbol.
func (s *Server) handleCompany(w http.ResponseWriter, r *http.Request) {
	symbol, ok := s.symbolFor(r)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown symbol %q", r.URL.Query().Get("symbol")), http.StatusBadRequest)
		return
	}
	data, _, ok, err := s.store.CompanyData(symbol)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "no company data available for symbol; try /api/refresh first", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("encoding /api/company response: %v", err)
	}
}

func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	symbol, ok := s.symbolFor(r)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown symbol %q", r.URL.Query().Get("symbol")), http.StatusBadRequest)
		return
	}

	displayLimit := s.config.ChartBars
	if displayLimit < s.config.Lookback {
		displayLimit = s.config.Lookback
	}

	bars, err := s.store.LatestBars(symbol, displayLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(bars) == 0 {
		http.Error(w, "no data available for symbol; try /api/refresh first", http.StatusNotFound)
		return
	}

	barDTOs := make([]barDTO, len(bars))
	for i, b := range bars {
		barDTOs[i] = barDTO{
			Time:   b.Time.Unix(),
			Open:   b.Open,
			High:   b.High,
			Low:    b.Low,
			Close:  b.Close,
			Volume: b.Volume,
		}
	}

	// Zones are computed off the most recent Lookback bars, which may be a
	// tail slice of the larger set served for display.
	result := s.zonesFromBars(bars)

	profileDTOs := make([]profileBinDTO, len(result.Profile))
	for i, p := range result.Profile {
		profileDTOs[i] = profileBinDTO{Low: p.Low, High: p.High, Volume: p.Volume}
	}

	fundamentals, _, _, err := s.store.Fundamentals(symbol)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := dataResponse{
		Symbol:       symbol,
		Bars:         barDTOs,
		LookbackBars: min(len(bars), s.config.Lookback),
		AITheme:      category.Theme(symbol),
		Fundamentals: fundamentalsDTO{
			PERatio:            fundamentals.PERatio,
			ForwardPERatio:     fundamentals.ForwardPERatio,
			PEGRatio:           fundamentals.PEGRatio,
			RevenueGrowthPct:   fundamentals.RevenueGrowthPct,
			NetMarginPct:       fundamentals.NetMarginPct,
			OperatingMarginPct: fundamentals.OperatingMarginPct,
		},
		Zones: zonesDTO{
			Max:             result.Max,
			Min:             result.Min,
			SupplyReached:   result.SupplyReached,
			SupplyTop:       result.SupplyTop,
			SupplyBtm:       result.SupplyBtm,
			SupplyAvg:       result.SupplyAvg,
			SupplyWavg:      result.SupplyWavg,
			DemandReached:   result.DemandReached,
			DemandTop:       result.DemandTop,
			DemandBtm:       result.DemandBtm,
			DemandAvg:       result.DemandAvg,
			DemandWavg:      result.DemandWavg,
			EquilibriumAvg:  result.EquilibriumAvg,
			EquilibriumWavg: result.EquilibriumWavg,
			Profile:         profileDTOs,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("encoding /api/data response: %v", err)
	}
}

// handleRefresh re-fetches the full daily history, fundamentals and company
// data from Yahoo Finance and insider trades from OpenInsider for the
// requested symbol, and upserts them into the store.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	symbol, ok := s.symbolFor(r)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown symbol %q", r.URL.Query().Get("symbol")), http.StatusBadRequest)
		return
	}

	bars, err := yahoo.FetchDaily(symbol, "5y")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := s.store.UpsertBars(symbol, bars); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fundamentalsFetched := true
	fundamentals, err := yahoo.FetchFundamentals(symbol)
	if err != nil {
		log.Printf("warning: could not refresh fundamentals: %v", err)
		fundamentalsFetched = false
	} else if err := s.store.SetFundamentals(symbol, fundamentals); err != nil {
		log.Printf("warning: could not store refreshed fundamentals: %v", err)
		fundamentalsFetched = false
	}

	companyDataFetched := true
	companyData, err := yahoo.FetchCompanyData(symbol, s.config.Peers[symbol])
	if err != nil {
		log.Printf("warning: could not refresh company data: %v", err)
		companyDataFetched = false
	} else if err := s.store.SetCompanyData(symbol, companyData); err != nil {
		log.Printf("warning: could not store refreshed company data: %v", err)
		companyDataFetched = false
	} else if name := companyData.OwnName(symbol); name != "" {
		if err := s.store.SetSymbolName(symbol, name); err != nil {
			log.Printf("warning: could not store refreshed company name: %v", err)
		}
	}

	insiderTradesFetched := true
	trades, err := openinsider.Fetch(symbol, openinsider.WindowDays)
	if err != nil {
		log.Printf("warning: could not refresh insider trades: %v", err)
		insiderTradesFetched = false
	} else if err := s.store.SetInsiderTrades(symbol, trades); err != nil {
		log.Printf("warning: could not store refreshed insider trades: %v", err)
		insiderTradesFetched = false
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"symbol":               symbol,
		"barsFetched":          len(bars),
		"fundamentalsFetched":  fundamentalsFetched,
		"companyDataFetched":   companyDataFetched,
		"insiderTradesFetched": insiderTradesFetched,
	})
}

type screenerRowDTO struct {
	Symbol             string            `json:"symbol"`
	Name               string            `json:"name"`
	Price              float64           `json:"price"`
	ChangePct          float64           `json:"changePct"`
	Zone               string            `json:"zone"` // "supply", "demand", or "none"
	DeepDemand         bool              `json:"deepDemand"`
	PEGRatio           *float64          `json:"pegRatio"`
	ROICPct            *float64          `json:"roicPct"`
	PSRatio            *float64          `json:"psRatio"`
	PERatio            *float64          `json:"peRatio"`
	RevenueGrowthPct   *float64          `json:"revenueGrowthPct"`
	NetMarginPct       *float64          `json:"netMarginPct"`
	Score              float64           `json:"score"`
	Qualified          bool              `json:"qualified"`
	ScoreBreakdown     []score.Component `json:"scoreBreakdown"`
	RecommendationMean *float64          `json:"recommendationMean"`
	RecommendationKey  string            `json:"recommendationKey"`
	TargetMean         *float64          `json:"targetMean"`
	UpsidePct          *float64          `json:"upsidePct"`
	Flags              []string          `json:"flags"`
	NetOpenMarket      *float64          `json:"netOpenMarket"`
	Note               string            `json:"note"`
	Sector             string            `json:"sector"`
	Industry           string            `json:"industry"`
	AITheme            string            `json:"aiTheme"`
}

// handleScreener returns every tracked symbol's latest price and current
// supply/demand zone status, for the spreadsheet-style list view.
func (s *Server) handleScreener(w http.ResponseWriter, r *http.Request) {
	infos, err := s.store.ListSymbolInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rows := make([]screenerRowDTO, 0, len(infos))
	for _, info := range infos {
		bars, err := s.store.LatestBars(info.Symbol, s.config.Lookback)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(bars) == 0 {
			continue
		}

		result := s.zonesFromBars(bars)
		last := bars[len(bars)-1]

		zone := "none"
		switch {
		case result.SupplyReached && last.Close >= result.SupplyBtm && last.Close <= result.SupplyTop:
			zone = "supply"
		case result.DemandReached && last.Close >= result.DemandBtm && last.Close <= result.DemandTop:
			zone = "demand"
		}

		deepDemand := zone == "demand" &&
			last.Close <= result.DemandBtm+(result.DemandTop-result.DemandBtm)*s.config.DeepDemand

		companyData, _, ok, err := s.store.CompanyData(info.Symbol)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var recommendationMean, targetMean *float64
		var recommendationKey string
		if ok {
			recommendationMean = companyData.Analyst.RecommendationMean
			recommendationKey = companyData.Analyst.RecommendationKey
			targetMean = companyData.Analyst.TargetMean
		}

		fundamentals, _, _, err := s.store.Fundamentals(info.Symbol)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var zonePosition *float64
		if result.SupplyReached && result.DemandReached && result.SupplyAvg > result.DemandAvg {
			pos := (last.Close - result.DemandAvg) / (result.SupplyAvg - result.DemandAvg)
			zonePosition = &pos
		}

		var marketCap, numAnalysts, freeCashflow, netDebtToMarketCapPct, currentRatio, earningsGrowthPct *float64
		if ok {
			marketCap = companyData.OwnMarketCap(info.Symbol)
			numAnalysts = companyData.Analyst.NumAnalysts
			freeCashflow = companyData.CashFlow.FreeCashflow
			currentRatio = companyData.BalanceSheet.CurrentRatio

			if marketCap != nil && *marketCap > 0 &&
				companyData.BalanceSheet.TotalDebt != nil && companyData.BalanceSheet.TotalCash != nil {
				pct := (*companyData.BalanceSheet.TotalDebt - *companyData.BalanceSheet.TotalCash) / *marketCap * 100
				netDebtToMarketCapPct = &pct
			}

			if len(companyData.Financials) >= 2 {
				latest, prior := companyData.Financials[0], companyData.Financials[1]
				if latest.NetIncome != nil && prior.NetIncome != nil && *prior.NetIncome != 0 {
					pct := (*latest.NetIncome - *prior.NetIncome) / math.Abs(*prior.NetIncome) * 100
					earningsGrowthPct = &pct
				}
			}
		}

		var netOpenMarket *float64
		if trades, _, ok, err := s.store.InsiderTrades(info.Symbol); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if ok {
			net := openinsider.NetOpenMarket(openinsider.Within(trades, 365))
			netOpenMarket = &net
		}

		scoreResult := score.Compute(score.Input{
			Price:                 last.Close,
			ZonePosition:          zonePosition,
			RecommendationMean:    recommendationMean,
			TargetMean:            targetMean,
			NumAnalysts:           numAnalysts,
			PERatio:               fundamentals.PERatio,
			ForwardPERatio:        fundamentals.ForwardPERatio,
			PEGRatio:              fundamentals.PEGRatio,
			PSRatio:               fundamentals.PSRatio,
			RevenueGrowthPct:      fundamentals.RevenueGrowthPct,
			EarningsGrowthPct:     earningsGrowthPct,
			NetMarginPct:          fundamentals.NetMarginPct,
			OperatingMarginPct:    fundamentals.OperatingMarginPct,
			ROICPct:               fundamentals.ROICPct,
			MarketCap:             marketCap,
			FreeCashflow:          freeCashflow,
			NetDebtToMarketCapPct: netDebtToMarketCapPct,
			CurrentRatio:          currentRatio,
			NetInsiderOpenMarket:  netOpenMarket,
		})

		row := screenerRowDTO{
			Symbol:             info.Symbol,
			Name:               info.Name,
			Price:              last.Close,
			Zone:               zone,
			DeepDemand:         deepDemand,
			PEGRatio:           fundamentals.PEGRatio,
			ROICPct:            fundamentals.ROICPct,
			PSRatio:            fundamentals.PSRatio,
			PERatio:            fundamentals.PERatio,
			RevenueGrowthPct:   fundamentals.RevenueGrowthPct,
			NetMarginPct:       fundamentals.NetMarginPct,
			Score:              scoreResult.Total,
			Qualified:          scoreResult.Qualified,
			ScoreBreakdown:     scoreResult.Components,
			RecommendationMean: recommendationMean,
			RecommendationKey:  recommendationKey,
			TargetMean:         targetMean,
			UpsidePct:          scoreResult.UpsidePct,
			Flags:              scoreResult.Flags,
			NetOpenMarket:      netOpenMarket,
			Note:               info.Note,
			AITheme:            category.Theme(info.Symbol),
		}
		if ok {
			row.Sector = companyData.Profile.Sector
			row.Industry = companyData.Profile.Industry
		}
		if len(bars) > 1 {
			prev := bars[len(bars)-2]
			if prev.Close != 0 {
				row.ChangePct = (last.Close - prev.Close) / prev.Close * 100
			}
		}

		rows = append(rows, row)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"rows": rows, "deepDemandPct": s.config.DeepDemand * 100, "aiThemes": category.Themes()}); err != nil {
		log.Printf("encoding /api/screener response: %v", err)
	}
}

var tickerRe = regexp.MustCompile(`^[A-Z0-9.\-]{1,12}$`)

func (s *Server) handleNote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Symbol string `json:"symbol"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	symbol := strings.ToUpper(strings.TrimSpace(body.Symbol))
	found, err := s.store.SetSymbolNote(symbol, strings.TrimSpace(body.Note))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, fmt.Sprintf("unknown symbol %q", symbol), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleInsider(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("symbol")))
	if !tickerRe.MatchString(symbol) {
		http.Error(w, fmt.Sprintf("invalid symbol %q", symbol), http.StatusBadRequest)
		return
	}
	days := 365
	if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && d > 0 && d <= 3650 {
		days = d
	}

	var prices []pricePointDTO
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		prices = s.insiderPrices(symbol, days)
	}()
	trades, fetchedAt, err := s.insiderTrades(symbol, days)
	wg.Wait()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	name, err := s.store.SymbolName(symbol)
	if err != nil {
		log.Printf("looking up name for %s: %v", symbol, err)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"symbol":    symbol,
		"name":      name,
		"days":      days,
		"trades":    trades,
		"prices":    prices,
		"fetchedAt": fetchedAt,
	}); err != nil {
		log.Printf("encoding /api/insider response: %v", err)
	}
}

func (s *Server) insiderTrades(symbol string, days int) ([]openinsider.Trade, int64, error) {
	if days > openinsider.WindowDays || !s.tracked(symbol) {
		trades, err := openinsider.FetchTrades(symbol, days)
		return trades, 0, err
	}

	trades, fetchedAt, ok, err := s.store.InsiderTrades(symbol)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		if trades, err = openinsider.Fetch(symbol, openinsider.WindowDays); err != nil {
			return nil, 0, err
		}
		if err := s.store.SetInsiderTrades(symbol, trades); err != nil {
			log.Printf("storing insider trades for %s: %v", symbol, err)
		}
		fetchedAt = time.Now()
	}
	return openinsider.Within(trades, days), fetchedAt.Unix(), nil
}

func (s *Server) tracked(symbol string) bool {
	for _, sym := range s.config.Symbols {
		if sym == symbol {
			return true
		}
	}
	return false
}

type pricePointDTO struct {
	Date  string  `json:"date"`
	Close float64 `json:"close"`
}

func (s *Server) insiderPrices(symbol string, days int) []pricePointDTO {
	cutoff := time.Now().UTC().AddDate(0, 0, -days)

	bars, err := s.store.LatestBars(symbol, days)
	if err != nil {
		log.Printf("loading stored bars for %s: %v", symbol, err)
	}
	if len(bars) == 0 || bars[0].Time.After(cutoff) || time.Since(bars[len(bars)-1].Time) > 7*24*time.Hour {
		if bars, err = yahoo.FetchDaily(symbol, yahooRange(days)); err != nil {
			log.Printf("fetching prices for %s insider view: %v", symbol, err)
			return []pricePointDTO{}
		}
	}

	prices := []pricePointDTO{}
	for _, b := range bars {
		if b.Time.Before(cutoff) || b.Close == 0 {
			continue
		}
		prices = append(prices, pricePointDTO{Date: b.Time.Format("2006-01-02"), Close: b.Close})
	}
	return prices
}

func yahooRange(days int) string {
	switch {
	case days <= 365:
		return "1y"
	case days <= 730:
		return "2y"
	case days <= 1825:
		return "5y"
	default:
		return "10y"
	}
}
