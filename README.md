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
- `-symbols-csv` (default none) — path to a CSV file with a `Symbol` column to import tickers from (e.g. a screener export); merges with `-symbols`
- `-lookback` (default `500`) — number of most recent daily bars used to build zones
- `-threshold` (default `10.0`) — volume %% that defines a zone edge
- `-resolution` (default `50`) — number of price bins spanning the lookback range
- `-db` (default `data/dashboard.db`) — SQLite file path
- `-addr` (default `:8080`) — server address
- `-fetch-range` (default `5y`) — Yahoo Finance history range fetched per symbol
- `-fetch-pacing` (default `1s`) — delay between individual Yahoo Finance requests made by the background worker

The server starts immediately; a background worker fetches bars, fundamentals,
and company data for every tracked symbol asynchronously, refreshing whatever's
missing or more than 20 hours old. With a large tracked list (hundreds of
tickers via `-symbols-csv`) a full pass can take a while, so a newly-added
symbol's chart may be empty for a bit until its turn comes up — reload once
the server log shows it's been fetched. Click "Refresh data" in the dashboard
to force an immediate re-fetch of the currently open ticker.

## How the zones are computed

The original Pine Script builds a volume profile from intrabar (lower-timeframe)
ticks. Yahoo's free daily data doesn't include that, so each daily bar's volume
is distributed across price bins proportional to how much of the bar's
`[low, high]` range overlaps each bin — the standard approximation used by
visible-range volume profile tools when only OHLCV bars are available. See
[internal/zones/zones.go](internal/zones/zones.go) for details.
