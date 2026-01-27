package cmd

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/sim"
	"github.com/spf13/cobra"
)

var backtestCmd = &cobra.Command{
	Use:   "backtest",
	Short: "Run backtests with various trading strategies",
	Long: `Backtest allows you to test trading strategies against historical tick data.

Supported strategies:
  - noop: Does nothing (baseline test)
  - open-once: Opens a single position at first tick
  - ema-cross: EMA crossover strategy with configurable parameters

Example:
  trader backtest -ticks data/eurusd.csv -strategy ema-cross -fast 20 -slow 50`,
	RunE: runBacktest,
}

var (
	btTicksPath   string
	btDBPath      string
	btBalance     float64
	btAccountID   string
	btCloseEnd    bool
	btStrategy    string
	btInstrument  string
	btUnits       float64
	btFast        int
	btSlow        int
	btRiskPct     float64
	btStopPips    float64
	btRR          float64
)

func init() {
	rootCmd.AddCommand(backtestCmd)

	backtestCmd.Flags().StringVarP(&btTicksPath, "ticks", "t", "", "path to tick CSV (time,instrument,bid,ask[,event...]) (required)")
	backtestCmd.Flags().StringVarP(&btDBPath, "db", "d", "./backtest.sqlite", "path to SQLite journal DB")
	backtestCmd.Flags().Float64VarP(&btBalance, "balance", "b", 100_000, "starting account balance/equity")
	backtestCmd.Flags().StringVar(&btAccountID, "account", "SIM-BACKTEST", "account ID for journaling")
	backtestCmd.Flags().BoolVar(&btCloseEnd, "close-end", true, "close all open trades at end of replay")

	backtestCmd.Flags().StringVarP(&btStrategy, "strategy", "s", "noop", "strategy name (noop, open-once, ema-cross)")
	backtestCmd.Flags().StringVarP(&btInstrument, "instrument", "i", "EUR_USD", "strategy instrument")
	backtestCmd.Flags().Float64VarP(&btUnits, "units", "u", 10_000, "order units (used by some strategies)")

	backtestCmd.Flags().IntVar(&btFast, "fast", 20, "ema-cross: fast EMA period")
	backtestCmd.Flags().IntVar(&btSlow, "slow", 50, "ema-cross: slow EMA period")
	backtestCmd.Flags().Float64Var(&btRiskPct, "risk", 0.005, "ema-cross: risk percent per trade (0.005 = 0.5%)")
	backtestCmd.Flags().Float64Var(&btStopPips, "stop-pips", 20, "ema-cross: stop loss in pips")
	backtestCmd.Flags().Float64Var(&btRR, "rr", 2.0, "ema-cross: take profit as R multiple (e.g. 2.0)")

	backtestCmd.MarkFlagRequired("ticks")
}

func runBacktest(cmd *cobra.Command, args []string) error {
	j, err := journal.NewSQLite(btDBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       btAccountID,
		Currency: "USD",
		Balance:  btBalance,
		Equity:   btBalance,
	}, j)

	strat, err := strategyByName(btStrategy, btInstrument, btUnits, btFast, btSlow, btRiskPct, btStopPips, btRR)
	if err != nil {
		return fmt.Errorf("strategy: %w", err)
	}

	ctx := context.Background()
	fmt.Printf("Running backtest with strategy: %s\n", btStrategy)
	fmt.Printf("  Ticks: %s\n", btTicksPath)
	fmt.Printf("  Journal: %s\n\n", btDBPath)

	if err := replayCSVWithStrategy(ctx, btTicksPath, engine, strat); err != nil {
		return fmt.Errorf("replay: %w", err)
	}

	if btCloseEnd {
		_ = engine.CloseAll(ctx, "EndOfReplay")
	}

	acct, _ := engine.GetAccount(ctx)
	fmt.Printf("\nBacktest Complete!\n")
	fmt.Printf("  Balance: $%.2f\n", acct.Balance)
	fmt.Printf("  Equity: $%.2f\n", acct.Equity)
	fmt.Printf("  Margin Used: $%.2f\n", acct.MarginUsed)
	fmt.Printf("  Free Margin: $%.2f\n", acct.FreeMargin)

	return nil
}

// TickStrategy is the minimal interface a backtest strategy must implement.
type TickStrategy interface {
	OnTick(ctx context.Context, b broker.Broker, tick broker.Price) error
}

func strategyByName(name string, instrument string, units float64, fast, slow int, riskPct, stopPips, rr float64) (TickStrategy, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "noop", "none":
		return NoopStrategy{}, nil

	case "open-once":
		return &OpenOnceStrategy{
			Instrument: instrument,
			Units:      units,
		}, nil

	case "ema-cross", "emacross":
		return NewEmaCrossStrategy(instrument, fast, slow, riskPct, stopPips, rr), nil

	default:
		return nil, fmt.Errorf("unknown strategy %q (supported: noop, open-once, ema-cross)", name)
	}
}

// NoopStrategy does nothing.
type NoopStrategy struct{}

func (NoopStrategy) OnTick(ctx context.Context, b broker.Broker, tick broker.Price) error {
	return nil
}

// OpenOnceStrategy opens a single market order the first time it sees a tick.
type OpenOnceStrategy struct {
	Instrument string
	Units      float64
	opened     bool
}

func (s *OpenOnceStrategy) OnTick(ctx context.Context, b broker.Broker, tick broker.Price) error {
	if s.opened {
		return nil
	}
	if tick.Instrument != s.Instrument {
		return nil
	}
	if s.Units == 0 {
		return fmt.Errorf("open-once: units must be non-zero")
	}

	_, err := b.CreateMarketOrder(ctx, broker.MarketOrderRequest{
		Instrument: s.Instrument,
		Units:      s.Units,
	})
	if err != nil {
		return err
	}
	s.opened = true
	return nil
}

func replayCSVWithStrategy(ctx context.Context, csvPath string, engine *sim.Engine, strat TickStrategy) error {
	f, err := os.Open(csvPath)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	firstRow, err := r.Read()
	if err != nil {
		return err
	}

	hasHeader := len(firstRow) > 0 && strings.EqualFold(strings.TrimSpace(firstRow[0]), "time")
	if !hasHeader {
		if err := handleRow(ctx, engine, strat, firstRow); err != nil {
			return err
		}
	}

	for {
		row, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if len(row) == 0 {
			continue
		}
		if err := handleRow(ctx, engine, strat, row); err != nil {
			return err
		}
	}
}

func handleRow(ctx context.Context, engine *sim.Engine, strat TickStrategy, row []string) error {
	if len(row) < 4 {
		return fmt.Errorf("bad row (need at least 4 cols time,instrument,bid,ask): %v", row)
	}

	ts := strings.TrimSpace(row[0])
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t2, err2 := time.Parse(time.RFC3339Nano, ts)
		if err2 != nil {
			return fmt.Errorf("bad time %q: %w", row[0], err)
		}
		t = t2
	}

	inst := strings.TrimSpace(row[1])

	bid, err := strconv.ParseFloat(strings.TrimSpace(row[2]), 64)
	if err != nil {
		return fmt.Errorf("bad bid %q: %w", row[2], err)
	}
	ask, err := strconv.ParseFloat(strings.TrimSpace(row[3]), 64)
	if err != nil {
		return fmt.Errorf("bad ask %q: %w", row[3], err)
	}

	tick := broker.Price{
		Time:       t,
		Instrument: inst,
		Bid:        bid,
		Ask:        ask,
	}

	if err := engine.UpdatePrice(tick); err != nil {
		return err
	}

	return strat.OnTick(ctx, engine, tick)
}
