package credit

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

type Rating struct {
	Rating  string
	Outlook string
	Rank    int
	Form    string
	Filed   string
}

func (r Rating) Origin() string {
	switch {
	case r.Form == "":
		return "manual"
	case r.Filed == "":
		return r.Form
	default:
		return r.Form + " filed " + r.Filed
	}
}

func RankOf(rating string) int {
	return scale[rating]
}

var scale = map[string]int{
	"AAA": 22, "AA+": 21, "AA": 20, "AA-": 19,
	"A+": 18, "A": 17, "A-": 16,
	"BBB+": 15, "BBB": 14, "BBB-": 13,
	"BB+": 12, "BB": 11, "BB-": 10,
	"B+": 9, "B": 8, "B-": 7,
	"CCC+": 6, "CCC": 5, "CCC-": 4,
	"CC": 3, "C": 2, "SD": 1, "D": 1,
}

type Source struct {
	path string

	mu      sync.Mutex
	modTime time.Time
	ratings map[string]Rating
}

func NewSource(path string) *Source {
	return &Source{path: path, ratings: map[string]Rating{}}
}

func (s *Source) Snapshot() map[string]Rating {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, err := os.Stat(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.ratings = map[string]Rating{}
			s.modTime = time.Time{}
		} else {
			log.Printf("credit: checking %s: %v", s.path, err)
		}
		return s.ratings
	}
	if info.ModTime().Equal(s.modTime) {
		return s.ratings
	}

	ratings, err := load(s.path)
	if err != nil {
		log.Printf("credit: loading %s: %v", s.path, err)
		return s.ratings
	}
	s.ratings = ratings
	s.modTime = info.ModTime()
	return s.ratings
}

func load(path string) (map[string]Rating, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		if err == io.EOF {
			return map[string]Rating{}, nil
		}
		return nil, err
	}

	symbolCol, ratingCol, outlookCol := -1, -1, -1
	for i, col := range header {
		switch strings.ToLower(strings.TrimSpace(strings.TrimPrefix(col, "\xef\xbb\xbf"))) {
		case "symbol":
			symbolCol = i
		case "rating":
			ratingCol = i
		case "outlook":
			outlookCol = i
		}
	}
	if symbolCol == -1 || ratingCol == -1 {
		return nil, fmt.Errorf("header needs Symbol and Rating columns")
	}

	ratings := map[string]Rating{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		symbol := strings.ToUpper(strings.TrimSpace(field(row, symbolCol)))
		rating := strings.ToUpper(strings.TrimSpace(field(row, ratingCol)))
		if symbol == "" || rating == "" {
			continue
		}
		symbol = strings.ReplaceAll(symbol, ".", "-")
		ratings[symbol] = Rating{
			Rating:  rating,
			Outlook: normalizeOutlook(field(row, outlookCol)),
			Rank:    scale[rating],
		}
	}
	return ratings, nil
}

func field(row []string, col int) string {
	if col < 0 || col >= len(row) {
		return ""
	}
	return row[col]
}

func normalizeOutlook(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}
