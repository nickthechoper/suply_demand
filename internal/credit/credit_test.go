package credit

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRatings(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ratings.csv")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSnapshotParsesRatings(t *testing.T) {
	path := writeRatings(t, "Symbol,Rating,Outlook\nORCL,bbb-,stable\nBRK.B,AA,Positive\n,A,\nXYZ,,\n")

	got := NewSource(path).Snapshot()

	if len(got) != 2 {
		t.Fatalf("got %d ratings, want 2: %v", len(got), got)
	}
	if r := got["ORCL"]; r.Rating != "BBB-" || r.Outlook != "Stable" || r.Rank != 13 {
		t.Errorf("ORCL = %+v", r)
	}
	if r := got["BRK-B"]; r.Rating != "AA" || r.Outlook != "Positive" || r.Rank != 20 {
		t.Errorf("BRK-B = %+v", r)
	}
}

func TestSnapshotToleratesByteOrderMark(t *testing.T) {
	path := writeRatings(t, "\xef\xbb\xbfSymbol,Rating\nMSFT,AAA\n")

	if _, ok := NewSource(path).Snapshot()["MSFT"]; !ok {
		t.Fatal("MSFT missing after BOM header")
	}
}

func TestSnapshotMissingFileIsEmpty(t *testing.T) {
	got := NewSource(filepath.Join(t.TempDir(), "absent.csv")).Snapshot()
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestSnapshotOutlookColumnOptional(t *testing.T) {
	path := writeRatings(t, "Symbol,Rating\nMSFT,AAA\n")

	r := NewSource(path).Snapshot()["MSFT"]

	if r.Rating != "AAA" || r.Outlook != "" || r.Rank != 22 {
		t.Errorf("MSFT = %+v", r)
	}
}

func TestSnapshotUnknownRatingKeptWithoutRank(t *testing.T) {
	path := writeRatings(t, "Symbol,Rating,Outlook\nFOO,NR,\n")

	r := NewSource(path).Snapshot()["FOO"]

	if r.Rating != "NR" || r.Rank != 0 {
		t.Errorf("FOO = %+v", r)
	}
}
