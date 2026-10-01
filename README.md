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

## Demand zone email alerts

Pass `-alert-to` to get an email when a tracked symbol's latest close falls into
the bottom part of its demand zone. "Low end" is the bottom `-alert-depth` of the
zone's height (default `0.05`, i.e. the bottom 5%). You get one email per
visit: the alert re-arms once the price climbs back above the top of the demand
zone. Alert state is stored in the database, so restarting doesn't resend.

SMTP settings live in `config/mail.json` (user, pass, host, port), which is
git-ignored. Then:

```powershell
go run ./cmd/dashboard -alert-to you@example.com
```

- `-alert-to` — comma-separated recipients; alerts are off unless this is set
- `-alert-cc` / `-alert-bcc` — optional comma-separated CC and BCC recipients
- `-mail-config` (default `config/mail.json`) — SMTP settings file
- `-alert-smtp-host` / `-alert-smtp-port` / `-alert-smtp-user` — override the file; port defaults to 587; port 465 uses implicit TLS, other ports use STARTTLS
- `-alert-from` — sender address (defaults to the SMTP user); `-alert-from-name` sets the display name (default `Supply & Demand Dashboard`)
- `-alert-depth` (default `0.05`) — fraction of the demand zone, measured up from its bottom, that triggers the alert
- `ALERT_SMTP_PASSWORD` — overrides the password in the mail config (Gmail needs an app password)

The list view has a matching "Demand Low" tab that shows only the symbols currently
inside that low end, using the same `-alert-depth`.

Alerts are checked each time the background worker stores fresh bars, so they
fire within a worker cycle of the price moving (Yahoo's latest daily bar
tracks the live price during market hours).

The server starts immediately; a background worker fetches bars, fundamentals,
and company data for every tracked symbol asynchronously, refreshing whatever's
missing or more than 20 hours old. With a large tracked list (hundreds of
tickers via `-symbols-csv`) a full pass can take a while, so a newly-added
symbol's chart may be empty for a bit until its turn comes up — reload once
the server log shows it's been fetched. Click "Refresh data" in the dashboard
to force an immediate re-fetch of the currently open ticker.

## Insider trades

http://localhost:8080/insider.html looks up SEC Form 4 insider trades for any
ticker (not just tracked ones) via OpenInsider, over the last 30 days to 2
years (365 by default). It opens with a plain-language net buying/selling
summary and open-market totals, plots every filing on the share price (from the
local store for tracked tickers, Yahoo otherwise; click a marker to filter to
that day), ranks insiders by open-market value, and lists all filings in a
sortable, filterable table linking to each SEC filing. Filings OpenInsider
flags as containing errors are marked. Results are cached in memory for
10 minutes. Also reachable from the "Insider trades" link in the chart and
list views, or directly as `insider.html?symbol=NVDA&days=90`.

## How the zones are computed

The original Pine Script builds a volume profile from intrabar (lower-timeframe)
ticks. Yahoo's free daily data doesn't include that, so each daily bar's volume
is distributed across price bins proportional to how much of the bar's
`[low, high]` range overlaps each bin — the standard approximation used by
visible-range volume profile tools when only OHLCV bars are available. See
[internal/zones/zones.go](internal/zones/zones.go) for details.
