package journal

import (
	"bytes"
	"log"
	"os"
	"text/template"
	"time"
)

// BacktestRunRow mirrors backtest_runs table.
type BacktestRun struct {
	RunID     string
	Created   time.Time
	Timeframe string
	Dataset   string
	// DatasetID     string
	// DatasetPath   string
	// DatasetSHA256 string

	// Instrument traded in this backtest
	Instrument string
	Strategy   string
	Config     []byte // strategy config

	// Risk Management
	RiskPct  float64 // 0.005 (0.5%)
	StopPips float64 // e.g. 20
	RR       float64 // take-profit multiple of risk, e.g. 2.0

	// Account and price timeframe
	Start time.Time
	End   time.Time

	// Results
	Trades int
	Wins   int
	Losses int

	// account info
	StartBalance float64
	EndBalance   float64

	// Derived / computed in Go
	NetPL        float64
	ReturnPct    float64
	WinRate      float64
	ProfitFactor float64
	MaxDDPct     float64

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
		log.Fatal(err)
	}

	// 2. Execute the template, writing the output to os.Stdout
	buf := new(bytes.Buffer)
	err = t.Execute(buf, v)
	if err != nil {
		log.Fatal(err)
	}
	err = os.WriteFile(v.OrgPath, buf.Bytes(), 0644)
	return err
}

const BacktestOrgTemplate = `
* BACKTEST: EMA-Cross {{.Config.Instrument}} {{if .Timeframe}}{{.Timeframe}}{{else}}(timeframe?){{end}}
:PROPERTIES:
:RUN_ID:      {{if .RunID}}{{.RunID}}{{else}}(run-id?){{end}}
:STRATEGY:    ema_cross
:TIMEFRAME:   {{if .Timeframe}}{{.Timeframe}}{{else}}(timeframe?){{end}}
:INSTRUMENT:  {{.Config.Instrument}}
:DATASET:     {{if .Dataset}}{{.Dataset}}{{else}}(dataset?){{end}}
:START_DATE:  {{.Result.Start.Format "2006-01-02"}}
:END_DATE:    {{.Result.End.Format "2006-01-02"}}
:START_BAL:   {{printf "%.2f" .StartBal}}
:END_BAL:     {{printf "%.2f" .EndBal}}
:NET_PL:      {{printf "%.2f" .NetPL}}
:RETURN_PCT:  {{printf "%.2f" .ReturnPct}}
:MAX_DD_PCT:  {{if ne .MaxDDPct 0.0}}{{printf "%.2f" .MaxDDPct}}{{else}}(max-dd?){{end}}
:TRADES:      {{.Result.Trades}}
:WINS:        {{.Result.Wins}}
:LOSSES:      {{.Result.Losses}}
:WIN_RATE:    {{printf "%.2f" .WinRatePct}}
:PROFIT_FAC:  {{if ne .ProfitFactor 0.0}}{{printf "%.2f" .ProfitFactor}}{{else}}(profit-factor?){{end}}
:CREATED:     [{{(orTime .Created).Format "2006-01-02 Mon 15:04"}}]
:END:

** Strategy Parameters
| Parameter        | Value |
|------------------+-------|
| Fast EMA         | {{.Config.FastPeriod}} |
| Slow EMA         | {{.Config.SlowPeriod}} |
| Stop (pips)      | {{printf "%.1f" .Config.StopPips}} |
| R:R              | {{printf "%.2f" .Config.RR}} |
| Risk per Trade % | {{printf "%.2f" (mul100 .Config.RiskPct)}} |

** Performance Summary
- Net P/L:          *{{printf "%.2f" .NetPL}}*
- Return:           *{{printf "%.2f" .ReturnPct}}%*
- Max Drawdown:     *{{if ne .MaxDDPct 0.0}}{{printf "%.2f" .MaxDDPct}}{{else}}(max-dd?){{end}}%*
- Win Rate:         *{{printf "%.2f" .WinRatePct}}%*
- Profit Factor:    *{{if ne .ProfitFactor 0.0}}{{printf "%.2f" .ProfitFactor}}{{else}}(profit-factor?){{end}}*

** Equity Curve
{{- if .EquityCurveImage }}
[[file:{{.EquityCurveImage}}]]
{{- else }}
# (optional) insert an exported equity curve image here
{{- end }}

** Trade Distribution
| Outcome | Count |
|---------+-------|
| Wins    | {{.Result.Wins}} |
| Losses  | {{.Result.Losses}} |
| Total   | {{.Result.Trades}} |

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
