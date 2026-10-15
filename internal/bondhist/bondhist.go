package bondhist

import (
	"time"

	"supplydemand/internal/finra"
	"supplydemand/internal/store"
)

const (
	BackfillDays = 730
	refreshEvery = 30 * time.Minute
	overlapDays  = 3
)

func Sync(st *store.Store, b finra.Bond) ([]finra.DailyYield, error) {
	return sync(st, b, time.Now().UTC(), finra.FetchYieldHistory)
}

func sync(st *store.Store, b finra.Bond, now time.Time, fetch func(finra.Bond, time.Time) ([]finra.DailyYield, error)) ([]finra.DailyYield, error) {
	fetchedAt, ok, err := st.BondHistoryFetchedAt(b.Symbol)
	if err != nil {
		return nil, err
	}
	if ok && now.Sub(fetchedAt) < refreshEvery {
		return st.BondYields(b.Symbol)
	}

	from := now.AddDate(0, 0, -BackfillDays)
	if ok {
		from = fetchedAt.AddDate(0, 0, -overlapDays)
	}
	points, err := fetch(b, from)
	if err != nil {
		return nil, err
	}
	if err := st.UpsertBondYields(b.Symbol, points); err != nil {
		return nil, err
	}
	return st.BondYields(b.Symbol)
}

func Since(points []finra.DailyYield, from time.Time) []finra.DailyYield {
	cutoff := from.Format(time.DateOnly)
	for i, p := range points {
		if p.Date >= cutoff {
			return points[i:]
		}
	}
	return []finra.DailyYield{}
}
