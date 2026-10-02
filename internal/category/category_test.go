package category

import "testing"

func TestTheme(t *testing.T) {
	cases := map[string]string{
		"NVDA": "AI Chips & Accelerators",
		"nvda": "AI Chips & Accelerators",
		"MSFT": "Hyperscalers",
		"CLS":  "Networking & Optical",
		"TER":  "Foundry & Semi Equipment",
		"GEV":  "Data Center Power & Cooling",
		"MCD":  "",
	}
	for symbol, want := range cases {
		if got := Theme(symbol); got != want {
			t.Errorf("Theme(%q) = %q, want %q", symbol, got, want)
		}
	}
}

func TestThemesMatchDefinitionOrder(t *testing.T) {
	got := Themes()
	if len(got) != len(themes) || got[0] != themes[0].name {
		t.Errorf("Themes() = %v", got)
	}
}
