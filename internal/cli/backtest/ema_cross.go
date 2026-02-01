package backtest

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/internal/backtest"
	"github.com/rustyeddy/trader/internal/cli/config"
	"github.com/rustyeddy/trader/internal/strategies"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/sim"
)

func newEmaCrossCmd(rc *config.RootConfig) *cobra.Command {
	var (
		cfg = strategies.EMACrossConfigDefaults()

		// prices
		ticksPath string
		fromStr   string
		toStr     string

		// Account
		startingBalance float64
		accountID       string
		closeEnd        bool
	)

	cmd := &cobra.Command{
		Use:   "ema-cross",
		Short: "EMA(20/50) crossover backtest",
		RunE: func(cmd *cobra.Command, args []string) error {
			if ticksPath == "" {
				return fmt.Errorf("-ticks is required")
			}
			if instrument == "" {
				return fmt.Errorf("-instrument is required")
			}
			if fast <= 0 || slow <= 0 || fast >= slow {
				return fmt.Errorf("require 0 < fast < slow (got %d/%d)", fast, slow)
			}
			if riskPct <= 0 || riskPct >= 1 {
				return fmt.Errorf("invalid -risk (got %v)", riskPct)
			}
			if stopPips <= 0 {
				return fmt.Errorf("invalid -stop-pips")
			}
			if rr <= 0 {
				return fmt.Errorf("invalid -rr")
			}
			if startingBalance <= 0 {
				return fmt.Errorf("invalid -starting-balance")
			}
			if accountID == "" {
				return fmt.Errorf("invalid -account")
			}

			var (
				from time.Time
				to   time.Time
				err  error
			)

			if fromStr != "" {
				from, err = time.Parse(time.RFC3339, fromStr)
				if err != nil {
					from, err = time.Parse(time.RFC3339Nano, fromStr)
					if err != nil {
						return fmt.Errorf("bad -from: %w", err)
					}
				}
			}

			if toStr != "" {
				to, err = time.Parse(time.RFC3339, toStr)
				if err != nil {
					to, err = time.Parse(time.RFC3339Nano, toStr)
					if err != nil {
						return fmt.Errorf("bad -to: %w", err)
					}
				}
			}
			if !from.IsZero() && !to.IsZero() && !from.Before(to) {
				return fmt.Errorf("-from must be before -to")
			}

			// ---------------------------
			// WIRING (this is where it goes)
			// ---------------------------
			ctx := context.Background()

			j, err := journal.NewSQLite(rc.DBPath)
			if err != nil {
				return err
			}
			defer j.Close()

			engine := sim.NewEngine(broker.Account{
				ID:       accountID,
				Currency: "USD",
				Balance:  startingBalance,
				Equity:   startingBalance,
			}, j)

			feed, err := backtest.NewCSVTicksFeed(ticksPath, from, to)
			if err != nil {
				return err
			}

			strat := strategies.NewEmaCross(instrument, fast, slow, riskPct, stopPips, rr)

			runner := &backtest.Runner{
				Engine:   engine,
				Feed:     feed,
				Strategy: strat,
				Options: backtest.RunnerOptions{
					CloseEnd:    closeEnd,
					CloseReason: "EndOfReplay",
				},
			}

			result, err := runner.Run(ctx, j)
			if err != nil {
				return err
			}

			fmt.Printf(
				"Done. balance=%.2f equity=%.2f trades=%d wins=%d losses=%d\n",
				result.Balance,
				result.Equity,
				result.Trades,
				result.Wins,
				result.Losses,
			)

			return nil
		},
	}

	// set flags for EMACrossConfig
	cmd.Flags().StringVar(&cfg.Instrument, "instrument", cfg.Instrument, "Instrument")
	cmd.Flags().IntVar(&cfg.FastPeriod, "fast", cfg.FastPeriod, "Fast EMA period")
	cmd.Flags().IntVar(&cfg.SlowPeriod, "slow", cfg.SlowPeriod, "Slow EMA period")

	// Risk
	cmd.Flags().Float64Var(&cfg.RiskPct, "risk", cfg.RickPct, "Risk per trade (0.005 = 0.5%)")
	cmd.Flags().Float64Var(&cfg.StopPips, "stop-pips", cfg.StopPips, "Stop loss in pips")
	cmd.Flags().Float64Var(&cfg.RR, "rr", cfg.RR, "Risk-reward multiple")

	cmd.Flags().StringVar(&ticksPath, "ticks", "", "Tick CSV (time,instrument,bid,ask)")

	cmd.Flags().Float64Var(&startingBalance, "starting-balance", 100000, "Starting balance")
	cmd.Flags().StringVar(&accountID, "account", "SIM-BACKTEST", "Account ID")
	cmd.Flags().BoolVar(&closeEnd, "close-end", true, "Close open trades at end")

	cmd.Flags().StringVar(&fromStr, "from", "", "Optional RFC3339 start time")
	cmd.Flags().StringVar(&toStr, "to", "", "Optional RFC3339 end time")

	return cmd
}
