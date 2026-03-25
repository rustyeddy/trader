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

var CMDBacktestEMACross = &cobra.Command{
	Use:   "ema-cross",
	Short: "Run ema-cross fast and slow",
	RunE:  RunEMACross,
}

var (
	stopPips32 int32   = 20
	takePips32 int32   = 40
	units32    int32   = 1000
	riskPct64  float64 = 0.5

	cfg = strategies.EMACrossConfig{}
)

var (
	btInstrument = "EURUSD"
	btTimeframe  = "H1"
	btFrom       = "2025-01-01"
	btTo         = "2025-12-31"
)

func init() {
	scfg := strategies.StrategyConfig{
		Balance: types.MoneyFromFloat(1000),
		Stop:    20,
		Take:    40,
		RR:      types.RateFromFloat(2.0),
	}
	cfg.StrategyConfig = scfg

	cmd := CMDBacktestEMACross
	cmd.Flags().IntVar(&cfg.FastPeriod, "fast", 12, "Fast EMA period")
	cmd.Flags().IntVar(&cfg.SlowPeriod, "slow", 26, "Slow EMA period")
	cmd.Flags().Int32Var(&stopPips32, "stop", stopPips32, "Stop loss in pips")
	cmd.Flags().Int32Var(&takePips32, "take", takePips32, "Take profit in pips")
	cmd.Flags().Int32Var(&units32, "units", units32, "Position size in units (e.g., 1000 for 1 micro-lot EUR/USD)")
	cmd.Flags().Float64Var(&riskPct64, "risk-pct", riskPct64, "Risk per trade as a percent, e.g. 0.5")
	cmd.Flags().Float64Var(&cfg.MinSpread, "min-spread", 0, "Min |fast-slow| required to signal; 0 disables")
	cmd.Flags().StringVar(&btInstrument, "instrument", btInstrument, "Instrument, e.g. EURUSD")
	cmd.Flags().StringVar(&btTimeframe, "timeframe", btTimeframe, "Timeframe: M1|H1|D1")
	cmd.Flags().StringVar(&btFrom, "from", btFrom, "Start date inclusive, YYYY-MM-DD")
	cmd.Flags().StringVar(&btTo, "to", btTo, "End date inclusive, YYYY-MM-DD")
}

func RunEMACross(cmd *cobra.Command, args []string) error {
	if rootCfg != nil && strings.TrimSpace(rootCfg.ConfigPath) != "" {
		return runConfiguredBatch(cmd, "ema-cross")
	}

	res, err := runEMACrossFromFlags()
	if err != nil {
		return err
	}
	bt.PrintBacktestRun(os.Stdout, res)
	return nil
}

func executeResolvedRun(ctx context.Context, cmd *cobra.Command, rr bt.ResolvedRun) (bt.BacktestRun, error) {
	switch strings.ToLower(strings.TrimSpace(rr.Strategy.Kind)) {
	case "ema-cross":
		return runResolvedEMACross(ctx, cmd, rr)
	default:
		return bt.BacktestRun{}, fmt.Errorf("unsupported strategy.kind %q", rr.Strategy.Kind)
	}
}

func runResolvedEMACross(ctx context.Context, cmd *cobra.Command, rr bt.ResolvedRun) (bt.BacktestRun, error) {
	runCfg := rr
	applyResolvedRunFlagOverrides(cmd, &runCfg)
	runCfg.Instrument = market.NormalizeInstrument(runCfg.Instrument)
	if runCfg.Scale <= 0 {
		runCfg.Scale = types.PriceScale
	}

	emaCfg, err := BuildEMACrossConfig(runCfg)
	if err != nil {
		return bt.BacktestRun{}, err
	}
	applyEMAFlagOverrides(cmd, &emaCfg)

	dataReq, err := runCfg.CandleRequest()
	if err != nil {
		return bt.BacktestRun{}, err
	}
	dataReq.Instrument = runCfg.Instrument

	meta, ok := market.Instruments[runCfg.Instrument]
	if !ok {
		return bt.BacktestRun{}, fmt.Errorf("unknown instrument %q", runCfg.Instrument)
	}

	accountCCY := strings.TrimSpace(runCfg.AccountCCY)
	if accountCCY == "" {
		accountCCY = meta.QuoteCurrency
	}

	req := bt.CandleRunRequest{
		DataRequest:     dataReq,
		StartingBalance: emaCfg.Balance,
		AccountCCY:      accountCCY,
		Scale:           runCfg.Scale,
	}

	dm := data.NewDataManager([]string{runCfg.Instrument}, req.DataRequest.Range.Start.Time(), req.DataRequest.Range.End.Time())
	adapter := &emaCrossAdapter{
		S:         strategies.NewEMACross(emaCfg),
		Units:     runCfg.Units,
		StopPips:  runCfg.StopPips,
		TakePips:  runCfg.TakePips,
		PipScaled: bt.PipScaled(meta.PipLocation),
	}

	return executeEMACross(ctx, req, adapter, runCfg, emaCfg, dm)
}

func runEMACrossFromFlags() (bt.BacktestRun, error) {
	stopPips := types.Price(stopPips32)
	takePips := types.Price(takePips32)
	units := types.Units(units32)

	start, err := time.Parse("2006-01-02", btFrom)
	if err != nil {
		return bt.BacktestRun{}, fmt.Errorf("bad --from: %w", err)
	}
	endDay, err := time.Parse("2006-01-02", btTo)
	if err != nil {
		return bt.BacktestRun{}, fmt.Errorf("bad --to: %w", err)
	}
	end := endDay.AddDate(0, 0, 1)

	var tf types.Timeframe
	switch strings.ToUpper(strings.TrimSpace(btTimeframe)) {
	case "M1":
		tf = types.M1
	case "H1":
		tf = types.H1
	case "D1":
		tf = types.D1
	default:
		return bt.BacktestRun{}, fmt.Errorf("unsupported timeframe %q", btTimeframe)
	}

	instrument := market.NormalizeInstrument(btInstrument)
	meta, ok := market.Instruments[instrument]
	if !ok {
		return bt.BacktestRun{}, fmt.Errorf("unknown instrument %q", instrument)
	}

	cfg.Scale = int32(types.PriceScale)

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
		StartingBalance: cfg.Balance,
		AccountCCY:      meta.QuoteCurrency,
		Scale:           types.PriceScale,
	}

	dm := data.NewDataManager([]string{instrument}, start, end)
	adapter := &emaCrossAdapter{
		S:         strategies.NewEMACross(cfg),
		Units:     units,
		StopPips:  stopPips,
		TakePips:  takePips,
		PipScaled: bt.PipScaled(meta.PipLocation),
	}

	rr := bt.ResolvedRun{
		Name:            "ema-cross-flags",
		Source:          req.DataRequest.Source,
		Instrument:      instrument,
		Timeframe:       strings.ToUpper(strings.TrimSpace(btTimeframe)),
		From:            btFrom,
		To:              btTo,
		Strict:          true,
		StartingBalance: cfg.Balance,
		AccountCCY:      meta.QuoteCurrency,
		Scale:           types.PriceScale,
		RiskPct:         types.RateFromFloat(riskPct64 / 100.0),
		StopPips:        stopPips,
		TakePips:        takePips,
		RR:              cfg.RR,
		Units:           units,
		Strategy: bt.StrategyConfig{
			Kind: "ema-cross",
		},
	}

	return executeEMACross(context.Background(), req, adapter, rr, cfg, dm)
}

func executeEMACross(
	ctx context.Context,
	req bt.CandleRunRequest,
	adapter *emaCrossAdapter,
	rr bt.ResolvedRun,
	emaCfg strategies.EMACrossConfig,
	src bt.CandleSource,
) (bt.BacktestRun, error) {
	eng, err := bt.RunCandles(ctx, src, req, adapter)
	if err != nil {
		return bt.BacktestRun{}, err
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
		Name:         rr.Name,
		Kind:         strings.TrimSpace(rr.Strategy.Kind),
		Created:      types.FromTime(time.Now().UTC()),
		Timeframe:    strings.ToUpper(rr.Timeframe),
		Dataset:      fmt.Sprintf("%s %s %s..%s", rr.Instrument, strings.ToUpper(rr.Timeframe), rr.From, rr.To),
		Instrument:   rr.Instrument,
		Strategy:     adapter.S.Name(),
		RiskPct:      rr.RiskPct,
		StopPips:     adapter.StopPips,
		RR:           emaCfg.RR,
		Start:        req.DataRequest.Range.Start,
		End:          req.DataRequest.Range.End,
		Trades:       len(eng.Trades),
		Wins:         wins,
		Losses:       losses,
		StartBalance: emaCfg.Balance,
		EndBalance:   eng.Balance,
		NetPL:        eng.Balance - emaCfg.Balance,
	}

	if emaCfg.Balance != 0 {
		run.ReturnPct = types.RateFromFloat(run.NetPL.Float64() / emaCfg.Balance.Float64())
	}
	if run.Trades > 0 {
		run.WinRate = types.RateFromFloat(float64(run.Wins) / float64(run.Trades))
	}

	return run, nil
}

func applyResolvedRunFlagOverrides(cmd *cobra.Command, r *bt.ResolvedRun) {
	if cmd.Flags().Changed("instrument") {
		r.Instrument = market.NormalizeInstrument(btInstrument)
	}
	if cmd.Flags().Changed("timeframe") {
		r.Timeframe = strings.ToUpper(strings.TrimSpace(btTimeframe))
	}
	if cmd.Flags().Changed("from") {
		r.From = strings.TrimSpace(btFrom)
	}
	if cmd.Flags().Changed("to") {
		r.To = strings.TrimSpace(btTo)
	}
	if cmd.Flags().Changed("units") {
		r.Units = types.Units(units32)
	}
	if cmd.Flags().Changed("stop") {
		r.StopPips = types.Price(stopPips32)
	}
	if cmd.Flags().Changed("take") {
		r.TakePips = types.Price(takePips32)
	}
	if cmd.Flags().Changed("risk-pct") {
		r.RiskPct = types.RateFromFloat(riskPct64 / 100.0)
	}
}

func applyEMAFlagOverrides(cmd *cobra.Command, emaCfg *strategies.EMACrossConfig) {
	if cmd.Flags().Changed("fast") {
		emaCfg.FastPeriod = cfg.FastPeriod
	}
	if cmd.Flags().Changed("slow") {
		emaCfg.SlowPeriod = cfg.SlowPeriod
	}
	if cmd.Flags().Changed("min-spread") {
		emaCfg.MinSpread = cfg.MinSpread
	}
	if cmd.Flags().Changed("stop") {
		emaCfg.Stop = types.Price(stopPips32)
	}
	if cmd.Flags().Changed("take") {
		emaCfg.Take = types.Price(takePips32)
	}
}

type emaCrossAdapter struct {
	S *strategies.EMACross

	Units     types.Units
	StopPips  types.Price
	TakePips  types.Price
	PipScaled types.Price
}

func (a *emaCrossAdapter) Name() string { return a.S.Name() }
func (a *emaCrossAdapter) Reset()       { a.S.Reset() }
func (a *emaCrossAdapter) Update(c market.Candle) strategies.Decision {
	dec := strategies.DefaultDecision{}
	return dec
}

func (a *emaCrossAdapter) OnBar(ctx *bt.CandleContext, c market.Candle) *bt.OrderRequest {
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
