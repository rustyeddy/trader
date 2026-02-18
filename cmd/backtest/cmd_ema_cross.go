package backtest

import (
	"fmt"
	"os"

	bt "github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/market/strategies"
	"github.com/spf13/cobra"
)

var CMDBacktestEMACross = &cobra.Command{
	Use:   "ema-cross",
	Short: "Run ema-cross fast and slow",
	RunE:  RunEMACross,
}

var (
	stopPips int32 = 20
	takePips int32 = 40
	units    int32 = 1000

	cfg = strategies.EMACrossConfig{}
)

func init() {
	scfg := strategies.StrategyConfig{
		Balance: 1000,
		Stop:    20,
		Take:    40,
		RR:      0.02,
		File:    "testdata/DAT_ASCII_EURUSD_M1_2025.csv",
	}
	cfg.StrategyConfig = scfg

	cmd := CMDBacktestEMACross
	cmd.Flags().StringVar(&cfg.File, "file", "", "Path to Dukascopy-style M1 candles file (semicolon-separated)")
	cmd.Flags().IntVar(&cfg.FastPeriod, "fast", 12, "Fast EMA period")
	cmd.Flags().IntVar(&cfg.SlowPeriod, "slow", 26, "Slow EMA period")
	cmd.Flags().Int32Var(&stopPips, "stop", stopPips, "Stop loss in pips")
	cmd.Flags().Int32Var(&takePips, "take", takePips, "Take profit in pips")
	cmd.Flags().Int32Var(&units, "units", units, "Position size in units (e.g., 1000 for 1 micro-lot EUR/USD)")
	cmd.Flags().Float64Var(&cfg.MinSpread, "min-spread", 0, "Min |fast-slow| required to signal; 0 disables")
}

func RunEMACross(cmd *cobra.Command, args []string) error {
	file := cfg.File
	if file == "" {
		file = "../testdata/DAT_ASCII_EURUSD_M1_2025.csv"
	}

	cs, err := market.NewCandleSet(file)
	if err != nil {
		return err
	}

	// CandleEngine expects H1.
	h1 := cs
	if cs.Timeframe == 60 {
		h1 = cs.AggregateH1(50)
	}

	// Strategy needs scale for int32->float math in indicators.
	cfg.Scale = h1.Scale

	meta, ok := market.Instruments[h1.Name]
	if !ok {
		return fmt.Errorf("unknown instrument %q", h1.Name)
	}
	pipScaled, err := bt.PipScaled(h1.Scale, meta.PipLocation)
	if err != nil {
		return err
	}

	strat := strategies.NewEMACross(cfg)
	eng := bt.NewCandleEngine(h1, cfg.Balance, meta.QuoteCurrency)

	adapter := &emaCrossAdapter{
		S:         strat,
		Units:     units,
		StopPips:  stopPips,
		TakePips:  takePips,
		PipScaled: pipScaled,
	}

	if err := eng.Run(adapter); err != nil {
		return err
	}

	// Minimal summary (no DB write yet)
	wins, losses := 0, 0
	for _, tr := range eng.Trades {
		if tr.PNL > 0 {
			wins++
		} else {
			losses++
		}
	}

	start := h1.Time(0)
	end := h1.Time(len(h1.Candles) - 1)

	run := bt.BacktestRun{
		Timeframe:    "H1",
		Dataset:      file,
		Instrument:   h1.Name,
		Strategy:     strat.Name(),
		StopPips:     float64(stopPips),
		RR:           cfg.RR,
		Start:        start,
		End:          end,
		Trades:       len(eng.Trades),
		Wins:         wins,
		Losses:       losses,
		StartBalance: cfg.Balance,
		EndBalance:   eng.Balance,
		NetPL:        eng.Balance - cfg.Balance,
	}
	if cfg.Balance != 0 {
		run.ReturnPct = (run.NetPL / cfg.Balance) * 100
	}
	if run.Trades > 0 {
		run.WinRate = (float64(run.Wins) / float64(run.Trades)) * 100
	}

	bt.PrintBacktestRun(os.Stdout, run)
	return nil
}

type emaCrossAdapter struct {
	S *strategies.EMACross

	Units     int32
	StopPips  int32
	TakePips  int32
	PipScaled int32
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

	entry := c.C
	stopDist := a.StopPips * a.PipScaled
	takeDist := a.TakePips * a.PipScaled

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
