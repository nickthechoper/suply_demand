package zones

import "math"

type Bar struct {
	High   float64
	Low    float64
	Volume float64
}

type ProfileBin struct {
	Low    float64
	High   float64
	Volume float64
}

type Params struct {
	ThresholdPct float64
	Resolution   int
}

type Result struct {
	Max float64
	Min float64

	SupplyReached bool
	SupplyTop     float64
	SupplyBtm     float64
	SupplyAvg     float64

	DemandReached bool
	DemandTop     float64
	DemandBtm     float64
	DemandAvg     float64

	EquilibriumAvg float64

	Profile []ProfileBin
}

func Compute(bars []Bar, params Params) Result {
	var res Result
	if len(bars) == 0 || params.Resolution <= 0 {
		return res
	}

	max, min := bars[0].High, bars[0].Low
	var totalVolume float64
	for _, b := range bars {
		if b.High > max {
			max = b.High
		}
		if b.Low < min {
			min = b.Low
		}
		totalVolume += b.Volume
	}
	res.Max = max
	res.Min = min

	if max <= min || totalVolume <= 0 {
		return res
	}

	r := (max - min) / float64(params.Resolution)

	profile := make([]ProfileBin, params.Resolution)
	for j := 0; j < params.Resolution; j++ {
		lo := min + float64(j)*r
		hi := lo + r
		profile[j] = ProfileBin{Low: lo, High: hi, Volume: binVolume(bars, lo, hi)}
	}
	res.Profile = profile

	var supplyCum float64
	for j := params.Resolution - 1; j >= 0; j-- {
		bin := profile[j]
		supplyCum += bin.Volume
		if supplyCum/totalVolume*100 > params.ThresholdPct {
			res.SupplyReached = true
			res.SupplyTop = max
			res.SupplyBtm = bin.Low
			res.SupplyAvg = (max + bin.Low) / 2
			break
		}
	}

	var demandCum float64
	for j := 0; j < params.Resolution; j++ {
		bin := profile[j]
		demandCum += bin.Volume
		if demandCum/totalVolume*100 > params.ThresholdPct {
			res.DemandReached = true
			res.DemandBtm = min
			res.DemandTop = bin.High
			res.DemandAvg = (min + bin.High) / 2
			break
		}
	}

	if res.SupplyReached && res.DemandReached {
		res.EquilibriumAvg = (max + min) / 2
	}

	return res
}

func binVolume(bars []Bar, lo, hi float64) float64 {
	var total float64
	for _, b := range bars {
		barRange := b.High - b.Low
		if barRange <= 0 {
			if b.High >= lo && b.High <= hi {
				total += b.Volume
			}
			continue
		}
		overlap := math.Min(b.High, hi) - math.Max(b.Low, lo)
		if overlap > 0 {
			total += b.Volume * (overlap / barRange)
		}
	}
	return total
}
