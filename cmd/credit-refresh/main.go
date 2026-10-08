package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"supplydemand/internal/credit"
	"supplydemand/internal/spglobal"
	"supplydemand/internal/store"
	"supplydemand/internal/worker"
)

func main() {
	db := flag.String("db", "data/dashboard.db", "SQLite database")
	config := flag.String("config", "config/spglobal.json", "S&P configuration")
	importPath := flag.String("import", "", "import a previously fetched S&P JSON file")
	symbol := flag.String("symbol", "", "restrict refresh to one tracked symbol, or select symbol for --import")
	headed := flag.Bool("headed", false, "open a visible browser for the refresh")
	candidates := flag.Bool("candidates", false, "list S&P issuer candidates for unresolved companies and exit")
	manual := flag.Bool("manual", false, "allow up to three minutes for browser sign-in/verification; implies -headed")
	flag.Parse()
	st, err := store.Open(*db)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if *candidates {
		if err := printCandidates(os.Stdout, st); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *importPath != "" {
		if err := importRating(st, *symbol, *importPath); err != nil {
			log.Fatal(err)
		}
		return
	}
	client, err := spglobal.Load(*config)
	if err != nil {
		log.Fatal(err)
	}
	if client == nil {
		log.Fatal("S&P refresh is disabled")
	}
	client.Config.Headed = client.Config.Headed || *headed || *manual
	client.Manual = *manual
	if !client.Ready() {
		log.Fatal("save an S&P session with fetch.cjs --manual --session before running the daily refresh")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	worker.SetActivityLog(os.Stdout)
	var selected []string
	if *symbol != "" {
		name, err := st.SymbolName(*symbol)
		if err != nil || name == "" {
			log.Fatal("symbol must be tracked and have a stored company name")
		}
		selected = []string{*symbol}
	}
	if err := worker.RefreshSPCredit(ctx, st, client, selected...); err != nil {
		log.Fatal(err)
	}
}

func printCandidates(w io.Writer, st *store.Store) error {
	companies, err := st.SPCompanies()
	if err != nil {
		return err
	}
	for _, c := range companies {
		if c.Discovery == nil || len(c.Discovery.Candidates) == 0 {
			continue
		}
		fmt.Fprintf(w, "%s (%s) %s\n", c.Symbol, c.Name, c.Discovery.Status)
		for _, candidate := range c.Discovery.Candidates {
			fmt.Fprintf(w, "  %s  %s\n", candidate.Name, candidate.URL)
		}
	}
	return nil
}

func importRating(st *store.Store, symbol, file string) error {
	name, err := st.SymbolName(symbol)
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("symbol must be tracked and have a company name")
	}
	blob, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var data credit.SPData
	if err := json.Unmarshal(blob, &data); err != nil {
		return err
	}
	if spglobal.NormalizeName(data.EntityName) != spglobal.NormalizeName(name) {
		return fmt.Errorf("S&P issuer name does not match %s (%s)", symbol, name)
	}
	if err := st.SetSPCredit(symbol, &data, credit.Discovery{Status: "verified", URL: data.SourceURL, Name: data.EntityName, CheckedAt: time.Now().UTC()}); err != nil {
		return err
	}
	fmt.Printf("Stored %s %s for %s in company_data.data.credit\n", data.Rating, data.Outlook, symbol)
	return nil
}
