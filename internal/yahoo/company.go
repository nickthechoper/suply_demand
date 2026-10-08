package yahoo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type AnalystTarget struct {
	TargetHigh   *float64 `json:"targetHigh"`
	TargetLow    *float64 `json:"targetLow"`
	TargetMean   *float64 `json:"targetMean"`
	TargetMedian *float64 `json:"targetMedian"`
	NumAnalysts  *float64 `json:"numAnalysts"`
}

type Holder struct {
	Organization string   `json:"organization"`
	PctHeld      *float64 `json:"pctHeld"`
	Value        *float64 `json:"value"`
}

type InsiderTransaction struct {
	FilerName string   `json:"filerName"`
	Relation  string   `json:"relation"`
	Shares    *float64 `json:"shares"`
	Date      string   `json:"date"`
	Text      string   `json:"text"`
}

type NetInsiderActivity struct {
	Period    string   `json:"period"`
	NetShares *float64 `json:"netShares"`
}

type Ownership struct {
	InsidersPct         *float64             `json:"insidersPct"`
	InstitutionsPct     *float64             `json:"institutionsPct"`
	InstitutionsCount   *float64             `json:"institutionsCount"`
	TopInstitutions     []Holder             `json:"topInstitutions"`
	TopFunds            []Holder             `json:"topFunds"`
	InsiderTransactions []InsiderTransaction `json:"insiderTransactions"`
	NetInsiderActivity  NetInsiderActivity   `json:"netInsiderActivity"`
}

type YearlyFinancials struct {
	EndDate      string   `json:"endDate"`
	TotalRevenue *float64 `json:"totalRevenue"`
	NetIncome    *float64 `json:"netIncome"`
}

type BalanceSheetSnapshot struct {
	TotalCash    *float64 `json:"totalCash"`
	TotalDebt    *float64 `json:"totalDebt"`
	CurrentRatio *float64 `json:"currentRatio"`
	QuickRatio   *float64 `json:"quickRatio"`
}

type CashFlowSnapshot struct {
	OperatingCashflow *float64 `json:"operatingCashflow"`
	FreeCashflow      *float64 `json:"freeCashflow"`
}

type TTMStatement struct {
	Revenue         *float64 `json:"revenue"`
	EBIT            *float64 `json:"ebit"`
	InterestExpense *float64 `json:"interestExpense"`
}

type SecFiling struct {
	Date  string `json:"date"`
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type CompanyProfile struct {
	Sector    string   `json:"sector"`
	Industry  string   `json:"industry"`
	Employees *float64 `json:"employees"`
	Website   string   `json:"website"`
	Address   string   `json:"address"`
	Summary   string   `json:"summary"`
}

type CompanyData struct {
	Profile      CompanyProfile       `json:"profile"`
	Analyst      AnalystTarget        `json:"analyst"`
	Ownership    Ownership            `json:"ownership"`
	Financials   []YearlyFinancials   `json:"financials"`
	BalanceSheet BalanceSheetSnapshot `json:"balanceSheet"`
	CashFlow     CashFlowSnapshot     `json:"cashFlow"`
	TTM          TTMStatement         `json:"ttm"`
	Filings      []SecFiling          `json:"filings"`
	Peers        []PeerQuote          `json:"peers"`
}

func (d CompanyData) OwnName(symbol string) string {
	for _, p := range d.Peers {
		if p.Symbol == symbol {
			return p.Name
		}
	}
	return ""
}

func (d CompanyData) OwnMarketCap(symbol string) *float64 {
	for _, p := range d.Peers {
		if p.Symbol == symbol {
			return p.MarketCap
		}
	}
	return nil
}

type companyDataResponse struct {
	QuoteSummary struct {
		Result []struct {
			AssetProfile struct {
				Sector              string   `json:"sector"`
				Industry            string   `json:"industry"`
				FullTimeEmployees   *float64 `json:"fullTimeEmployees"`
				Website             string   `json:"website"`
				Address1            string   `json:"address1"`
				City                string   `json:"city"`
				State               string   `json:"state"`
				Country             string   `json:"country"`
				LongBusinessSummary string   `json:"longBusinessSummary"`
			} `json:"assetProfile"`
			FinancialData struct {
				TargetHighPrice         quoteSummaryFloat `json:"targetHighPrice"`
				TargetLowPrice          quoteSummaryFloat `json:"targetLowPrice"`
				TargetMeanPrice         quoteSummaryFloat `json:"targetMeanPrice"`
				TargetMedianPrice       quoteSummaryFloat `json:"targetMedianPrice"`
				NumberOfAnalystOpinions quoteSummaryFloat `json:"numberOfAnalystOpinions"`
				TotalCash               quoteSummaryFloat `json:"totalCash"`
				TotalDebt               quoteSummaryFloat `json:"totalDebt"`
				CurrentRatio            quoteSummaryFloat `json:"currentRatio"`
				QuickRatio              quoteSummaryFloat `json:"quickRatio"`
				OperatingCashflow       quoteSummaryFloat `json:"operatingCashflow"`
				FreeCashflow            quoteSummaryFloat `json:"freeCashflow"`
			} `json:"financialData"`
			MajorHoldersBreakdown struct {
				InsidersPercentHeld     quoteSummaryFloat `json:"insidersPercentHeld"`
				InstitutionsPercentHeld quoteSummaryFloat `json:"institutionsPercentHeld"`
				InstitutionsCount       quoteSummaryFloat `json:"institutionsCount"`
			} `json:"majorHoldersBreakdown"`
			InstitutionOwnership struct {
				OwnershipList []ownershipEntry `json:"ownershipList"`
			} `json:"institutionOwnership"`
			FundOwnership struct {
				OwnershipList []ownershipEntry `json:"ownershipList"`
			} `json:"fundOwnership"`
			InsiderTransactions struct {
				Transactions []struct {
					FilerName       string            `json:"filerName"`
					FilerRelation   string            `json:"filerRelation"`
					Shares          quoteSummaryFloat `json:"shares"`
					StartDate       quoteSummaryDate  `json:"startDate"`
					TransactionText string            `json:"transactionText"`
				} `json:"transactions"`
			} `json:"insiderTransactions"`
			NetSharePurchaseActivity struct {
				Period        string            `json:"period"`
				NetInfoShares quoteSummaryFloat `json:"netInfoShares"`
			} `json:"netSharePurchaseActivity"`
			IncomeStatementHistory struct {
				IncomeStatementHistory []struct {
					EndDate      quoteSummaryDate  `json:"endDate"`
					TotalRevenue quoteSummaryFloat `json:"totalRevenue"`
					NetIncome    quoteSummaryFloat `json:"netIncome"`
				} `json:"incomeStatementHistory"`
			} `json:"incomeStatementHistory"`
			SecFilings struct {
				Filings []struct {
					Date     string `json:"date"`
					Type     string `json:"type"`
					Title    string `json:"title"`
					EdgarURL string `json:"edgarUrl"`
				} `json:"filings"`
			} `json:"secFilings"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteSummary"`
}

type ownershipEntry struct {
	Organization string            `json:"organization"`
	PctHeld      quoteSummaryFloat `json:"pctHeld"`
	Value        quoteSummaryFloat `json:"value"`
}

type ttmStatementResponse struct {
	Timeseries struct {
		Result []map[string]json.RawMessage `json:"result"`
		Error  *struct {
			Description string `json:"description"`
		} `json:"error"`
	} `json:"timeseries"`
}

func fetchTTMStatement(client *http.Client, crumb string, symbol string) (TTMStatement, error) {
	period2 := time.Now().Unix()
	period1 := time.Now().AddDate(-2, 0, 0).Unix()
	dataURL := fmt.Sprintf(
		"https://query2.finance.yahoo.com/ws/fundamentals-timeseries/v1/finance/timeseries/%s?type=trailingTotalRevenue,trailingEBIT,trailingInterestExpense&period1=%d&period2=%d&crumb=%s",
		symbol, period1, period2, url.QueryEscape(crumb),
	)
	body, err := get(client, dataURL)
	if err != nil {
		return TTMStatement{}, fmt.Errorf("fetching yahoo TTM statement: %w", err)
	}

	var parsed ttmStatementResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return TTMStatement{}, fmt.Errorf("decoding yahoo TTM statement response: %w", err)
	}
	if parsed.Timeseries.Error != nil {
		return TTMStatement{}, fmt.Errorf("yahoo timeseries api error: %s", parsed.Timeseries.Error.Description)
	}

	latest := func(key string) *float64 {
		var out *float64
		for _, result := range parsed.Timeseries.Result {
			raw, ok := result[key]
			if !ok {
				continue
			}
			var points []struct {
				ReportedValue quoteSummaryFloat `json:"reportedValue"`
			}
			if json.Unmarshal(raw, &points) != nil {
				continue
			}
			for _, p := range points {
				if p.ReportedValue.Raw != nil {
					out = p.ReportedValue.Raw
				}
			}
		}
		return out
	}

	return TTMStatement{
		Revenue:         latest("trailingTotalRevenue"),
		EBIT:            latest("trailingEBIT"),
		InterestExpense: latest("trailingInterestExpense"),
	}, nil
}

func FetchCompanyData(symbol string, peerSymbols []string) (CompanyData, error) {
	client, crumb, err := NewAuthedClient()
	if err != nil {
		return CompanyData{}, err
	}
	return FetchCompanyDataWithClient(client, crumb, symbol, peerSymbols)
}

func FetchCompanyDataWithClient(client *http.Client, crumb string, symbol string, peerSymbols []string) (CompanyData, error) {
	modules := "assetProfile,financialData,majorHoldersBreakdown,institutionOwnership,fundOwnership,insiderTransactions,netSharePurchaseActivity,incomeStatementHistory,secFilings"
	dataURL := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=%s&crumb=%s",
		symbol, modules, url.QueryEscape(crumb),
	)
	body, err := get(client, dataURL)
	if err != nil {
		return CompanyData{}, fmt.Errorf("fetching yahoo company data: %w", err)
	}

	var parsed companyDataResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return CompanyData{}, fmt.Errorf("decoding yahoo company data response: %w", err)
	}
	if parsed.QuoteSummary.Error != nil {
		return CompanyData{}, fmt.Errorf("yahoo quoteSummary api error: %s", parsed.QuoteSummary.Error.Description)
	}
	if len(parsed.QuoteSummary.Result) == 0 {
		return CompanyData{}, fmt.Errorf("yahoo quoteSummary api returned no data for %s", symbol)
	}
	r := parsed.QuoteSummary.Result[0]

	address := r.AssetProfile.Address1
	if r.AssetProfile.City != "" {
		address += ", " + r.AssetProfile.City
	}
	if r.AssetProfile.State != "" {
		address += ", " + r.AssetProfile.State
	}
	if r.AssetProfile.Country != "" {
		address += ", " + r.AssetProfile.Country
	}

	data := CompanyData{
		Profile: CompanyProfile{
			Sector:    r.AssetProfile.Sector,
			Industry:  r.AssetProfile.Industry,
			Employees: r.AssetProfile.FullTimeEmployees,
			Website:   r.AssetProfile.Website,
			Address:   address,
			Summary:   r.AssetProfile.LongBusinessSummary,
		},
		Analyst: AnalystTarget{
			TargetHigh:   r.FinancialData.TargetHighPrice.Raw,
			TargetLow:    r.FinancialData.TargetLowPrice.Raw,
			TargetMean:   r.FinancialData.TargetMeanPrice.Raw,
			TargetMedian: r.FinancialData.TargetMedianPrice.Raw,
			NumAnalysts:  r.FinancialData.NumberOfAnalystOpinions.Raw,
		},
		BalanceSheet: BalanceSheetSnapshot{
			TotalCash:    r.FinancialData.TotalCash.Raw,
			TotalDebt:    r.FinancialData.TotalDebt.Raw,
			CurrentRatio: r.FinancialData.CurrentRatio.Raw,
			QuickRatio:   r.FinancialData.QuickRatio.Raw,
		},
		CashFlow: CashFlowSnapshot{
			OperatingCashflow: r.FinancialData.OperatingCashflow.Raw,
			FreeCashflow:      r.FinancialData.FreeCashflow.Raw,
		},
		Ownership: Ownership{
			InsidersPct:       r.MajorHoldersBreakdown.InsidersPercentHeld.Raw,
			InstitutionsPct:   r.MajorHoldersBreakdown.InstitutionsPercentHeld.Raw,
			InstitutionsCount: r.MajorHoldersBreakdown.InstitutionsCount.Raw,
			NetInsiderActivity: NetInsiderActivity{
				Period:    r.NetSharePurchaseActivity.Period,
				NetShares: r.NetSharePurchaseActivity.NetInfoShares.Raw,
			},
		},
	}

	for _, o := range r.InstitutionOwnership.OwnershipList {
		data.Ownership.TopInstitutions = append(data.Ownership.TopInstitutions, Holder{
			Organization: o.Organization,
			PctHeld:      o.PctHeld.Raw,
			Value:        o.Value.Raw,
		})
	}
	for _, o := range r.FundOwnership.OwnershipList {
		data.Ownership.TopFunds = append(data.Ownership.TopFunds, Holder{
			Organization: o.Organization,
			PctHeld:      o.PctHeld.Raw,
			Value:        o.Value.Raw,
		})
	}
	for _, t := range r.InsiderTransactions.Transactions {
		data.Ownership.InsiderTransactions = append(data.Ownership.InsiderTransactions, InsiderTransaction{
			FilerName: t.FilerName,
			Relation:  t.FilerRelation,
			Shares:    t.Shares.Raw,
			Date:      t.StartDate.Fmt,
			Text:      t.TransactionText,
		})
	}
	for _, s := range r.IncomeStatementHistory.IncomeStatementHistory {
		data.Financials = append(data.Financials, YearlyFinancials{
			EndDate:      s.EndDate.Fmt,
			TotalRevenue: s.TotalRevenue.Raw,
			NetIncome:    s.NetIncome.Raw,
		})
	}
	for _, f := range r.SecFilings.Filings {
		data.Filings = append(data.Filings, SecFiling{
			Date:  f.Date,
			Type:  f.Type,
			Title: f.Title,
			URL:   f.EdgarURL,
		})
	}

	if ttm, err := fetchTTMStatement(client, crumb, symbol); err == nil {
		data.TTM = ttm
	}

	peers, err := FetchPeerQuotes(client, crumb, symbol, peerSymbols)
	if err != nil {
		peers = nil
	} else {
		enrichPeerFundamentals(client, crumb, peers)
	}
	data.Peers = peers

	return data, nil
}
