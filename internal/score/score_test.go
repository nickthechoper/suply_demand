package score

import "testing"

func f(v float64) *float64 { return &v }

func baseInput() Input {
	return Input{
		Price:              100,
		PERatio:            f(15),
		ROICPct:            f(15),
		NetMarginPct:       f(15),
		RevenueGrowthPct:   f(10),
		RecommendationMean: f(2),
		NumAnalysts:        f(20),
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
	if w := component(Compute(baseInput()), "Credit").Weight; w != 15 {
		t.Fatalf("credit weight = %v, want 15", w)
	}
}

func TestCreditRaisesAndLowersScore(t *testing.T) {
	without := Compute(baseInput())

	strong := baseInput()
	strong.CreditRank = f(22)
	weak := baseInput()
	weak.CreditRank = f(8)

	high, low := Compute(strong), Compute(weak)
	if !(high.Total > without.Total && without.Total > low.Total) {
		t.Fatalf("expected AAA > no credit > B, got %v, %v, %v", high.Total, without.Total, low.Total)
	}
	if s := component(high, "Credit").Score; s == nil || *s != 100 {
		t.Fatalf("AAA credit score = %v, want 100", s)
	}
}

func TestMissingCreditDoesNotBlockQualification(t *testing.T) {
	r := Compute(baseInput())
	if !r.Qualified {
		t.Fatalf("expected qualified without credit, components: %+v", r.Components)
	}
	if component(r, "Credit").Score != nil {
		t.Fatal("credit score should be nil when no rating is known")
	}
}
