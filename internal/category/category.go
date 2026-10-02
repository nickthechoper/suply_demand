package category

import "strings"

type theme struct {
	name    string
	symbols string
}

var themes = []theme{
	{"AI Chips & Accelerators", "NVDA AMD AVGO MRVL INTC ARM QCOM ALAB CRDO LSCC MPWR NVTS SITM"},
	{"Foundry & Semi Equipment", "TSM ASML AMAT LRCX KLAC TER ONTO ASYS AMKR GFS FORM CAMT ACLS UCTT ICHR COHU"},
	{"Memory & Storage", "MU WDC STX SNDK PSTG NTAP"},
	{"Networking & Optical", "ANET CSCO CIEN COHR LITE AAOI FN GLW APH CLS CALX NOK ERIC"},
	{"AI Servers & Hardware", "SMCI DELL HPE IBM JBL FLEX"},
	{"Data Center Power & Cooling", "VRT ETN GEV PWR EME FIX NVT MOD HUBB POWL AAON JCI TT CARR"},
	{"Data Center REITs", "EQIX DLR IRM AMT CCI"},
	{"AI Cloud & Neoclouds", "CRWV NBIS IREN CIFR WULF APLD CORZ BTDR HUT GLXY"},
	{"Hyperscalers", "MSFT AMZN GOOGL GOOG META ORCL BABA BIDU"},
	{"AI Software & Platforms", "PLTR SNOW DDOG MDB NOW CRM ADBE AI PATH SOUN BBAI TEM S CRWD ESTC GTLB CFLT DT NET MNDY HUBS WDAY INTU TWLO BRZE APP U"},
	{"AI Apps & Consumer", "DUOL TTD PINS SNAP RDDT SHOP SPOT NFLX"},
	{"Power & Energy for AI", "CEG VST TLN OKLO SMR NNE BE CCJ NRG FLNC LEU UEC"},
	{"Robotics & Autonomy", "TSLA ISRG SYM RR SERV PRCT MBLY LAZR LIDR OUST ABB ROK ZBRA"},
	{"AI Healthcare & Biotech", "RXRX SDGR ABSI RLAY GH VEEV DOCS"},
	{"Cybersecurity for AI", "PANW FTNT ZS OKTA CYBR RBRK"},
}

var bySymbol = func() map[string]string {
	m := make(map[string]string)
	for _, t := range themes {
		for _, s := range strings.Fields(t.symbols) {
			if _, taken := m[s]; !taken {
				m[s] = t.name
			}
		}
	}
	return m
}()

func Theme(symbol string) string {
	return bySymbol[strings.ToUpper(symbol)]
}

func Themes() []string {
	names := make([]string, len(themes))
	for i, t := range themes {
		names[i] = t.name
	}
	return names
}
