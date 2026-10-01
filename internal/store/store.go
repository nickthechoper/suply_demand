// Package store persists daily OHLCV bars in a local SQLite database.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"supplydemand/internal/openinsider"
	"supplydemand/internal/yahoo"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
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

	// forward_pe_ratio was added after the fundamentals table shipped;
	// back-fill it on existing databases that predate the column.
	if _, err := db.Exec(`ALTER TABLE fundamentals ADD COLUMN forward_pe_ratio REAL`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return nil, fmt.Errorf("adding forward_pe_ratio column to fundamentals table: %w", err)
		}
	}

	// operating_margin_pct, ps_ratio, and roic_pct were added after the
	// fundamentals table shipped; back-fill them on existing databases that
	// predate the columns.
	for _, col := range []string{"operating_margin_pct", "ps_ratio", "roic_pct"} {
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE fundamentals ADD COLUMN %s REAL`, col)); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return nil, fmt.Errorf("adding %s column to fundamentals table: %w", col, err)
			}
		}
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

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS symbols (
			symbol   TEXT PRIMARY KEY,
			added_at INTEGER NOT NULL
		)
	`); err != nil {
		return nil, fmt.Errorf("creating symbols table: %w", err)
	}

	// name was added after the symbols table shipped; back-fill it on
	// existing databases that predate the column.
	if _, err := db.Exec(`ALTER TABLE symbols ADD COLUMN name TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return nil, fmt.Errorf("adding name column to symbols table: %w", err)
		}
	}

	if _, err := db.Exec(`ALTER TABLE symbols ADD COLUMN bars_fetched_at INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return nil, fmt.Errorf("adding bars_fetched_at column to symbols table: %w", err)
		}
	}

	if _, err := db.Exec(`ALTER TABLE symbols ADD COLUMN note TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return nil, fmt.Errorf("adding note column to symbols table: %w", err)
		}
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS insider_trades (
			symbol     TEXT PRIMARY KEY,
			data       TEXT NOT NULL,
			fetched_at INTEGER NOT NULL
		)
	`); err != nil {
		return nil, fmt.Errorf("creating insider_trades table: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS demand_alerts (
			symbol  TEXT PRIMARY KEY,
			sent_at INTEGER NOT NULL
		)
	`); err != nil {
		return nil, fmt.Errorf("creating demand_alerts table: %w", err)
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

	if _, err := tx.Exec(`UPDATE symbols SET bars_fetched_at = ? WHERE symbol = ?`, time.Now().Unix(), symbol); err != nil {
		return fmt.Errorf("stamping bars fetch time: %w", err)
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
		INSERT INTO fundamentals (symbol, pe_ratio, forward_pe_ratio, peg_ratio, ps_ratio, revenue_growth_pct, net_margin_pct, operating_margin_pct, roic_pct, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(symbol) DO UPDATE SET
			pe_ratio = excluded.pe_ratio,
			forward_pe_ratio = excluded.forward_pe_ratio,
			peg_ratio = excluded.peg_ratio,
			ps_ratio = excluded.ps_ratio,
			revenue_growth_pct = excluded.revenue_growth_pct,
			net_margin_pct = excluded.net_margin_pct,
			operating_margin_pct = excluded.operating_margin_pct,
			roic_pct = excluded.roic_pct,
			fetched_at = excluded.fetched_at
	`, symbol, f.PERatio, f.ForwardPERatio, f.PEGRatio, f.PSRatio, f.RevenueGrowthPct, f.NetMarginPct, f.OperatingMarginPct, f.ROICPct, time.Now().Unix())
	return err
}

// Fundamentals returns the stored fundamentals snapshot for symbol along with
// when it was fetched. ok is false if nothing has been stored yet.
func (s *Store) Fundamentals(symbol string) (f yahoo.Fundamentals, fetchedAt time.Time, ok bool, err error) {
	var peRatio, forwardPERatio, pegRatio, psRatio, revenueGrowthPct, netMarginPct, operatingMarginPct, roicPct sql.NullFloat64
	var ts int64
	row := s.db.QueryRow(`
		SELECT pe_ratio, forward_pe_ratio, peg_ratio, ps_ratio, revenue_growth_pct, net_margin_pct, operating_margin_pct, roic_pct, fetched_at
		FROM fundamentals WHERE symbol = ?
	`, symbol)
	if scanErr := row.Scan(&peRatio, &forwardPERatio, &pegRatio, &psRatio, &revenueGrowthPct, &netMarginPct, &operatingMarginPct, &roicPct, &ts); scanErr != nil {
		if scanErr == sql.ErrNoRows {
			return f, time.Time{}, false, nil
		}
		return f, time.Time{}, false, scanErr
	}
	if peRatio.Valid {
		f.PERatio = &peRatio.Float64
	}
	if forwardPERatio.Valid {
		f.ForwardPERatio = &forwardPERatio.Float64
	}
	if pegRatio.Valid {
		f.PEGRatio = &pegRatio.Float64
	}
	if psRatio.Valid {
		f.PSRatio = &psRatio.Float64
	}
	if revenueGrowthPct.Valid {
		f.RevenueGrowthPct = &revenueGrowthPct.Float64
	}
	if netMarginPct.Valid {
		f.NetMarginPct = &netMarginPct.Float64
	}
	if operatingMarginPct.Valid {
		f.OperatingMarginPct = &operatingMarginPct.Float64
	}
	if roicPct.Valid {
		f.ROICPct = &roicPct.Float64
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

func (s *Store) SetInsiderTrades(symbol string, trades []openinsider.Trade) error {
	blob, err := json.Marshal(trades)
	if err != nil {
		return fmt.Errorf("encoding insider trades: %w", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO insider_trades (symbol, data, fetched_at)
		VALUES (?, ?, ?)
		ON CONFLICT(symbol) DO UPDATE SET
			data = excluded.data,
			fetched_at = excluded.fetched_at
	`, symbol, string(blob), time.Now().Unix())
	return err
}

func (s *Store) InsiderTrades(symbol string) (trades []openinsider.Trade, fetchedAt time.Time, ok bool, err error) {
	var blob string
	var ts int64
	row := s.db.QueryRow(`SELECT data, fetched_at FROM insider_trades WHERE symbol = ?`, symbol)
	if scanErr := row.Scan(&blob, &ts); scanErr != nil {
		if scanErr == sql.ErrNoRows {
			return nil, time.Time{}, false, nil
		}
		return nil, time.Time{}, false, scanErr
	}
	if err := json.Unmarshal([]byte(blob), &trades); err != nil {
		return nil, time.Time{}, false, fmt.Errorf("decoding stored insider trades: %w", err)
	}
	return trades, time.Unix(ts, 0).UTC(), true, nil
}

func (s *Store) DemandAlertSent(symbol string) (bool, error) {
	var sentAt int64
	err := s.db.QueryRow(`SELECT sent_at FROM demand_alerts WHERE symbol = ?`, symbol).Scan(&sentAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) MarkDemandAlert(symbol string) error {
	_, err := s.db.Exec(`
		INSERT INTO demand_alerts (symbol, sent_at) VALUES (?, ?)
		ON CONFLICT(symbol) DO UPDATE SET sent_at = excluded.sent_at
	`, symbol, time.Now().Unix())
	return err
}

func (s *Store) ClearDemandAlert(symbol string) error {
	_, err := s.db.Exec(`DELETE FROM demand_alerts WHERE symbol = ?`, symbol)
	return err
}

// UpsertSymbol adds symbol to the tracked list if it isn't already there.
func (s *Store) UpsertSymbol(symbol string) error {
	_, err := s.db.Exec(`
		INSERT INTO symbols (symbol, added_at) VALUES (?, ?)
		ON CONFLICT(symbol) DO NOTHING
	`, symbol, time.Now().Unix())
	return err
}

// ListSymbols returns every tracked symbol, alphabetically.
func (s *Store) ListSymbols() ([]string, error) {
	rows, err := s.db.Query(`SELECT symbol FROM symbols ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var symbols []string
	for rows.Next() {
		var symbol string
		if err := rows.Scan(&symbol); err != nil {
			return nil, err
		}
		symbols = append(symbols, symbol)
	}
	return symbols, rows.Err()
}

// SymbolInfo pairs a tracked ticker with its company name, if known.
type SymbolInfo struct {
	Symbol string
	Name   string
	Note   string
}

// ListSymbolInfo returns every tracked symbol with its company name,
// alphabetically by symbol. Name is empty until SetSymbolName has been
// called for that symbol.
func (s *Store) ListSymbolInfo() ([]SymbolInfo, error) {
	rows, err := s.db.Query(`SELECT symbol, name, note FROM symbols ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var infos []SymbolInfo
	for rows.Next() {
		var info SymbolInfo
		if err := rows.Scan(&info.Symbol, &info.Name, &info.Note); err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, rows.Err()
}

// SetSymbolName records the company name for an already-tracked symbol.
func (s *Store) SetSymbolName(symbol, name string) error {
	_, err := s.db.Exec(`UPDATE symbols SET name = ? WHERE symbol = ?`, name, symbol)
	return err
}

func (s *Store) SetSymbolNote(symbol, note string) (bool, error) {
	res, err := s.db.Exec(`UPDATE symbols SET note = ? WHERE symbol = ?`, note, symbol)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SymbolName returns the stored company name for symbol, or "" if unknown.
func (s *Store) SymbolName(symbol string) (string, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM symbols WHERE symbol = ?`, symbol).Scan(&name)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}

type FetchTimes struct {
	Symbol       string
	Name         string
	Bars         time.Time
	Fundamentals time.Time
	CompanyData  time.Time
	Insider      time.Time
}

func (s *Store) ListFetchTimes() ([]FetchTimes, error) {
	rows, err := s.db.Query(`
		SELECT s.symbol, s.name, s.bars_fetched_at, COALESCE(f.fetched_at, 0), COALESCE(c.fetched_at, 0), COALESCE(i.fetched_at, 0)
		FROM symbols s
		LEFT JOIN fundamentals f ON f.symbol = s.symbol
		LEFT JOIN company_data c ON c.symbol = s.symbol
		LEFT JOIN insider_trades i ON i.symbol = s.symbol
		ORDER BY s.symbol
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FetchTimes
	for rows.Next() {
		var ft FetchTimes
		var bars, fundamentals, companyData, insider int64
		if err := rows.Scan(&ft.Symbol, &ft.Name, &bars, &fundamentals, &companyData, &insider); err != nil {
			return nil, err
		}
		ft.Bars = unixOrZero(bars)
		ft.Fundamentals = unixOrZero(fundamentals)
		ft.CompanyData = unixOrZero(companyData)
		ft.Insider = unixOrZero(insider)
		out = append(out, ft)
	}
	return out, rows.Err()
}

func unixOrZero(ts int64) time.Time {
	if ts == 0 {
		return time.Time{}
	}
	return time.Unix(ts, 0).UTC()
}
