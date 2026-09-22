// Package store persists daily OHLCV bars in a local SQLite database.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"supplydemand/internal/yahoo"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite db: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS bars (
			symbol   TEXT NOT NULL,
			ts       INTEGER NOT NULL,
			open     REAL NOT NULL,
			high     REAL NOT NULL,
			low      REAL NOT NULL,
			close    REAL NOT NULL,
			volume   REAL NOT NULL,
			PRIMARY KEY (symbol, ts)
		)
	`); err != nil {
		return nil, fmt.Errorf("creating bars table: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS fundamentals (
			symbol             TEXT PRIMARY KEY,
			pe_ratio           REAL,
			peg_ratio          REAL,
			revenue_growth_pct REAL,
			net_margin_pct     REAL,
			fetched_at         INTEGER NOT NULL
		)
	`); err != nil {
		return nil, fmt.Errorf("creating fundamentals table: %w", err)
	}

	// Company data (profile, analyst targets, ownership, financials, filings,
	// peer comparison) is a deeply nested, list-heavy structure that's only
	// ever read and replaced wholesale, so it's stored as a JSON blob rather
	// than normalized across many tables.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS company_data (
			symbol     TEXT PRIMARY KEY,
			data       TEXT NOT NULL,
			fetched_at INTEGER NOT NULL
		)
	`); err != nil {
		return nil, fmt.Errorf("creating company_data table: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// UpsertBars inserts or replaces bars for the given symbol.
func (s *Store) UpsertBars(symbol string, bars []yahoo.Bar) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO bars (symbol, ts, open, high, low, close, volume)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(symbol, ts) DO UPDATE SET
			open = excluded.open,
			high = excluded.high,
			low = excluded.low,
			close = excluded.close,
			volume = excluded.volume
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, b := range bars {
		if _, err := stmt.Exec(symbol, b.Time.Unix(), b.Open, b.High, b.Low, b.Close, b.Volume); err != nil {
			return fmt.Errorf("upserting bar at %s: %w", b.Time, err)
		}
	}

	return tx.Commit()
}

// LatestBars returns up to `limit` most recent bars for symbol, ordered oldest to newest.
func (s *Store) LatestBars(symbol string, limit int) ([]yahoo.Bar, error) {
	rows, err := s.db.Query(`
		SELECT ts, open, high, low, close, volume
		FROM bars
		WHERE symbol = ?
		ORDER BY ts DESC
		LIMIT ?
	`, symbol, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bars []yahoo.Bar
	for rows.Next() {
		var ts int64
		var b yahoo.Bar
		if err := rows.Scan(&ts, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume); err != nil {
			return nil, err
		}
		b.Time = time.Unix(ts, 0).UTC()
		bars = append(bars, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse to oldest-to-newest order.
	for i, j := 0, len(bars)-1; i < j; i, j = i+1, j-1 {
		bars[i], bars[j] = bars[j], bars[i]
	}

	return bars, nil
}

// SetFundamentals upserts the fundamentals snapshot for symbol, stamped with the current time.
func (s *Store) SetFundamentals(symbol string, f yahoo.Fundamentals) error {
	_, err := s.db.Exec(`
		INSERT INTO fundamentals (symbol, pe_ratio, peg_ratio, revenue_growth_pct, net_margin_pct, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(symbol) DO UPDATE SET
			pe_ratio = excluded.pe_ratio,
			peg_ratio = excluded.peg_ratio,
			revenue_growth_pct = excluded.revenue_growth_pct,
			net_margin_pct = excluded.net_margin_pct,
			fetched_at = excluded.fetched_at
	`, symbol, f.PERatio, f.PEGRatio, f.RevenueGrowthPct, f.NetMarginPct, time.Now().Unix())
	return err
}

// Fundamentals returns the stored fundamentals snapshot for symbol along with
// when it was fetched. ok is false if nothing has been stored yet.
func (s *Store) Fundamentals(symbol string) (f yahoo.Fundamentals, fetchedAt time.Time, ok bool, err error) {
	var peRatio, pegRatio, revenueGrowthPct, netMarginPct sql.NullFloat64
	var ts int64
	row := s.db.QueryRow(`
		SELECT pe_ratio, peg_ratio, revenue_growth_pct, net_margin_pct, fetched_at
		FROM fundamentals WHERE symbol = ?
	`, symbol)
	if scanErr := row.Scan(&peRatio, &pegRatio, &revenueGrowthPct, &netMarginPct, &ts); scanErr != nil {
		if scanErr == sql.ErrNoRows {
			return f, time.Time{}, false, nil
		}
		return f, time.Time{}, false, scanErr
	}
	if peRatio.Valid {
		f.PERatio = &peRatio.Float64
	}
	if pegRatio.Valid {
		f.PEGRatio = &pegRatio.Float64
	}
	if revenueGrowthPct.Valid {
		f.RevenueGrowthPct = &revenueGrowthPct.Float64
	}
	if netMarginPct.Valid {
		f.NetMarginPct = &netMarginPct.Float64
	}
	return f, time.Unix(ts, 0).UTC(), true, nil
}

// SetCompanyData upserts the company data snapshot for symbol, stamped with the current time.
func (s *Store) SetCompanyData(symbol string, data yahoo.CompanyData) error {
	blob, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encoding company data: %w", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO company_data (symbol, data, fetched_at)
		VALUES (?, ?, ?)
		ON CONFLICT(symbol) DO UPDATE SET
			data = excluded.data,
			fetched_at = excluded.fetched_at
	`, symbol, string(blob), time.Now().Unix())
	return err
}

// CompanyData returns the stored company data snapshot for symbol along with
// when it was fetched. ok is false if nothing has been stored yet.
func (s *Store) CompanyData(symbol string) (data yahoo.CompanyData, fetchedAt time.Time, ok bool, err error) {
	var blob string
	var ts int64
	row := s.db.QueryRow(`SELECT data, fetched_at FROM company_data WHERE symbol = ?`, symbol)
	if scanErr := row.Scan(&blob, &ts); scanErr != nil {
		if scanErr == sql.ErrNoRows {
			return data, time.Time{}, false, nil
		}
		return data, time.Time{}, false, scanErr
	}
	if err := json.Unmarshal([]byte(blob), &data); err != nil {
		return data, time.Time{}, false, fmt.Errorf("decoding stored company data: %w", err)
	}
	return data, time.Unix(ts, 0).UTC(), true, nil
}

// LatestTimestamp returns the most recent bar timestamp stored for symbol, or the zero time if none.
func (s *Store) LatestTimestamp(symbol string) (time.Time, error) {
	var ts sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(ts) FROM bars WHERE symbol = ?`, symbol).Scan(&ts)
	if err != nil {
		return time.Time{}, err
	}
	if !ts.Valid {
		return time.Time{}, nil
	}
	return time.Unix(ts.Int64, 0).UTC(), nil
}
