package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"supplydemand/internal/alert"
	"supplydemand/internal/credit"
	"supplydemand/internal/mail"
	"supplydemand/internal/sec"
	"supplydemand/internal/server"
	"supplydemand/internal/store"
	"supplydemand/internal/worker"
	"supplydemand/internal/zones"
)

var defaultPeers = map[string][]string{
	"MCD":  {"YUM", "SBUX", "QSR", "WEN"},
	"GRAB": {"UBER", "DASH", "SE"},
	"JOBY": {"ACHR", "LILM", "EH"},
}

func main() {
	symbols := flag.String("symbols", "MCD,GRAB,JOBY", "comma-separated ticker symbols to track")
	symbolsCSV := flag.String("symbols-csv", "", "path to a CSV file with a \"Symbol\" column to import tickers from")
	dbPath := flag.String("db", "data/dashboard.db", "path to the SQLite database file")
	lookback := flag.Int("lookback", 500, "number of most recent daily bars used to build zones")
	chartBars := flag.Int("chart-bars", 800, "number of most recent daily bars served to the chart (>= lookback, gives range buttons like 2Y room to work)")
	threshold := flag.Float64("threshold", 10.0, "volume threshold %% that defines a zone edge")
	resolution := flag.Int("resolution", 50, "number of price bins spanning the lookback's price range")
	addr := flag.String("addr", ":8080", "address to serve the dashboard on")
	webDir := flag.String("webdir", "web", "directory containing the dashboard's static frontend")
	fetchRange := flag.String("fetch-range", "5y", "Yahoo Finance history range to fetch")
	fetchPacing := flag.Duration("fetch-pacing", time.Second, "delay between individual Yahoo Finance requests made by the background worker")
	logPath := flag.String("log-file", "data/dashboard.log", "file that receives all log output, including routine fetch activity kept off the terminal")
	alertTo := flag.String("alert-to", "", "comma-separated email addresses to notify when a symbol reaches the low end of its demand zone (enables alerts)")
	alertCC := flag.String("alert-cc", "", "comma-separated CC addresses for alert emails")
	alertBCC := flag.String("alert-bcc", "", "comma-separated BCC addresses for alert emails")
	alertFromName := flag.String("alert-from-name", "Supply & Demand Dashboard", "sender display name for alert emails")
	alertFrom := flag.String("alert-from", "", "sender address for alert emails (defaults to -alert-smtp-user)")
	alertDepth := flag.Float64("alert-depth", 0.05, "how far up the demand zone, as a fraction of its height from the bottom, still counts as its low end")
	smtpHost := flag.String("alert-smtp-host", "", "SMTP server host for alert emails, e.g. smtp.gmail.com")
	smtpPort := flag.String("alert-smtp-port", "", "SMTP server port (465 uses implicit TLS, anything else uses STARTTLS when offered); defaults to the mail config, then 587")
	smtpUser := flag.String("alert-smtp-user", "", "SMTP username; the password comes from ALERT_SMTP_PASSWORD or the mail config")
	mailConfigPath := flag.String("mail-config", "config/mail.json", "JSON file with SMTP user, pass, host and port; flags and ALERT_SMTP_PASSWORD override it")
	creditPath := flag.String("credit-ratings", "config/credit_ratings.csv", "CSV file with Symbol, Rating and Outlook columns holding S&P issuer credit ratings; edits are picked up without a restart")
	secUserAgent := flag.String("sec-user-agent", "", "identity sent to SEC EDGAR, e.g. \"Your Name you@example.com\"; overrides SEC_USER_AGENT and the SEC config file")
	secConfigPath := flag.String("sec-config", "config/sec.json", "JSON file with the userAgent identity sent to SEC EDGAR; enables reading S&P credit ratings from 10-K and 10-Q filings")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(*logPath), 0o755); err != nil {
		log.Fatalf("creating log directory: %v", err)
	}
	logFile, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Fatalf("opening log file: %v", err)
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(os.Stderr, logFile))
	worker.SetActivityLog(logFile)

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("opening store: %v", err)
	}
	defer st.Close()

	explicitSymbols := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "symbols" {
			explicitSymbols = true
		}
	})

	existing, err := st.ListSymbols()
	if err != nil {
		log.Fatalf("listing tracked symbols: %v", err)
	}
	if explicitSymbols || (len(existing) == 0 && *symbolsCSV == "") {
		for _, symbol := range strings.Split(*symbols, ",") {
			symbol = strings.TrimSpace(symbol)
			if symbol == "" {
				continue
			}
			if err := st.UpsertSymbol(symbol); err != nil {
				log.Fatalf("tracking symbol %s: %v", symbol, err)
			}
		}
	}

	if *symbolsCSV != "" {
		imported, err := importSymbolsCSV(st, *symbolsCSV)
		if err != nil {
			log.Fatalf("importing symbols from %s: %v", *symbolsCSV, err)
		}
		log.Printf("imported %d symbols from %s", imported, *symbolsCSV)
	}

	trackedSymbols, err := st.ListSymbols()
	if err != nil {
		log.Fatalf("listing tracked symbols: %v", err)
	}
	if len(trackedSymbols) == 0 {
		log.Fatalf("no symbols tracked; pass -symbols or -symbols-csv")
	}

	worker.BackfillNames(st, trackedSymbols)

	var afterBars func(symbol string)
	if *alertTo != "" {
		mailCfg, err := mail.LoadConfig(*mailConfigPath)
		if err != nil {
			log.Fatalf("loading mail config: %v", err)
		}
		password := mailCfg.Pass
		if env := os.Getenv("ALERT_SMTP_PASSWORD"); env != "" {
			password = env
			log.Printf("using SMTP password from the ALERT_SMTP_PASSWORD environment variable, not %s", *mailConfigPath)
		}
		sender, err := newAlertSender(
			firstNonEmpty(*smtpHost, mailCfg.Host),
			firstNonEmpty(*smtpPort, mailCfg.PortString(), "587"),
			firstNonEmpty(*smtpUser, mailCfg.User),
			password,
			*alertFrom,
			*alertFromName,
		)
		if err != nil {
			log.Fatalf("configuring alerts: %v", err)
		}
		afterBars = alert.New(alert.Config{
			Store:    st,
			Mailer:   sender,
			To:       splitList(*alertTo),
			CC:       splitList(*alertCC),
			BCC:      splitList(*alertBCC),
			Lookback: *lookback,
			Params:   zones.Params{ThresholdPct: *threshold, Resolution: *resolution},
			Depth:    *alertDepth,
		}).Check
		log.Printf("demand zone email alerts enabled for %s", *alertTo)
	}

	secFileAgent, err := sec.LoadUserAgent(*secConfigPath)
	if err != nil {
		log.Fatalf("loading SEC config: %v", err)
	}
	var secClient *sec.Client
	if agent := firstNonEmpty(*secUserAgent, os.Getenv("SEC_USER_AGENT"), secFileAgent); agent != "" {
		secClient = sec.NewClient(agent)
		log.Printf("reading S&P credit ratings from SEC filings")
	} else {
		log.Printf("credit ratings from SEC filings are off: set userAgent in %s, pass -sec-user-agent, or set SEC_USER_AGENT", *secConfigPath)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(ctx, trackedSymbols, worker.Config{
			Store:      st,
			Peers:      defaultPeers,
			FetchRange: *fetchRange,
			Pacing:     *fetchPacing,
			AfterBars:  afterBars,
			SEC:        secClient,
		})
	}()

	srv := server.New(st, server.Config{
		Symbols:      trackedSymbols,
		Lookback:     *lookback,
		ChartBars:    *chartBars,
		ThresholdPct: *threshold,
		Resolution:   *resolution,
		WebDir:       *webDir,
		Peers:        defaultPeers,
		DeepDemand:   *alertDepth,
		Credit:       credit.NewSource(*creditPath),
		SEC:          secClient,
	})

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: srv.Routes(),
	}

	serveErrs := make(chan error, 1)
	go func() {
		log.Printf("serving dashboard for %d symbols on http://localhost%s", len(trackedSymbols), *addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErrs <- err
		}
	}()

	select {
	case err := <-serveErrs:
		log.Fatalf("server error: %v", err)
	case <-ctx.Done():
		log.Printf("shutting down...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}

	select {
	case <-workerDone:
	case <-shutdownCtx.Done():
		log.Printf("worker did not stop before shutdown timeout")
	}
}

func newAlertSender(host, port, user, password, from, fromName string) (mail.Sender, error) {
	if host == "" {
		return mail.Sender{}, fmt.Errorf("SMTP host is required when -alert-to is set (-alert-smtp-host or the mail config)")
	}
	if user != "" && password == "" {
		return mail.Sender{}, fmt.Errorf("SMTP password is not set (ALERT_SMTP_PASSWORD or the mail config)")
	}
	if from == "" {
		from = user
	}
	if from == "" {
		return mail.Sender{}, fmt.Errorf("sender address is required (-alert-from or the SMTP user)")
	}
	return mail.Sender{Host: host, Port: port, User: user, Password: password, FromAddr: from, FromName: fromName}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func importSymbolsCSV(st *store.Store, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return 0, err
	}
	symbolCol := -1
	for i, col := range header {
		if strings.EqualFold(strings.TrimSpace(col), "Symbol") {
			symbolCol = i
			break
		}
	}
	if symbolCol == -1 {
		return 0, fmt.Errorf("no \"Symbol\" column found in %s", path)
	}

	count := 0
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, err
		}
		symbol := strings.TrimSpace(row[symbolCol])
		if symbol == "" {
			continue
		}
		symbol = strings.ReplaceAll(symbol, ".", "-")
		if err := st.UpsertSymbol(symbol); err != nil {
			return count, fmt.Errorf("tracking symbol %s: %w", symbol, err)
		}
		count++
	}

	return count, nil
}
