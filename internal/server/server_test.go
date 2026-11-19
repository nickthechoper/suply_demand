package server

import (
	"math"
	"testing"
	"time"

	"supplydemand/internal/yahoo"
)

func TestTwoYearStats(t *testing.T) {
	end := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	closes := []struct {
		daysBack int
		close    float64
	}{
		{900, 10}, {730, 100}, {400, 200}, {300, 150}, {0, 250},
	}
	var bars []yahoo.Bar
	for _, c := range closes {
		bars = append(bars, yahoo.Bar{Time: end.AddDate(0, 0, -c.daysBack), Close: c.close})
	}

	ret, dd := twoYearStats(bars)
	if ret == nil || dd == nil {
		t.Fatal("expected stats")
	}
	if math.Abs(*ret-150) > 0.01 {
		t.Errorf("return = %v, want 150", *ret)
	}
	if math.Abs(*dd-(-25)) > 0.01 {
		t.Errorf("drawdown = %v, want -25", *dd)
	}
}

func TestTwoYearStatsTooFewBars(t *testing.T) {
	ret, dd := twoYearStats([]yahoo.Bar{{Time: time.Now(), Close: 1}})
	if ret != nil || dd != nil {
		t.Error("expected nil stats")
	}
}
