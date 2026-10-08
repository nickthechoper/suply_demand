# Supply & Demand Dashboard

## Run

```bash
go run ./cmd/dashboard
```

Then open http://localhost:8080

## S&P ratings script

The dashboard refreshes S&P issuer credit ratings daily and discovers entity IDs for tracked companies using [config/spglobal.json](config/spglobal.json). Save a browser session first. See [tools/sp-ratings/README.md](tools/sp-ratings/README.md) for setup, manual refreshes, and JSON/CSV export.
