package alert

import (
	"fmt"
	"html"
	"log"
	"math"
	"strings"
	"time"

	"supplydemand/internal/bondhist"
	"supplydemand/internal/finra"
	"supplydemand/internal/mail"
	"supplydemand/internal/store"
)

const (
	minHistoryChanges = 20
	maxStaleDays      = 4
	recentDays        = 10
)

var bondHorizons = []int{1, 5}

type BondConfig struct {
	Store  *store.Store
	Mailer Mailer
	To     []string
	CC     []string
	BCC    []string

	Sigma       float64
	MinMoveBp   float64
	HistoryDays int

	History func(b finra.Bond, days int) ([]finra.DailyYield, error)
	Now     func() time.Time
}

type BondChecker struct {
	cfg BondConfig
}

func NewBond(cfg BondConfig) *BondChecker {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.History == nil {
		cfg.History = func(b finra.Bond, days int) ([]finra.DailyYield, error) {
			history, err := bondhist.Sync(cfg.Store, b)
			if err != nil {
				return nil, err
			}
			return bondhist.Since(history, cfg.Now().UTC().AddDate(0, 0, -days)), nil
		}
	}
	return &BondChecker{cfg: cfg}
}

type bondMove struct {
	Horizon   int
	From      finra.DailyYield
	To        finra.DailyYield
	ChangeBp  float64
	TypicalBp float64
	Ratio     float64
}

func (m bondMove) direction() string {
	if m.ChangeBp > 0 {
		return "up"
	}
	return "down"
}

func sharpestMove(history []finra.DailyYield, sigma, minBp float64) (bondMove, bool) {
	n := len(history)
	var best bondMove
	found := false
	for _, h := range bondHorizons {
		count := n - 1 - h
		if count < minHistoryChanges {
			continue
		}
		var sumSq float64
		for i := h; i < n-1; i++ {
			d := (history[i].YieldPct - history[i-h].YieldPct) * 100
			sumSq += d * d
		}
		typical := math.Sqrt(sumSq / float64(count))
		if typical == 0 {
			continue
		}
		change := (history[n-1].YieldPct - history[n-1-h].YieldPct) * 100
		ratio := change / typical
		if math.Abs(ratio) < sigma || math.Abs(change) < minBp {
			continue
		}
		if !found || math.Abs(ratio) > math.Abs(best.Ratio) {
			best = bondMove{Horizon: h, From: history[n-1-h], To: history[n-1], ChangeBp: change, TypicalBp: typical, Ratio: ratio}
			found = true
		}
	}
	return best, found
}

func (c *BondChecker) Check(symbol string) {
	bonds, _, ok, err := c.cfg.Store.Bonds(symbol)
	if err != nil {
		log.Printf("alert: loading bonds for %s: %v", symbol, err)
		return
	}
	if !ok {
		return
	}
	now := c.cfg.Now().UTC()
	bond, found := finra.Headline(bonds, now)
	if !found {
		return
	}

	history, err := c.cfg.History(bond, c.cfg.HistoryDays)
	if err != nil {
		log.Printf("alert: loading yield history for %s: %v", symbol, err)
		return
	}
	if len(history) == 0 {
		return
	}
	latest, err := time.Parse(time.DateOnly, history[len(history)-1].Date)
	if err != nil || now.Sub(latest) > maxStaleDays*24*time.Hour {
		return
	}

	sent, err := c.cfg.Store.BondAlertDirection(symbol)
	if err != nil {
		log.Printf("alert: reading bond alert state for %s: %v", symbol, err)
		return
	}

	move, sharp := sharpestMove(history, c.cfg.Sigma, c.cfg.MinMoveBp)
	switch {
	case sharp && sent != move.direction():
		c.send(symbol, bond, move, history)
	case !sharp && sent != "":
		if err := c.cfg.Store.ClearBondAlert(symbol); err != nil {
			log.Printf("alert: re-arming bond alert for %s: %v", symbol, err)
			return
		}
		log.Printf("alert: %s bond yield moves back to normal, alert re-armed", symbol)
	}
}

func (c *BondChecker) send(symbol string, bond finra.Bond, m bondMove, history []finra.DailyYield) {
	name, err := c.cfg.Store.SymbolName(symbol)
	if err != nil {
		log.Printf("alert: looking up name for %s: %v", symbol, err)
	}
	label := symbol
	if name != "" {
		label = fmt.Sprintf("%s (%s)", symbol, name)
	}

	verb := "jumped"
	if m.ChangeBp < 0 {
		verb = "dropped"
	}
	span := "1 trading day"
	if m.Horizon > 1 {
		span = fmt.Sprintf("%d trading days", m.Horizon)
	}
	subject := fmt.Sprintf("%s bond yield %s %+.0f bp in %s to %.2f%%", symbol, verb, m.ChangeBp, span, m.To.YieldPct)
	headline := fmt.Sprintf("The yield on %s's %s bond %s from %.2f%% to %.2f%% in %s, %.1f times its typical move over that span.",
		label, bond.Label(), verb, m.From.YieldPct, m.To.YieldPct, span, math.Abs(m.Ratio))

	rating := strings.Join(nonEmpty(bond.Moodys, bond.SP), " / ")
	if rating == "" {
		rating = "n/a"
	}
	rows := [][2]string{
		{"Bond", fmt.Sprintf("%s (CUSIP %s)", bond.Label(), bond.CUSIP)},
		{"Change", fmt.Sprintf("%+.0f bp (%s %.2f%% to %s %.2f%%)", m.ChangeBp, m.From.Date, m.From.YieldPct, m.To.Date, m.To.YieldPct)},
		{"Typical move", fmt.Sprintf("%.0f bp over %s, from the last %d days of trades", m.TypicalBp, span, c.cfg.HistoryDays)},
		{"Rating (Moody's / S&P)", rating},
	}
	start := max(0, len(history)-recentDays)
	var recent []string
	for _, d := range history[start:] {
		recent = append(recent, fmt.Sprintf("%s %.2f%%", d.Date, d.YieldPct))
	}
	rows = append(rows, [2]string{"Recent daily yields", strings.Join(recent, ", ")})

	text := headline + "\n\n"
	page := "<p>" + html.EscapeString(headline) + "</p><table cellpadding=\"4\">"
	for _, r := range rows {
		text += r[0] + ": " + r[1] + "\n"
		page += "<tr><td><b>" + html.EscapeString(r[0]) + "</b></td><td>" + html.EscapeString(r[1]) + "</td></tr>"
	}
	page += "</table>"

	if err := c.cfg.Mailer.Send(mail.Message{
		To:      c.cfg.To,
		CC:      c.cfg.CC,
		BCC:     c.cfg.BCC,
		Subject: subject,
		Text:    text,
		HTML:    page,
	}); err != nil {
		log.Printf("alert: could not email bond alert for %s: %v", symbol, err)
		return
	}
	if err := c.cfg.Store.MarkBondAlert(symbol, m.direction()); err != nil {
		log.Printf("alert: emailed %s but could not record it, it may be sent again: %v", symbol, err)
		return
	}
	log.Printf("alert: emailed bond yield alert for %s: %+.0f bp in %s", symbol, m.ChangeBp, span)
}

func nonEmpty(values ...string) []string {
	var out []string
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
