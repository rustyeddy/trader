package backtest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
)

var btRunName string

type candleCmdCommon struct {
	Instrument string
	Timeframe  string
	From       string
	To         string

	StopPips  int32
	TakePips  int32
	Units     int64
	RiskPct64 float64
}

func newCandleCmdCommon() candleCmdCommon {
	return candleCmdCommon{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2025-01-01",
		To:         "2025-12-31",
		StopPips:   20,
		TakePips:   40,
		Units:      1000,
		RiskPct64:  0.5,
	}
}

func (o *candleCmdCommon) addFlags(cmd *cobra.Command) {
	cmd.Flags().Int32Var(&o.StopPips, "stop", o.StopPips, "Stop loss in pips")
	cmd.Flags().Int32Var(&o.TakePips, "take", o.TakePips, "Take profit in pips")
	cmd.Flags().Int64Var(&o.Units, "units", o.Units, "Position size in units")
	cmd.Flags().Float64Var(&o.RiskPct64, "risk-pct", o.RiskPct64, "Risk per trade as a percent, e.g. 0.5")
	cmd.Flags().StringVar(&o.Instrument, "instrument", o.Instrument, "Instrument, e.g. EURUSD")
	cmd.Flags().StringVar(&o.Timeframe, "timeframe", o.Timeframe, "Timeframe: M1|H1|D1")
	cmd.Flags().StringVar(&o.From, "from", o.From, "Start date inclusive, YYYY-MM-DD")
	cmd.Flags().StringVar(&o.To, "to", o.To, "End date inclusive, YYYY-MM-DD")
}

func (o candleCmdCommon) stopPips() trader.Price { return trader.Price(o.StopPips) }
func (o candleCmdCommon) takePips() trader.Price { return trader.Price(o.TakePips) }
func (o candleCmdCommon) units() trader.Units    { return trader.Units(o.Units) }
func (o candleCmdCommon) riskPct() trader.Rate   { return trader.RateFromFloat(o.RiskPct64 / 100.0) }

type candleStrategyAdapter struct {
	S trader.Strategy

	Units     trader.Units
	StopPips  trader.Price
	TakePips  trader.Price
	PipScaled trader.Price
}

func (a *candleStrategyAdapter) Name() string { return a.S.Name() }
func (a *candleStrategyAdapter) Reset()       { a.S.Reset() }

func (a *candleStrategyAdapter) OnBar(ctx *trader.CandleContext, c trader.Candle) *trader.OpenRequest {
	d := a.S.Update(c)
	if len(d.Opens) == 0 {
		return nil
	}
	req := *d.Opens[0]
	if req.Price == 0 {
		req.Price = c.Close
	}
	if req.Timestamp == 0 {
		req.Timestamp = ctx.Timestamp
	}
	if req.Units == 0 {
		req.Units = a.Units
	}
	stopDist := a.StopPips * trader.Price(a.PipScaled)
	takeDist := a.TakePips * trader.Price(a.PipScaled)
	if req.Side == trader.Long {
		if req.Stop == 0 && a.StopPips > 0 {
			req.Stop = c.Close - stopDist
		}
		if req.Take == 0 && a.TakePips > 0 {
			req.Take = c.Close + takeDist
		}
	}
	if req.Side == trader.Short {
		if req.Stop == 0 && a.StopPips > 0 {
			req.Stop = c.Close + stopDist
		}
		if req.Take == 0 && a.TakePips > 0 {
			req.Take = c.Close - takeDist
		}
	}
	return &req
}

type candleRunMeta struct {
	RunID   string
	RunName string
	Kind    string
	Created trader.Timestamp

	Balance  trader.Money
	RR       trader.Rate
	Strategy string
}

func runCandleStrategy(
	ctx context.Context,
	opts candleCmdCommon,
	strat trader.Strategy,
	meta candleRunMeta,
	acct *trader.Account,
) error {
	run, err := executeCandleStrategy(ctx, opts, strat, meta, acct)
	if err != nil {
		return err
	}
	trader.PrintBacktestRun(os.Stdout, run)
	return nil
}

func executeCandleStrategy(
	ctx context.Context,
	opts candleCmdCommon,
	strat trader.Strategy,
	meta candleRunMeta,
	acct *trader.Account,
) (trader.BacktestRun, error) {
	start, err := time.Parse("2006-01-02", opts.From)
	if err != nil {
		return trader.BacktestRun{}, fmt.Errorf("bad --from: %w", err)
	}
	endDay, err := time.Parse("2006-01-02", opts.To)
	if err != nil {
		return trader.BacktestRun{}, fmt.Errorf("bad --to: %w", err)
	}
	end := endDay.AddDate(0, 0, 1)

	var tf trader.Timeframe
	switch strings.ToUpper(strings.TrimSpace(opts.Timeframe)) {
	case "M1":
		tf = trader.M1
	case "H1":
		tf = trader.H1
	case "D1":
		tf = trader.D1
	default:
		return trader.BacktestRun{}, fmt.Errorf("unsupported timeframe %q", opts.Timeframe)
	}

	instrument := trader.NormalizeInstrument(opts.Instrument)
	instMeta := trader.GetInstrument(instrument)
	if instMeta == nil {
		return trader.BacktestRun{}, fmt.Errorf("unknown instrument %q", instrument)
	}

	req := trader.CandleRunRequest{
		DataRequest: trader.CandleRequest{
			Source:     "candles",
			Instrument: instrument,
			Timeframe:  tf,
			Range: trader.TimeRange{
				Start: trader.FromTime(start),
				End:   trader.FromTime(end),
			},
			Strict: true,
		},
	}

	dm := trader.NewDataManager([]string{instrument}, start, end)
	adapter := &candleStrategyAdapter{
		S:         strat,
		Units:     opts.units(),
		StopPips:  opts.stopPips(),
		TakePips:  opts.takePips(),
		PipScaled: trader.PipScaled(instMeta.PipLocation),
	}

	eng, err := trader.RunCandles(ctx, dm, req, adapter, acct)
	if err != nil {
		return trader.BacktestRun{}, err
	}

	wins, losses := 0, 0
	for _, tr := range eng.Account.Trades {
		if tr.PNL > 0 {
			wins++
		} else if tr.PNL < 0 {
			losses++
		}
	}

	run := trader.BacktestRun{
		RunID:        meta.RunID,
		Name:         meta.RunName,
		Kind:         meta.Kind,
		Created:      meta.Created,
		Timeframe:    strings.ToUpper(opts.Timeframe),
		Dataset:      fmt.Sprintf("%s %s %s..%s", instrument, strings.ToUpper(opts.Timeframe), opts.From, opts.To),
		Instrument:   instrument,
		Strategy:     meta.Strategy,
		RiskPct:      opts.riskPct(),
		StopPips:     opts.stopPips(),
		RR:           meta.RR,
		Start:        req.DataRequest.Range.Start,
		End:          req.DataRequest.Range.End,
		Trades:       len(eng.Account.Trades),
		Wins:         wins,
		Losses:       losses,
		StartBalance: meta.Balance,
		EndBalance:   eng.Account.Balance,
		NetPL:        eng.Account.Balance - meta.Balance,
	}

	if meta.Balance != 0 {
		run.ReturnPct = trader.RateFromFloat(run.NetPL.Float64() / meta.Balance.Float64())
	}
	if run.Trades > 0 {
		run.WinRate = trader.RateFromFloat(float64(run.Wins) / float64(run.Trades))
	}

	return run, nil
}

func selectConfigRunByKind(cfg *trader.Config, requested, wantKind string) (string, error) {
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
