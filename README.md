# Supply & Demand Dashboard

A local Go replacement for LuxAlgo's "Supply and Demand Fixed Range" Pine Script
indicator. Fetches daily OHLCV data for one or more tickers (default: MCD, GRAB)
from Yahoo Finance, stores it in SQLite, computes volume-based supply/demand/
equilibrium zones, and serves a candlestick dashboard in the browser with a
ticker selector to switch between them.

## Run

```bash
go run ./cmd/dashboard
```

Then open http://localhost:8080

## Flags

- `-symbols` (default `MCD,GRAB`) — comma-separated tickers to track
- `-lookback` (default `500`) — number of most recent daily bars used to build zones
- `-threshold` (default `10.0`) — volume %% that defines a zone edge
- `-resolution` (default `50`) — number of price bins spanning the lookback range
- `-db` (default `data/dashboard.db`) — SQLite file path
- `-addr` (default `:8080`) — server address
- `-fetch-range` (default `5y`) — Yahoo Finance history range fetched on startup

Data auto-refreshes on startup if the stored data is more than 20 hours old.
Click "Refresh data" in the dashboard to force a re-fetch at any time.

## How the zones are computed

The original Pine Script builds a volume profile from intrabar (lower-timeframe)
ticks. Yahoo's free daily data doesn't include that, so each daily bar's volume
is distributed across price bins proportional to how much of the bar's
`[low, high]` range overlaps each bin — the standard approximation used by
visible-range volume profile tools when only OHLCV bars are available. See
[internal/zones/zones.go](internal/zones/zones.go) for details.
