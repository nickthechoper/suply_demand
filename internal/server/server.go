package server

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"supplydemand/internal/boj"
	"supplydemand/internal/bondhist"
	"supplydemand/internal/category"
	"supplydemand/internal/finra"
	"supplydemand/internal/openinsider"
	"supplydemand/internal/store"
	"supplydemand/internal/tic"
	"supplydemand/internal/yahoo"
	"supplydemand/internal/zones"
)

type Config struct {
	Symbols      []string
	Lookback     int
	ChartBars    int
	ThresholdPct float64
	Resolution   int
	WebDir       string
	Peers        map[string][]string
	DeepDemand   float64
}

type Server struct {
	store  *store.Store
	config Config
}

func New(st *store.Store, cfg Config) *Server {
	return &Server{store: st, config: cfg}
}

type barDTO struct {
	Time   int64   `json:"time"`
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

	DemandReached bool    `json:"demandReached"`
	DemandTop     float64 `json:"demandTop"`
	DemandBtm     float64 `json:"demandBtm"`

	EquilibriumAvg float64 `json:"equilibriumAvg"`

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
	mux.HandleFunc("/api/insider/recent", s.handleInsiderRecent)
	mux.HandleFunc("/api/insider/latest", s.handleInsiderLatest)
	mux.HandleFunc("/api/note", s.handleNote)
	mux.HandleFunc("/api/favorite", s.handleFavorite)
	mux.HandleFunc("/api/bonds", s.handleBonds)
	mux.HandleFunc("/api/bonds/history", s.handleBondHistory)
	mux.HandleFunc("/api/japan-flows", s.handleJapanFlows)
	mux.HandleFunc("/api/us-flows", s.handleUSFlows)
	files := http.FileServer(http.Dir(s.config.WebDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			http.Redirect(w, r, "/list.html", http.StatusFound)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
	return mux
}

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
			Max:            result.Max,
			Min:            result.Min,
			SupplyReached:  result.SupplyReached,
			SupplyTop:      result.SupplyTop,
			SupplyBtm:      result.SupplyBtm,
			DemandReached:  result.DemandReached,
			DemandTop:      result.DemandTop,
			DemandBtm:      result.DemandBtm,
			EquilibriumAvg: result.EquilibriumAvg,
			Profile:        profileDTOs,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("encoding /api/data response: %v", err)
	}
}

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

	bondsFetched := true
	name, err := s.store.SymbolName(symbol)
	if err != nil {
		log.Printf("looking up name for %s: %v", symbol, err)
	}
	if bonds, err := finra.Fetch(symbol, name); err != nil {
		log.Printf("warning: could not refresh bonds: %v", err)
		bondsFetched = false
	} else if err := s.store.SetBonds(symbol, bonds); err != nil {
		log.Printf("warning: could not store refreshed bonds: %v", err)
		bondsFetched = false
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"symbol":               symbol,
		"barsFetched":          len(bars),
		"fundamentalsFetched":  fundamentalsFetched,
		"companyDataFetched":   companyDataFetched,
		"insiderTradesFetched": insiderTradesFetched,
		"bondsFetched":         bondsFetched,
	})
}

type screenerRowDTO struct {
	Symbol             string   `json:"symbol"`
	Name               string   `json:"name"`
	Price              float64  `json:"price"`
	DailyChangePct     *float64 `json:"dailyChangePct"`
	Zone               string   `json:"zone"`
	DeepDemand         bool     `json:"deepDemand"`
	ZonePct            *float64 `json:"zonePct"`
	PEGRatio           *float64 `json:"pegRatio"`
	ROICPct            *float64 `json:"roicPct"`
	PSRatio            *float64 `json:"psRatio"`
	PERatio            *float64 `json:"peRatio"`
	RevenueGrowthPct   *float64 `json:"revenueGrowthPct"`
	NetMarginPct       *float64 `json:"netMarginPct"`
	RecommendationMean *float64 `json:"recommendationMean"`
	RecommendationKey  string   `json:"recommendationKey"`
	TargetMean         *float64 `json:"targetMean"`
	UpsidePct          *float64 `json:"upsidePct"`
	Flags              []string `json:"flags"`
	NetOpenMarket      *float64 `json:"netOpenMarket"`
	BondYieldPct       *float64 `json:"bondYieldPct"`
	BondYieldChange    *float64 `json:"bondYieldChange"`
	BondLabel          string   `json:"bondLabel"`
	Note               string   `json:"note"`
	Favorite           bool     `json:"favorite"`
	Sector             string   `json:"sector"`
	Industry           string   `json:"industry"`
	AITheme            string   `json:"aiTheme"`
}

type flagInput struct {
	NumAnalysts           *float64
	PERatio               *float64
	RevenueGrowthPct      *float64
	EarningsGrowthPct     *float64
	NetMarginPct          *float64
	FreeCashflow          *float64
	NetDebtToMarketCapPct *float64
	CurrentRatio          *float64
	BondYieldPct          *float64
}

const highBondYieldPct = 10

func flags(in flagInput) []string {
	var out []string
	if in.NetMarginPct != nil && *in.NetMarginPct < 0 {
		out = append(out, "Unprofitable")
	}
	if in.FreeCashflow != nil && *in.FreeCashflow < 0 {
		out = append(out, "Negative free cash flow")
	}
	if in.NetDebtToMarketCapPct != nil && *in.NetDebtToMarketCapPct > 50 {
		out = append(out, "Heavy net debt load")
	}
	if in.CurrentRatio != nil && *in.CurrentRatio < 1 {
		out = append(out, "Weak liquidity")
	}
	if in.BondYieldPct != nil && *in.BondYieldPct >= highBondYieldPct {
		out = append(out, "High bond yield")
	}
	if in.EarningsGrowthPct != nil && in.RevenueGrowthPct != nil && *in.EarningsGrowthPct < *in.RevenueGrowthPct-20 {
		out = append(out, "Earnings growth lagging revenue")
	}
	if in.NumAnalysts != nil && *in.NumAnalysts < 3 {
		out = append(out, "Thin analyst coverage")
	}
	if in.PERatio != nil && *in.PERatio > 0 && *in.PERatio < 5 {
		out = append(out, "Possible value trap")
	}
	return out
}

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

		var zonePct *float64
		if span := result.Max - result.Min; span > 0 {
			v := (last.Close - result.Min) / span * 100
			zonePct = &v
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

		var numAnalysts, freeCashflow, netDebtToMarketCapPct, currentRatio, earningsGrowthPct *float64
		if ok {
			marketCap := companyData.OwnMarketCap(info.Symbol)
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

		var dailyChangePct *float64
		if len(bars) >= 2 && bars[len(bars)-2].Close > 0 {
			v := (last.Close - bars[len(bars)-2].Close) / bars[len(bars)-2].Close * 100
			dailyChangePct = &v
		}

		var upsidePct *float64
		if targetMean != nil && *targetMean > 0 && last.Close > 0 {
			v := (*targetMean - last.Close) / last.Close * 100
			upsidePct = &v
		}

		var netOpenMarket *float64
		if trades, _, ok, err := s.store.InsiderTrades(info.Symbol); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if ok {
			net := openinsider.NetOpenMarket(openinsider.Within(trades, 365))
			netOpenMarket = &net
		}

		var bondYieldPct, bondYieldChange *float64
		var bondLabel string
		if bonds, _, ok, err := s.store.Bonds(info.Symbol); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if ok {
			if b, found := finra.Headline(bonds, time.Now().UTC()); found {
				bondYieldPct, bondYieldChange, bondLabel = b.YieldPct, b.YieldChange, b.Label()
			}
		}

		row := screenerRowDTO{
			Symbol:             info.Symbol,
			Name:               info.Name,
			Price:              last.Close,
			DailyChangePct:     dailyChangePct,
			Zone:               zone,
			DeepDemand:         deepDemand,
			ZonePct:            zonePct,
			PEGRatio:           fundamentals.PEGRatio,
			ROICPct:            fundamentals.ROICPct,
			PSRatio:            fundamentals.PSRatio,
			PERatio:            fundamentals.PERatio,
			RevenueGrowthPct:   fundamentals.RevenueGrowthPct,
			NetMarginPct:       fundamentals.NetMarginPct,
			RecommendationMean: recommendationMean,
			RecommendationKey:  recommendationKey,
			TargetMean:         targetMean,
			UpsidePct:          upsidePct,
			Flags: flags(flagInput{
				NumAnalysts:           numAnalysts,
				PERatio:               fundamentals.PERatio,
				RevenueGrowthPct:      fundamentals.RevenueGrowthPct,
				EarningsGrowthPct:     earningsGrowthPct,
				NetMarginPct:          fundamentals.NetMarginPct,
				FreeCashflow:          freeCashflow,
				NetDebtToMarketCapPct: netDebtToMarketCapPct,
				CurrentRatio:          currentRatio,
				BondYieldPct:          bondYieldPct,
			}),
			NetOpenMarket:   netOpenMarket,
			BondYieldPct:    bondYieldPct,
			BondYieldChange: bondYieldChange,
			BondLabel:       bondLabel,
			Note:            info.Note,
			Favorite:        info.Favorite,
			AITheme:         category.Theme(info.Symbol),
		}
		if ok {
			row.Sector = companyData.Profile.Sector
			row.Industry = companyData.Profile.Industry
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

func (s *Server) handleFavorite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Symbol   string `json:"symbol"`
		Favorite bool   `json:"favorite"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	symbol := strings.ToUpper(strings.TrimSpace(body.Symbol))
	found, err := s.store.SetSymbolFavorite(symbol, body.Favorite)
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

func (s *Server) handleBonds(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("symbol")))
	if !tickerRe.MatchString(symbol) {
		http.Error(w, fmt.Sprintf("invalid symbol %q", symbol), http.StatusBadRequest)
		return
	}

	bonds, fetchedAt, err := s.bonds(symbol)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	var headline string
	if b, found := finra.Headline(bonds, time.Now().UTC()); found {
		headline = b.Symbol
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"symbol":    symbol,
		"bonds":     bonds,
		"headline":  headline,
		"fetchedAt": fetchedAt.Unix(),
	}); err != nil {
		log.Printf("encoding /api/bonds response: %v", err)
	}
}

func (s *Server) bonds(symbol string) ([]finra.Bond, time.Time, error) {
	bonds, fetchedAt, ok, err := s.store.Bonds(symbol)
	if err != nil || ok {
		return bonds, fetchedAt, err
	}
	name, err := s.store.SymbolName(symbol)
	if err != nil {
		log.Printf("looking up name for %s: %v", symbol, err)
	}
	if bonds, err = finra.Fetch(symbol, name); err != nil {
		return nil, time.Time{}, err
	}
	if s.tracked(symbol) {
		if err := s.store.SetBonds(symbol, bonds); err != nil {
			log.Printf("storing bonds for %s: %v", symbol, err)
		}
	}
	return bonds, time.Now(), nil
}

func (s *Server) handleBondHistory(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("symbol")))
	if !tickerRe.MatchString(symbol) {
		http.Error(w, fmt.Sprintf("invalid symbol %q", symbol), http.StatusBadRequest)
		return
	}
	bonds, _, err := s.bonds(symbol)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	want := r.URL.Query().Get("bond")
	var bond finra.Bond
	found := false
	if want == "" {
		bond, found = finra.Headline(bonds, time.Now().UTC())
	} else {
		for _, b := range bonds {
			if b.Symbol == want {
				bond, found = b, true
				break
			}
		}
	}
	if !found {
		http.Error(w, fmt.Sprintf("no bond %q for %s", want, symbol), http.StatusNotFound)
		return
	}

	history, err := bondhist.Sync(s.store, bond)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"symbol":  symbol,
		"bond":    bond.Symbol,
		"label":   bond.Label(),
		"history": history,
	}); err != nil {
		log.Printf("encoding /api/bonds/history response: %v", err)
	}
}

func (s *Server) handleJapanFlows(w http.ResponseWriter, r *http.Request) {
	flows, err := boj.JapaneseDebtFlows()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(flows); err != nil {
		log.Printf("encoding /api/japan-flows response: %v", err)
	}
}

func (s *Server) handleUSFlows(w http.ResponseWriter, r *http.Request) {
	flows, err := tic.USForeignBondFlows()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(flows); err != nil {
		log.Printf("encoding /api/us-flows response: %v", err)
	}
}

type recentTradeDTO struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
	openinsider.Trade
}

func (s *Server) handleInsiderRecent(w http.ResponseWriter, r *http.Request) {
	days := 30
	if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && d > 0 && d <= openinsider.WindowDays {
		days = d
	}
	infos, err := s.store.ListSymbolInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := []recentTradeDTO{}
	var oldest time.Time
	for _, info := range infos {
		trades, fetchedAt, ok, err := s.store.InsiderTrades(info.Symbol)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			continue
		}
		if oldest.IsZero() || fetchedAt.Before(oldest) {
			oldest = fetchedAt
		}
		for _, t := range openinsider.Within(trades, days) {
			out = append(out, recentTradeDTO{Symbol: info.Symbol, Name: info.Name, Trade: t})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TradeDate != out[j].TradeDate {
			return out[i].TradeDate > out[j].TradeDate
		}
		return out[i].FilingDate > out[j].FilingDate
	})
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"days":      days,
		"trades":    out,
		"tickers":   len(infos),
		"fetchedAt": oldest.Unix(),
	}); err != nil {
		log.Printf("encoding /api/insider/recent response: %v", err)
	}
}

func (s *Server) handleInsiderLatest(w http.ResponseWriter, r *http.Request) {
	days := 3
	if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && d > 0 && d <= 30 {
		days = d
	}
	trades, fetchedAt, err := openinsider.Latest(days)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"days":      days,
		"trades":    trades,
		"fetchedAt": fetchedAt.Unix(),
	}); err != nil {
		log.Printf("encoding /api/insider/latest response: %v", err)
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
