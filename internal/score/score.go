package score

type Input struct {
	Zone                  string
	Price                 float64
	RecommendationMean    *float64
	TargetMean            *float64
	PERatio               *float64
	PEGRatio              *float64
	RevenueGrowthPct      *float64
	NetMarginPct          *float64
	OperatingMarginPct    *float64
	FreeCashflow          *float64
	NetDebtToMarketCapPct *float64
	EarningsGrowthPct     *float64
}

type Result struct {
	Qualified         bool
	Total             float64
	ZoneScore         float64
	AnalystScore      float64
	FundamentalsScore float64
	UpsidePct         *float64
	Flags             []string
}

func Compute(in Input) Result {
	if in.Price <= 0 || in.RecommendationMean == nil || in.TargetMean == nil ||
		in.PERatio == nil || in.PEGRatio == nil || in.RevenueGrowthPct == nil || in.NetMarginPct == nil ||
		in.OperatingMarginPct == nil {
		return Result{}
	}

	zoneScore := 50.0
	switch in.Zone {
	case "demand":
		zoneScore = 100
	case "supply":
		zoneScore = 0
	}

	ratingScore := clamp((5-*in.RecommendationMean)/4*100, 0, 100)

	upsidePct := (*in.TargetMean - in.Price) / in.Price * 100
	upsideScore := clamp((upsidePct+30)/90*100, 0, 100)

	analystScore := (ratingScore + upsideScore) / 2

	peScore := 0.0
	if *in.PERatio > 0 {
		peScore = 100 - clamp(*in.PERatio, 0, 50)/50*100
	}

	pegScore := 0.0
	if *in.PEGRatio > 0 {
		pegScore = 100 - clamp(*in.PEGRatio, 0, 3)/3*100
	}

	growthScore := clamp((*in.RevenueGrowthPct+20)/60*100, 0, 100)
	marginScore := clamp((*in.NetMarginPct+10)/40*100, 0, 100)
	operatingMarginScore := clamp((*in.OperatingMarginPct+10)/40*100, 0, 100)

	fundamentalsScore := (peScore + pegScore + growthScore + marginScore + operatingMarginScore) / 5

	var flags []string
	riskPenalty := 0.0

	if in.FreeCashflow != nil && *in.FreeCashflow < 0 {
		flags = append(flags, "Negative free cash flow")
		riskPenalty += 15
	}
	if in.NetDebtToMarketCapPct != nil && *in.NetDebtToMarketCapPct > 50 {
		flags = append(flags, "Heavy net debt load")
		riskPenalty += 15
	}
	if in.EarningsGrowthPct != nil && *in.EarningsGrowthPct < *in.RevenueGrowthPct-20 {
		flags = append(flags, "Earnings growth lagging revenue")
		riskPenalty += 15
	}

	fundamentalsScore = clamp(fundamentalsScore-riskPenalty, 0, 100)

	return Result{
		Qualified:         true,
		Total:             zoneScore/3 + analystScore/3 + fundamentalsScore/3,
		ZoneScore:         zoneScore,
		AnalystScore:      analystScore,
		FundamentalsScore: fundamentalsScore,
		UpsidePct:         &upsidePct,
		Flags:             flags,
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
