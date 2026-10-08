package store

import (
	"encoding/json"
	"fmt"

	"supplydemand/internal/credit"
)

type SPCompany struct {
	Symbol    string
	Name      string
	Credit    *credit.SPData
	Discovery *credit.Discovery
	Fetch     *credit.FetchState
}

func (s *Store) SPCompanies() ([]SPCompany, error) {
	rows, err := s.db.Query(`SELECT s.symbol, s.name, COALESCE(json_extract(c.data, '$.credit'), 'null'), COALESCE(json_extract(c.data, '$.creditDiscovery'), 'null'), COALESCE(json_extract(c.data, '$.creditFetch'), 'null') FROM symbols s LEFT JOIN company_data c ON c.symbol = s.symbol ORDER BY s.symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SPCompany
	for rows.Next() {
		var c SPCompany
		var data, discovery, fetch string
		if err := rows.Scan(&c.Symbol, &c.Name, &data, &discovery, &fetch); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &c.Credit); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(discovery), &c.Discovery); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(fetch), &c.Fetch); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) SetSPFetchState(symbol string, state credit.FetchState) error {
	blob, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO company_data (symbol, data, fetched_at)
		VALUES (?, json_object('creditFetch', json(?)), 0)
		ON CONFLICT(symbol) DO UPDATE SET data = json_set(company_data.data, '$.creditFetch', json(?))`, symbol, string(blob), string(blob))
	return err
}

func (s *Store) SetSPCredit(symbol string, data *credit.SPData, discovery credit.Discovery) error {
	if data != nil {
		if err := data.Normalize(); err != nil {
			return err
		}
		if discovery.Status != "verified" || discovery.URL != data.SourceURL {
			return fmt.Errorf("S&P discovery and rating identity differ")
		}
	}
	state, err := json.Marshal(discovery)
	if err != nil {
		return err
	}
	objectPairs, setPairs := "'creditDiscovery', json(?)", "'$.creditDiscovery', json(?)"
	args := []any{string(state)}
	if data != nil {
		blob, err := json.Marshal(data)
		if err != nil {
			return err
		}
		objectPairs += ", 'credit', json(?)"
		setPairs += ", '$.credit', json(?)"
		args = append(args, string(blob))
	}
	args = append([]any{symbol}, append(args, args...)...)
	_, err = s.db.Exec(`INSERT INTO company_data (symbol, data, fetched_at)
		VALUES (?, json_object(`+objectPairs+`), 0)
		ON CONFLICT(symbol) DO UPDATE SET data = json_set(company_data.data, `+setPairs+`)`, args...)
	return err
}

func (s *Store) SPCreditRatings() (map[string]credit.Rating, error) {
	rows, err := s.db.Query(`SELECT symbol, json_extract(data, '$.credit.rating'), COALESCE(json_extract(data, '$.credit.outlook'), '') FROM company_data WHERE json_extract(data, '$.credit.rating') IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]credit.Rating{}
	for rows.Next() {
		var symbol string
		d := credit.SPData{}
		if err := rows.Scan(&symbol, &d.Rating, &d.Outlook); err != nil {
			return nil, err
		}
		out[symbol] = d.AsRating()
	}
	return out, rows.Err()
}
