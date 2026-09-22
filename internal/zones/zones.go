// Package zones ports LuxAlgo's "Supply and Demand Fixed Range" Pine Script
// logic to Go. The original indicator builds its volume profile from
// intrabar (lower-timeframe) ticks via request.security_lower_tf, which
// Yahoo Finance's free daily data doesn't provide. Instead, each daily bar's
// volume is distributed across the price bins its [low, high] range
// overlaps, proportional to the overlap size — the standard approximation
// used by visible-range volume profile tools when only OHLCV bars are
// available.
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
	ThresholdPct float64 // "per": % of total volume that defines a zone edge
	Resolution   int     // "div": number of price bins spanning [min, max]
}

type Result struct {
	Max float64
	Min float64

	SupplyReached bool
	SupplyTop     float64
	SupplyBtm     float64
	SupplyAvg     float64 // midpoint of [SupplyBtm, Max]
	SupplyWavg    float64 // volume-weighted price within the supply zone

	DemandReached bool
	DemandTop     float64
	DemandBtm     float64
	DemandAvg     float64 // midpoint of [Min, DemandTop]
	DemandWavg    float64 // volume-weighted price within the demand zone

	EquilibriumAvg  float64
	EquilibriumWavg float64

	Profile []ProfileBin
}

// Compute builds supply/demand/equilibrium zones from bars using a fixed
// lookback window (the caller should already have trimmed bars to that
// window).
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

	// Supply zone: walk bins top-down from max until accumulated volume
	// exceeds the threshold percentage of total lookback volume.
	var supplyCum, supplyWeighted float64
	for j := params.Resolution - 1; j >= 0; j-- {
		bin := profile[j]
		supplyCum += bin.Volume
		supplyWeighted += bin.Low * bin.Volume
		if !res.SupplyReached && supplyCum/totalVolume*100 > params.ThresholdPct {
			res.SupplyReached = true
			res.SupplyTop = max
			res.SupplyBtm = bin.Low
			res.SupplyAvg = (max + bin.Low) / 2
			res.SupplyWavg = supplyWeighted / supplyCum
			break
		}
	}

	// Demand zone: walk bins bottom-up from min until accumulated volume
	// exceeds the threshold percentage of total lookback volume.
	var demandCum, demandWeighted float64
	for j := 0; j < params.Resolution; j++ {
		bin := profile[j]
		demandCum += bin.Volume
		demandWeighted += bin.High * bin.Volume
		if !res.DemandReached && demandCum/totalVolume*100 > params.ThresholdPct {
			res.DemandReached = true
			res.DemandBtm = min
			res.DemandTop = bin.High
			res.DemandAvg = (min + bin.High) / 2
			res.DemandWavg = demandWeighted / demandCum
			break
		}
	}

	if res.SupplyReached && res.DemandReached {
		res.EquilibriumAvg = (max + min) / 2
		res.EquilibriumWavg = (res.SupplyWavg + res.DemandWavg) / 2
	}

	return res
}

// binVolume estimates the volume traded within [lo, hi] by distributing each
// bar's volume proportionally to how much of its [Low, High] range overlaps
// the bin.
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
