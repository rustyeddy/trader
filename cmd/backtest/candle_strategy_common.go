package backtest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	bt "github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/data"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/market/strategies"
	"github.com/rustyeddy/trader/types"
	"github.com/spf13/cobra"
)

var btRunName string

type candleCmdCommon struct {
	Instrument string
	Timeframe  string
	From       string
	To         string

	StopPips32 int32
	TakePips32 int32
	Units32    int32
	RiskPct64  float64
}

func newCandleCmdCommon() candleCmdCommon {
	return candleCmdCommon{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2025-01-01",
		To:         "2025-12-31",
		StopPips32: 20,
		TakePips32: 40,
		Units32:    1000,
		RiskPct64:  0.5,
	}
}

func (o *candleCmdCommon) addFlags(cmd *cobra.Command) {
	cmd.Flags().Int32Var(&o.StopPips32, "stop", o.StopPips32, "Stop loss in pips")
	cmd.Flags().Int32Var(&o.TakePips32, "take", o.TakePips32, "Take profit in pips")
	cmd.Flags().Int32Var(&o.Units32, "units", o.Units32, "Position size in units")
	cmd.Flags().Float64Var(&o.RiskPct64, "risk-pct", o.RiskPct64, "Risk per trade as a percent, e.g. 0.5")
	cmd.Flags().StringVar(&o.Instrument, "instrument", o.Instrument, "Instrument, e.g. EURUSD")
	cmd.Flags().StringVar(&o.Timeframe, "timeframe", o.Timeframe, "Timeframe: M1|H1|D1")
	cmd.Flags().StringVar(&o.From, "from", o.From, "Start date inclusive, YYYY-MM-DD")
	cmd.Flags().StringVar(&o.To, "to", o.To, "End date inclusive, YYYY-MM-DD")
}

func (o candleCmdCommon) stopPips() types.Price { return types.Price(o.StopPips32) }
func (o candleCmdCommon) takePips() types.Price { return types.Price(o.TakePips32) }
func (o candleCmdCommon) units() types.Units    { return types.Units(o.Units32) }
func (o candleCmdCommon) riskPct() types.Rate   { return types.RateFromFloat(o.RiskPct64 / 100.0) }

type candleStrategyAdapter struct {
	S strategies.Strategy

	Units     types.Units
	StopPips  types.Price
	TakePips  types.Price
	PipScaled types.Price
}

func (a *candleStrategyAdapter) Name() string { return a.S.Name() }
func (a *candleStrategyAdapter) Reset()       { a.S.Reset() }

func (a *candleStrategyAdapter) OnBar(ctx *bt.CandleContext, c market.Candle) *bt.OrderRequest {
	d := a.S.Update(c)
	if d.Signal() == strategies.Hold {
		return nil
	}

	entry := c.Close
	stopDist := a.StopPips * types.Price(a.PipScaled)
	takeDist := a.TakePips * types.Price(a.PipScaled)

	req := &bt.OrderRequest{
		Units:  a.Units,
		Reason: d.Reason(),
	}

	switch d.Signal() {
	case strategies.Buy:
		req.Side = bt.Long
		if a.StopPips > 0 {
			req.Stop = entry - stopDist
		}
		if a.TakePips > 0 {
			req.Take = entry + takeDist
		}
	case strategies.Sell:
		req.Side = bt.Short
		if a.StopPips > 0 {
			req.Stop = entry + stopDist
		}
		if a.TakePips > 0 {
			req.Take = entry - takeDist
		}
	default:
		return nil
	}

	return req
}

type candleRunMeta struct {
	RunID   string
	RunName string
	Kind    string
	Created types.Timestamp

	Balance  types.Money
	RR       types.Rate
	Strategy string
}

func runCandleStrategy(
	ctx context.Context,
	opts candleCmdCommon,
	strat strategies.Strategy,
	meta candleRunMeta) error {

	start, err := time.Parse("2006-01-02", opts.From)
	if err != nil {
		return fmt.Errorf("bad --from: %w", err)
	}
	endDay, err := time.Parse("2006-01-02", opts.To)
	if err != nil {
		return fmt.Errorf("bad --to: %w", err)
	}
	end := endDay.AddDate(0, 0, 1)

	var tf types.Timeframe
	switch strings.ToUpper(strings.TrimSpace(opts.Timeframe)) {
	case "M1":
		tf = types.M1
	case "H1":
		tf = types.H1
	case "D1":
		tf = types.D1
	default:
		return fmt.Errorf("unsupported timeframe %q", opts.Timeframe)
	}

	instrument := market.NormalizeInstrument(opts.Instrument)
	instMeta, ok := market.Instruments[instrument]
	if !ok {
		return fmt.Errorf("unknown instrument %q", instrument)
	}

	req := bt.CandleRunRequest{
		DataRequest: data.CandleRequest{
			Source:     "candles",
			Instrument: instrument,
			Timeframe:  tf,
			Range: types.TimeRange{
				Start: types.FromTime(start),
				End:   types.FromTime(end),
			},
			Strict: true,
		},
		StartingBalance: meta.Balance,
		AccountCCY:      instMeta.QuoteCurrency,
		Scale:           types.PriceScale,
	}

	dm := data.NewDataManager([]string{instrument}, start, end)
	adapter := &candleStrategyAdapter{
		S:         strat,
		Units:     opts.units(),
		StopPips:  opts.stopPips(),
		TakePips:  opts.takePips(),
		PipScaled: bt.PipScaled(instMeta.PipLocation),
	}

	eng, err := bt.RunCandles(ctx, dm, req, adapter)
	if err != nil {
		return err
	}

	wins, losses := 0, 0
	for _, tr := range eng.Trades {
		if tr.PNL > 0 {
			wins++
		} else if tr.PNL < 0 {
			losses++
		}
	}

	run := bt.BacktestRun{
		RunID:   meta.RunID,
		Name:    meta.RunName,
		Kind:    meta.Kind,
		Created: meta.Created,

		Timeframe:    strings.ToUpper(opts.Timeframe),
		Dataset:      fmt.Sprintf("%s %s %s..%s", instrument, strings.ToUpper(opts.Timeframe), opts.From, opts.To),
		Instrument:   instrument,
		Strategy:     meta.Strategy,
		RiskPct:      opts.riskPct(),
		StopPips:     opts.stopPips(),
		RR:           meta.RR,
		Start:        req.DataRequest.Range.Start,
		End:          req.DataRequest.Range.End,
		Trades:       len(eng.Trades),
		Wins:         wins,
		Losses:       losses,
		StartBalance: meta.Balance,
		EndBalance:   eng.Balance,
		NetPL:        eng.Balance - meta.Balance,
	}

	if meta.Balance != 0 {
		run.ReturnPct = types.RateFromFloat(run.NetPL.Float64() / meta.Balance.Float64())
	}
	if run.Trades > 0 {
		run.WinRate = types.RateFromFloat(float64(run.Wins) / float64(run.Trades))
	}

	bt.PrintBacktestRun(os.Stdout, run)
	return nil
}

func selectConfigRunByKind(cfg *bt.Config, requested, wantKind string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested, nil
	}

	var matches []string
	for _, r := range cfg.Runs {
		if strings.EqualFold(strings.TrimSpace(r.Strategy.Kind), wantKind) {
			matches = append(matches, r.Name)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("config contains no runs with strategy.kind=%q", wantKind)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("config contains %d runs with strategy.kind=%q; use --run to select one", len(matches), wantKind)
	}
}
