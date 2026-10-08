# S&P issuer ratings fetcher

Fetch the issuer credit ratings visible on an S&P Global organization page using your own login. The default URL is Grab Holdings Ltd. (entity 682461).

## Setup and run

Requires Node.js 20 or newer. From the project root:

```powershell
cd tools/sp-ratings
npm install
npx playwright install chromium
node fetch.cjs
```

The script opens a browser and prompts for your username and hidden password in the local terminal if login is required. Credentials can also come from `SP_USERNAME` and `SP_PASSWORD` environment variables. Do not put your password in the script or share it in chat. Automatic form filling is limited to HTTPS S&P domains. If the form is different, uses an external identity provider, or requires MFA, complete sign-in in the browser when prompted.

To use your installed Chrome instead of downloading Chromium:

```powershell
node fetch.cjs --channel chrome
```

To handle the entire login yourself in the browser:

```powershell
node fetch.cjs --manual
```

## Output

Successful runs write `output/682461.json` and `output/682461.csv`. The output includes company name, entity ID, source URL, UTC fetch timestamp, and each issuer rating's type, rating, rating date, last review date, regulatory identifiers, outlook, and outlook date. Dates from the page retain their original format. Regulatory disclosure links and other page sections are excluded.

```powershell
node fetch.cjs --url "https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/682461" --out output/grab
```

`--out` is a file prefix; the script appends `.json` and `.csv`. Without it, the prefix is `output/<entityId>`. Existing output files at that prefix are overwritten only after ratings have been extracted.

## Reusing login

Session storage is optional and is saved only when `--session` is supplied:

```powershell
node fetch.cjs --manual --session .auth/session.json
node fetch.cjs --headless --session .auth/session.json
```

The session file contains sensitive authentication tokens. Keep it private; `.auth/` is ignored by Git. Expired sessions, MFA, and login challenges require another visible login. The script does not bypass them. Session state uses Playwright's cookies/local storage/IndexedDB support; accounts that depend on other browser state may need to log in each run.

Use `--timeout 60` for slower pages. Run `npm test` for extraction checks. Live login and selectors must be verified with an authorized account; the included fixtures reproduce the supplied screenshot and do not constitute a live S&P login test.

## Daily dashboard refresh for all tracked companies

From the project root, save a session once and start the dashboard:

```powershell
node tools/sp-ratings/fetch.cjs --channel chrome --manual --session tools/sp-ratings/.auth/session.json
go run ./cmd/dashboard
```

If your terminal is already in `tools/sp-ratings`, the session command is `node fetch.cjs --channel chrome --manual --session .auth/session.json`.

`config/spglobal.json` controls the independent S&P background worker. While the dashboard runs, it checks for due work every minute, refreshes verified issuers once every 24 hours, and attempts entity discovery for every tracked symbol that has a stored company name. The worker reuses one browser for a batch of up to 25 companies, with two seconds between companies. It runs independently of Yahoo authentication. Restart the dashboard to load code/configuration changes. No operating-system scheduled task is installed, so refreshes pause while the dashboard is stopped and resume when it starts.

Discovery uses S&P's Find a Rating page and organization links. It requires the issuer's full normalized name to match the tracked company's stored name, then verifies the name again on the ratings page. Corporate suffixes such as Inc., Corp., and Ltd. are ignored. Subsidiaries, renamed issuers, multiple matching entities, and other uncertain matches are left unresolved. `unmatched` means no exact match was found in the loaded search results, not that the company is unrated. Unresolved results are retried after seven days. Failures use exponential retry delays from five minutes to six hours and retain the previous successful credit data. Companies without stored names wait for the existing name worker.

Eligible companies are queued by their oldest S&P check or batch attempt, with companies that have never been attempted first. A verified issuer does not jump ahead of an older discovery job. The worker records each selected batch before starting its browser and persists retry times and failure counts, so failed/aborted batches and dashboard restarts continue through the queue instead of repeatedly starting at A. Alphabetical order only breaks equal timestamps, such as companies that have never been attempted. Successful lookups reset retry failures; daily and discovery refresh intervals still determine when work becomes eligible.

The `entities` configuration map provides explicit symbol-to-issuer URLs; Grab's verified URL is included. Override an unresolved match by supplying its verified issuer URL. Matching issuer and company names are still required. Disable the worker with `"enabled": false`, or use `-sp-config` to select another configuration file.

Ratings are stored in `company_data.data.credit`, with all extracted issuer rows, source URL, entity ID, fetch time, and normalized rating/outlook. The dashboard selects Foreign Currency LT first, then Local Currency LT. Discovery state is stored in `company_data.data.creditDiscovery`; persistent attempt/retry state is in `company_data.data.creditFetch`. Yahoo updates preserve all three fields. Credit updates preserve Yahoo's `company_data.fetched_at`; `credit.fetchedAt` independently controls credit freshness. A credit-only row starts with `fetched_at = 0` so Yahoo data remains due for fetching. No table migration is required.

The dashboard uses S&P issuer ratings, with the existing manual ratings CSV as the highest-priority override. `/api/company` includes the full S&P snapshot under `credit.details` when S&P is the selected source. The previous SEC filing extraction worker and API fallback have been removed.

Run one batch of due companies, or import a previously fetched file:

```powershell
go run ./cmd/credit-refresh
go run ./cmd/credit-refresh -symbol AAPL
go run ./cmd/credit-refresh -symbol AAPL -headed -manual
go run ./cmd/credit-refresh -symbol GRAB -import tools/sp-ratings/output/682461.json
go run ./cmd/credit-refresh -candidates
```

These commands run from the project root. Both support `-db`; the batch command supports `-config`. An import checks the stored company name and issuer identity before writing. A single batch processes at most the configured batch size; the running dashboard continues through the remaining symbols over subsequent cycles.

If unattended Chrome returns a verification block, use `-headed -manual`. This opens a visible browser and waits up to three minutes at a login or verification page. Complete verification normally in that browser; the batch resumes automatically and updates the saved session. If verification cannot be completed, the batch stops and existing ratings are preserved. `-headed` alone shows the browser without waiting. Headless batches were blocked by S&P even with saved session state, so `config/spglobal.json` sets `"headed": true`; daily batches open Chrome but never wait for manual verification.

Access problems (verification page, expired login, closed browser) make the batch exit with code 3. The worker treats this as a block rather than a company failure: it restores the batch's retry state, charges nothing to the companies, and pauses all S&P work for `blockedPause` (default `1h`). Other batch failures still use the per-company exponential backoff.

Between batches that did work, the worker waits `batchInterval` (default `5m`); with nothing due it polls every minute.

When discovery returns `unmatched` or `ambiguous`, up to ten issuer candidates from the search results are stored in `creditDiscovery.candidates`. List them, then add the right URL to `entities` in `config/spglobal.json`:

```powershell
go run ./cmd/credit-refresh -candidates
```

Name matching ignores case, accents, corporate suffixes and a leading "The"; `tools/sp-ratings/name-cases.json` holds fixtures shared by the Go and JS implementations. The dashboard selects the first rated Foreign Currency LT, then the first rated Local Currency LT, and uses `NR`/`WR` only when no rated long-term entry exists. A company whose S&P result fails validation (no long-term rating, different issuer name, URL mismatch) is recorded as a failed company without aborting the rest of the batch.

On Linux and macOS a cancelled batch sends SIGTERM so Playwright can close the browser, and is force-killed after 15 seconds. The daily worker does not poll news or publication feeds; the Ratings Actions page is a possible future trigger.
