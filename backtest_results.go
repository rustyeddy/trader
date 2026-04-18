package trader

import (
	"bytes"
	"os"
	"text/template"
	"time"

	tlog "github.com/rustyeddy/trader/log"
	"github.com/rustyeddy/trader/types"
)

// Result is a lightweight summary of a backtest run.
type Result struct {
	Balance types.Money
	Equity  types.Money

	Trades int
	Wins   int
	Losses int

	Start types.Timestamp
	End   types.Timestamp
}

// BacktestRunRow mirrors backtest_runs table.
type BacktestRun struct {
	RunID     string
	Name      string
	Kind      string
	Created   types.Timestamp
	Timeframe string
	Dataset   string

	// Instrument traded in this backtest
	Instrument string
	Strategy   string
	Config     []byte // strategy config

	// Risk Management
	RiskPct  types.Rate  // 0.005 (0.5%)
	StopPips types.Price // e.g. 20
	RR       types.Rate  // take-profit multiple of risk, e.g. 2.0

	// Account and price timeframe
	Start types.Timestamp
	End   types.Timestamp

	// Results
	Trades int
	Wins   int
	Losses int

	// account info
	StartBalance types.Money
	EndBalance   types.Money

	// Derived / computed in Go
	NetPL        types.Money
	ReturnPct    types.Rate
	WinRate      types.Rate
	ProfitFactor types.Rate
	MaxDDPct     types.Rate

	GitCommit string
	OrgPath   string
	EquityPNG string

	Notes       []string
	NextActions []string
}

var backtestOrgFuncs = template.FuncMap{
	"mul100": func(x float64) float64 { return x * 100.0 },
	"orTime": func(t time.Time) time.Time {
		if t.IsZero() {
			return time.Now()
		}
		return t
	},
}

func (v *BacktestRun) WriteBacktestOrg() error {

	// 1. Create a new template and parse the template string
	t, err := template.New("backtest").Funcs(backtestOrgFuncs).Parse(BacktestOrgTemplate)
	if err != nil {
		tlog.Fatal("parse backtest template", "err", err)
	}

	// 2. Execute the template, writing the output to os.Stdout
	buf := new(bytes.Buffer)
	err = t.Execute(buf, v)
	if err != nil {
		tlog.Fatal("execute backtest template", "err", err)
	}
	err = os.WriteFile(v.OrgPath, buf.Bytes(), 0644)
	return err
}

const BacktestOrgTemplate = `
* BACKTEST: EMA-Cross {{.Instrument}} {{if .Timeframe}}{{.Timeframe}}{{else}}(timeframe?){{end}}
:PROPERTIES:
:RUN_ID:      {{if .RunID}}{{.RunID}}{{else}}(run-id?){{end}}
:STRATEGY:    ema_cross
:TIMEFRAME:   {{if .Timeframe}}{{.Timeframe}}{{else}}(timeframe?){{end}}
:INSTRUMENT:  {{.Instrument}}
:DATASET:     {{if .Dataset}}{{.Dataset}}{{else}}(dataset?){{end}}
:START_DATE:  {{.Start.Format "2006-01-02"}}
:END_DATE:    {{.End.Format "2006-01-02"}}
:START_BAL:   {{printf "%.2f" .StartBalance}}
:END_BAL:     {{printf "%.2f" .EndBalance}}
:NET_PL:      {{printf "%.2f" .NetPL}}
:RETURN_PCT:  {{printf "%.2f" .ReturnPct}}
:MAX_DD_PCT:  {{if ne .MaxDDPct 0.0}}{{printf "%.2f" .MaxDDPct}}{{else}}(max-dd?){{end}}
:TRADES:      {{.Trades}}
:WINS:        {{.Wins}}
:LOSSES:      {{.Losses}}
:WIN_RATE:    {{printf "%.2f" .WinRate}}
:PROFIT_FAC:  {{if ne .ProfitFactor 0.0}}{{printf "%.2f" .ProfitFactor}}{{else}}(profit-factor?){{end}}
:CREATED:     [{{(orTime .Created).Format "2006-01-02 Mon 15:04"}}]
:END:

** Strategy Parameters
| Parameter        | Value |
|------------------+-------|
| Config		   | {{printf "%s" .Config}} |
| Stop (pips)      | {{printf "%.1f" .StopPips}} |
| R:R              | {{printf "%.2f" .RR}} |
| Risk per Trade % | {{printf "%.2f" (mul100 .RiskPct)}} |

** Performance Summary
- Net P/L:          *{{printf "%.2f" .NetPL}}*
- Return:           *{{printf "%.2f" .ReturnPct}}%*
- Max Drawdown:     *{{if ne .MaxDDPct 0.0}}{{printf "%.2f" .MaxDDPct}}{{else}}(max-dd?){{end}}%*
- Win Rate:         *{{printf "%.2f" .WinRate*100}}%*
- Profit Factor:    *{{if ne .ProfitFactor 0.0}}{{printf "%.2f" .ProfitFactor}}{{else}}(profit-factor?){{end}}*

** Equity Curve
{{- if .EquityPNG }}
[[file:{{.EquityPNG}}]]
{{- else }}
# (optional) insert an exported equity curve image here
{{- end }}

** Trade Distribution
| Outcome | Count |
|---------+-------|
| Wins    | {{.Wins}} |
| Losses  | {{.Losses}} |
| Total   | {{.Trades}} |

{{- if .Notes }}
** Observations
{{- range .Notes }}
- {{.}}
{{- end }}
{{- end }}

{{- if .NextActions }}
** Notes / Next Actions
{{- range .NextActions }}
- [ ] {{.}}
{{- end }}
{{- end }}
`
