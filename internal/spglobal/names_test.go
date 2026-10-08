package spglobal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeNameMatchesSharedFixtures(t *testing.T) {
	blob, err := os.ReadFile(filepath.Join("..", "..", "tools", "sp-ratings", "name-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input string `json:"input"`
		Want  string `json:"want"`
	}
	if err := json.Unmarshal(blob, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no fixtures")
	}
	for _, c := range cases {
		if got := NormalizeName(c.Input); got != c.Want {
			t.Errorf("NormalizeName(%q) = %q, want %q", c.Input, got, c.Want)
		}
	}
}
