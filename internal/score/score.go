package score

type Input struct {
	Price                 float64
	ZonePosition          *float64
	RecommendationMean    *float64
	TargetMean            *float64
	NumAnalysts           *float64
	PERatio               *float64
	ForwardPERatio        *float64
	PEGRatio              *float64
	PSRatio               *float64
	RevenueGrowthPct      *float64
	EarningsGrowthPct     *float64
	NetMarginPct          *float64
	OperatingMarginPct    *float64
	ROICPct               *float64
	MarketCap             *float64
	FreeCashflow          *float64
	NetDebtToMarketCapPct *float64
	CurrentRatio          *float64
	NetInsiderOpenMarket  *float64
}

type Component struct {
	Name   string   `json:"name"`
	Weight float64  `json:"weight"`
	Score  *float64 `json:"score"`
}

type Result struct {
	Qualified  bool
	Total      float64
	Components []Component
	UpsidePct  *float64
	Flags      []string
}

type curve [][2]float64

func (c curve) at(v float64) float64 {
	if v <= c[0][0] {
		return c[0][1]
	}
	for i := 1; i < len(c); i++ {
		if v <= c[i][0] {
			x0, y0, x1, y1 := c[i-1][0], c[i-1][1], c[i][0], c[i][1]
			return y0 + (v-x0)/(x1-x0)*(y1-y0)
		}
	}
	return c[len(c)-1][1]
}

var (
	peCurve              = curve{{0, 60}, {8, 100}, {15, 80}, {25, 50}, {40, 20}, {60, 0}}
	pegCurve             = curve{{0.5, 100}, {1, 80}, {1.5, 60}, {2, 40}, {3, 0}}
	psCurve              = curve{{1, 100}, {3, 70}, {6, 40}, {10, 15}, {20, 0}}
	fcfYieldCurve        = curve{{-5, 0}, {0, 25}, {3, 55}, {6, 85}, {10, 100}}
	roicCurve            = curve{{-10, 0}, {0, 20}, {8, 50}, {15, 80}, {25, 100}}
	operatingMarginCurve = curve{{-10, 0}, {0, 25}, {10, 55}, {20, 80}, {35, 100}}
	netMarginCurve       = curve{{-10, 0}, {0, 25}, {8, 55}, {15, 80}, {25, 100}}
	netDebtCurve         = curve{{-10, 100}, {0, 85}, {25, 60}, {50, 30}, {100, 0}}
	currentRatioCurve    = curve{{0.5, 0}, {1, 50}, {1.5, 80}, {2, 100}}
	revenueGrowthCurve   = curve{{-20, 0}, {0, 30}, {10, 60}, {25, 85}, {50, 100}}
	earningsGrowthCurve  = curve{{-50, 0}, {0, 35}, {15, 65}, {40, 90}, {100, 100}}
	ratingCurve          = curve{{1.5, 100}, {2, 75}, {2.5, 50}, {3, 25}, {3.5, 0}}
	upsideCurve          = curve{{-20, 0}, {0, 30}, {15, 65}, {30, 90}, {50, 100}}
	zoneCurve            = curve{{0, 100}, {1, 0}}
	insiderBpsCurve      = curve{{-50, 30}, {-5, 45}, {0, 50}, {1, 70}, {10, 100}}
)

const (
	weightValuation = 25
	weightQuality   = 25
	weightGrowth    = 15
	weightAnalyst   = 15
	weightZone      = 15
	weightInsider   = 5

	minCoverage = 0.6
)

type bucket []float64

func (b *bucket) add(v *float64, c curve) {
	if v != nil {
		*b = append(*b, c.at(*v))
	}
}

func (b *bucket) addScore(v *float64) {
	if v != nil {
		*b = append(*b, *v)
	}
}

func (b bucket) mean() *float64 {
	if len(b) == 0 {
		return nil
	}
	var sum float64
	for _, v := range b {
		sum += v
	}
	m := sum / float64(len(b))
	return &m
}

func Compute(in Input) Result {
	if in.Price <= 0 {
		return Result{}
	}

	var fcfYieldPct *float64
	if in.FreeCashflow != nil && in.MarketCap != nil && *in.MarketCap > 0 {
		v := *in.FreeCashflow / *in.MarketCap * 100
		fcfYieldPct = &v
	}

	var valuation bucket
	valuation.addScore(earningsMultipleScore(in))
	if in.PEGRatio != nil && *in.PEGRatio > 0 {
		valuation.add(in.PEGRatio, pegCurve)
	}
	if in.PSRatio != nil && *in.PSRatio > 0 {
		valuation.add(in.PSRatio, psCurve)
	}
	valuation.add(fcfYieldPct, fcfYieldCurve)

	var quality bucket
	quality.add(in.ROICPct, roicCurve)
	quality.add(in.OperatingMarginPct, operatingMarginCurve)
	quality.add(in.NetMarginPct, netMarginCurve)
	quality.add(in.NetDebtToMarketCapPct, netDebtCurve)
	quality.add(in.CurrentRatio, currentRatioCurve)

	var growth bucket
	growth.add(in.RevenueGrowthPct, revenueGrowthCurve)
	growth.add(in.EarningsGrowthPct, earningsGrowthCurve)

	var upsidePct *float64
	if in.TargetMean != nil && *in.TargetMean > 0 {
		v := (*in.TargetMean - in.Price) / in.Price * 100
		upsidePct = &v
	}

	var analyst bucket
	analyst.add(in.RecommendationMean, ratingCurve)
	analyst.add(upsidePct, upsideCurve)
	analystScore := analyst.mean()
	if analystScore != nil {
		confidence := 0.5
		if in.NumAnalysts != nil {
			confidence = *in.NumAnalysts / (*in.NumAnalysts + 5)
		}
		v := 50 + (*analystScore-50)*confidence
		analystScore = &v
	}

	var zone bucket
	zone.add(in.ZonePosition, zoneCurve)

	var insider bucket
	if in.NetInsiderOpenMarket != nil && in.MarketCap != nil && *in.MarketCap > 0 {
		bps := *in.NetInsiderOpenMarket / *in.MarketCap * 10000
		insider.add(&bps, insiderBpsCurve)
	}

	components := []Component{
		{Name: "Valuation", Weight: weightValuation, Score: valuation.mean()},
		{Name: "Quality", Weight: weightQuality, Score: quality.mean()},
		{Name: "Growth", Weight: weightGrowth, Score: growth.mean()},
		{Name: "Analyst", Weight: weightAnalyst, Score: analystScore},
		{Name: "Zone", Weight: weightZone, Score: zone.mean()},
		{Name: "Insider", Weight: weightInsider, Score: insider.mean()},
	}

	var weighted, covered, total float64
	for _, c := range components {
		total += c.Weight
		if c.Score != nil {
			weighted += *c.Score * c.Weight
			covered += c.Weight
		}
	}

	result := Result{
		Components: components,
		UpsidePct:  upsidePct,
		Flags:      flags(in),
	}
	if components[0].Score == nil || components[1].Score == nil || covered/total < minCoverage {
		return result
	}
	result.Qualified = true
	result.Total = weighted / covered
	return result
}

func earningsMultipleScore(in Input) *float64 {
	var scores bucket
	for _, pe := range []*float64{in.PERatio, in.ForwardPERatio} {
		if pe != nil && *pe > 0 {
			scores.add(pe, peCurve)
		}
	}
	if len(scores) == 0 && in.NetMarginPct != nil && *in.NetMarginPct < 0 {
		zero := 0.0
		return &zero
	}
	return scores.mean()
}

func flags(in Input) []string {
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
