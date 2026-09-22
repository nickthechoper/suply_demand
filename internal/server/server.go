// Package server exposes the dashboard's HTTP API and static frontend.
package server

import (
	"encoding/json"
	"log"
	"net/http"

	"supplydemand/internal/store"
	"supplydemand/internal/yahoo"
	"supplydemand/internal/zones"
)

type Config struct {
	Symbol       string
	Lookback     int // number of most recent bars used to compute supply/demand zones
	ChartBars    int // number of most recent bars served for display (>= Lookback, gives range buttons like 2Y room to work)
	ThresholdPct float64
	Resolution   int
	WebDir       string
	Peers        []string // peer tickers used for P/E comparison
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
	PERatio          *float64 `json:"peRatio"`
	PEGRatio         *float64 `json:"pegRatio"`
	RevenueGrowthPct *float64 `json:"revenueGrowthPct"`
	NetMarginPct     *float64 `json:"netMarginPct"`
}

type dataResponse struct {
	Symbol       string          `json:"symbol"`
	Bars         []barDTO        `json:"bars"`
	Zones        zonesDTO        `json:"zones"`
	Fundamentals fundamentalsDTO `json:"fundamentals"`
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/data", s.handleData)
	mux.HandleFunc("/api/company", s.handleCompany)
	mux.HandleFunc("/api/refresh", s.handleRefresh)
	mux.Handle("/", http.FileServer(http.Dir(s.config.WebDir)))
	return mux
}

// handleCompany returns the stored company profile, analyst targets,
// ownership, financials, filings, and peer comparison for the configured
// symbol.
func (s *Server) handleCompany(w http.ResponseWriter, r *http.Request) {
	data, _, ok, err := s.store.CompanyData(s.config.Symbol)
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
	displayLimit := s.config.ChartBars
	if displayLimit < s.config.Lookback {
		displayLimit = s.config.Lookback
	}

	bars, err := s.store.LatestBars(s.config.Symbol, displayLimit)
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
	zoneSourceBars := bars
	if len(zoneSourceBars) > s.config.Lookback {
		zoneSourceBars = zoneSourceBars[len(zoneSourceBars)-s.config.Lookback:]
	}
	zoneBars := make([]zones.Bar, len(zoneSourceBars))
	for i, b := range zoneSourceBars {
		zoneBars[i] = zones.Bar{High: b.High, Low: b.Low, Volume: b.Volume}
	}

	result := zones.Compute(zoneBars, zones.Params{
		ThresholdPct: s.config.ThresholdPct,
		Resolution:   s.config.Resolution,
	})

	profileDTOs := make([]profileBinDTO, len(result.Profile))
	for i, p := range result.Profile {
		profileDTOs[i] = profileBinDTO{Low: p.Low, High: p.High, Volume: p.Volume}
	}

	fundamentals, _, _, err := s.store.Fundamentals(s.config.Symbol)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := dataResponse{
		Symbol: s.config.Symbol,
		Bars:   barDTOs,
		Fundamentals: fundamentalsDTO{
			PERatio:          fundamentals.PERatio,
			PEGRatio:         fundamentals.PEGRatio,
			RevenueGrowthPct: fundamentals.RevenueGrowthPct,
			NetMarginPct:     fundamentals.NetMarginPct,
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

// handleRefresh re-fetches the full daily history and fundamentals for the
// configured symbol from Yahoo Finance and upserts them into the store.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	bars, err := yahoo.FetchDaily(s.config.Symbol, "5y")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := s.store.UpsertBars(s.config.Symbol, bars); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fundamentalsFetched := true
	fundamentals, err := yahoo.FetchFundamentals(s.config.Symbol)
	if err != nil {
		log.Printf("warning: could not refresh fundamentals: %v", err)
		fundamentalsFetched = false
	} else if err := s.store.SetFundamentals(s.config.Symbol, fundamentals); err != nil {
		log.Printf("warning: could not store refreshed fundamentals: %v", err)
		fundamentalsFetched = false
	}

	companyDataFetched := true
	companyData, err := yahoo.FetchCompanyData(s.config.Symbol, s.config.Peers)
	if err != nil {
		log.Printf("warning: could not refresh company data: %v", err)
		companyDataFetched = false
	} else if err := s.store.SetCompanyData(s.config.Symbol, companyData); err != nil {
		log.Printf("warning: could not store refreshed company data: %v", err)
		companyDataFetched = false
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"symbol":              s.config.Symbol,
		"barsFetched":         len(bars),
		"fundamentalsFetched": fundamentalsFetched,
		"companyDataFetched":  companyDataFetched,
	})
}
