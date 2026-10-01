// Package alert emails a notification when a tracked symbol's price falls
// into the low end of its demand zone.
package alert

import (
	"fmt"
	"html"
	"log"

	"supplydemand/internal/mail"
	"supplydemand/internal/store"
	"supplydemand/internal/yahoo"
	"supplydemand/internal/zones"
)

type Mailer interface {
	Send(mail.Message) error
}

type Config struct {
	Store  *store.Store
	Mailer Mailer
	To     []string
	CC     []string
	BCC    []string

	// Lookback and Params must match the values the dashboard uses so the
	// emailed zone is the one shown on the chart.
	Lookback int
	Params   zones.Params

	// Depth is how far up the demand zone, as a fraction of its height
	// measured from the bottom, the price may be and still trigger an alert.
	Depth float64
}

type Checker struct {
	cfg Config
}

func New(cfg Config) *Checker {
	return &Checker{cfg: cfg}
}

type status struct {
	Price   float64
	Trigger float64
	Zone    zones.Result
}

// evaluate reports the latest close against the demand zone. ok is false when
// there is not enough data to form a demand zone.
func evaluate(bars []yahoo.Bar, params zones.Params, depth float64) (s status, ok bool) {
	if len(bars) == 0 {
		return s, false
	}
	zoneBars := make([]zones.Bar, len(bars))
	for i, b := range bars {
		zoneBars[i] = zones.Bar{High: b.High, Low: b.Low, Volume: b.Volume}
	}
	res := zones.Compute(zoneBars, params)
	if !res.DemandReached {
		return s, false
	}
	return status{
		Price:   bars[len(bars)-1].Close,
		Trigger: res.DemandBtm + (res.DemandTop-res.DemandBtm)*depth,
		Zone:    res,
	}, true
}

// Check sends at most one email per visit to the low end of the demand zone.
// Once sent, the symbol stays quiet until its price climbs back above the top
// of the demand zone, which re-arms the alert.
func (c *Checker) Check(symbol string) {
	bars, err := c.cfg.Store.LatestBars(symbol, c.cfg.Lookback)
	if err != nil {
		log.Printf("alert: loading bars for %s: %v", symbol, err)
		return
	}
	s, ok := evaluate(bars, c.cfg.Params, c.cfg.Depth)
	if !ok {
		return
	}

	sent, err := c.cfg.Store.DemandAlertSent(symbol)
	if err != nil {
		log.Printf("alert: reading alert state for %s: %v", symbol, err)
		return
	}

	switch {
	case !sent && s.Price <= s.Trigger:
		c.send(symbol, s)
	case sent && s.Price > s.Zone.DemandTop:
		if err := c.cfg.Store.ClearDemandAlert(symbol); err != nil {
			log.Printf("alert: re-arming %s: %v", symbol, err)
			return
		}
		log.Printf("alert: %s left the demand zone, alert re-armed", symbol)
	}
}

func (c *Checker) send(symbol string, s status) {
	name, err := c.cfg.Store.SymbolName(symbol)
	if err != nil {
		log.Printf("alert: looking up name for %s: %v", symbol, err)
	}
	label := symbol
	if name != "" {
		label = fmt.Sprintf("%s (%s)", symbol, name)
	}

	z := s.Zone
	subject := fmt.Sprintf("%s at low end of demand zone: %.2f", symbol, s.Price)
	headline := fmt.Sprintf("%s closed at %.2f, inside the bottom %.0f%% of its demand zone.", label, s.Price, c.cfg.Depth*100)
	rows := [][2]string{
		{"Demand zone", fmt.Sprintf("%.2f - %.2f", z.DemandBtm, z.DemandTop)},
		{"Alert level", fmt.Sprintf("%.2f", s.Trigger)},
		{"Equilibrium", fmt.Sprintf("%.2f", z.EquilibriumAvg)},
		{"Supply zone", fmt.Sprintf("%.2f - %.2f", z.SupplyBtm, z.SupplyTop)},
	}

	text := headline + "\n\n"
	page := "<p>" + html.EscapeString(headline) + "</p><table cellpadding=\"4\">"
	for _, r := range rows {
		text += r[0] + ": " + r[1] + "\n"
		page += "<tr><td><b>" + r[0] + "</b></td><td>" + r[1] + "</td></tr>"
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
		log.Printf("alert: could not email alert for %s: %v", symbol, err)
		return
	}
	if err := c.cfg.Store.MarkDemandAlert(symbol); err != nil {
		log.Printf("alert: emailed %s but could not record it, it may be sent again: %v", symbol, err)
		return
	}
	log.Printf("alert: emailed demand zone alert for %s at %.2f", symbol, s.Price)
}
