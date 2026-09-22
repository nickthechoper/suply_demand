// Package store persists daily OHLCV bars in a local SQLite database.
package store

import (
	"database/sql"
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
