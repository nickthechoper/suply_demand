package score

import "testing"

func f(v float64) *float64 { return &v }

func baseInput() Input {
	return Input{
		Price:            100,
		PERatio:          f(15),
		ROICPct:          f(15),
		NetMarginPct:     f(15),
		RevenueGrowthPct: f(10),
		TargetMean:       f(120),
		NumAnalysts:      f(20),
	}
}

func component(r Result, name string) Component {
	for _, c := range r.Components {
		if c.Name == name {
			return c
		}
	}
	return Component{}
}

func TestWeightsSumToOneHundred(t *testing.T) {
	var sum float64
	for _, c := range Compute(baseInput()).Components {
		sum += c.Weight
	}
	if sum != 100 {
		t.Fatalf("weights sum to %v, want 100", sum)
	}
}

func resilienceInput() Input {
	in := baseInput()
	in.Revenue = f(1000)
	in.FreeCashflow = f(200)
	in.OperatingCashflow = f(250)
	in.TotalCash = f(400)
	in.TotalDebt = f(200)
	in.EBIT = f(160)
	in.InterestExpense = f(10)
	return in
}

func TestResilienceMatchesFormulaWithoutAltman(t *testing.T) {
	s := component(Compute(resilienceInput()), "Resilience").Score
	if s == nil {
		t.Fatal("expected a resilience score")
	}
	want := 100 * (0.30*1 + 0.25*1 + 0.20*1 + 0.15*0.8) / 0.90
	if d := *s - want; d > 1e-9 || d < -1e-9 {
		t.Fatalf("resilience = %v, want %v", *s, want)
	}
}

func TestResilienceDropsWithWeakBalanceSheet(t *testing.T) {
	weak := resilienceInput()
	weak.FreeCashflow = f(-100)
	weak.OperatingCashflow = f(50)
	weak.TotalCash = f(10)
	weak.TotalDebt = f(900)
	weak.EBIT = f(5)

	strong := component(Compute(resilienceInput()), "Resilience").Score
	low := component(Compute(weak), "Resilience").Score
	if strong == nil || low == nil || *low >= *strong {
		t.Fatalf("expected weak < strong, got %v, %v", low, strong)
	}
}

func TestResilienceNeedsEnoughInputs(t *testing.T) {
	in := baseInput()
	in.TotalCash = f(100)
	in.TotalDebt = f(0)
	if component(Compute(in), "Resilience").Score != nil {
		t.Fatal("resilience should be nil with only the liquidity input")
	}
}
