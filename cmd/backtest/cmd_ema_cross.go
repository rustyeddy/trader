package backtest

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/cli/config"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/sim"
	"github.com/rustyeddy/trader/strategies"
)

func newEmaCrossCmd(rc *config.RootConfig) *cobra.Command {
	var (
		cfg = strategies.EMACrossConfigDefaults()

		// prices
		ticksPath string
		fromStr   string
		toStr     string
		from      time.Time
		to        time.Time

		// Account
		startingBalance float64
		accountID       string
		closeEnd        bool

		checkInputs func() error
	)

	cmd := &cobra.Command{
		Use:   "ema-cross",
		Short: "EMA(20/50) crossover backtest",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := checkInputs()
			if err != nil {
				return err
			}

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

			strat := strategies.NewEmaCross(cfg)
			runner := &backtest.Runner{
				Engine:   engine,
				Feed:     feed,
				Strategy: strat,
				Options: backtest.RunnerOptions{
					CloseEnd:    closeEnd,
					CloseReason: "EndOfReplay",
				},
			}

			ctx := context.Background()
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

			jbytes, err := cfg.JSON()
			if err != nil {
				fmt.Println("ERROR create JSON from config ", err)
			}

			btr := backtest.BacktestRun{
				RunID:     id.New(),
				Created:   time.Now(),
				Timeframe: "TODO",
				Dataset:   ticksPath,

				Strategy: "EMA-Cross",
				Config:   jbytes,

				Instrument: cfg.Instrument,
				RiskPct:    cfg.RiskPct,
				StopPips:   cfg.StopPips,
				RR:         cfg.RR,

				Start: result.Start,
				End:   result.End,

				Trades: result.Trades,
				Wins:   result.Wins,
				Losses: result.Losses,

				StartBalance: startingBalance,
				EndBalance:   result.Balance,

				// Derived values
				NetPL:        result.Balance - startingBalance,
				ReturnPct:    (result.Balance - startingBalance) / startingBalance,
				WinRate:      float64(result.Wins) / float64(result.Trades),
				ProfitFactor: 9.99,
				MaxDDPct:     9.99,

				GitCommit: "TODO",
				OrgPath:   "backtest.org",
				EquityPNG: "TODO",
			}

			backtest.PrintBacktestRun(os.Stdout, btr)
			err = backtest.RecordBacktest(ctx, btr)
			if err != nil {
				return err
			}

			err = btr.WriteBacktestOrg()
			if err != nil {
				return err
			}
			return err
		},
	}

	// set flags for EMACrossConfig
	cmd.Flags().StringVar(&cfg.Instrument, "instrument", cfg.Instrument, "Instrument")
	cmd.Flags().IntVar(&cfg.FastPeriod, "fast", cfg.FastPeriod, "Fast EMA period")
	cmd.Flags().IntVar(&cfg.SlowPeriod, "slow", cfg.SlowPeriod, "Slow EMA period")

	// Risk
	cmd.Flags().Float64Var(&cfg.RiskPct, "risk", cfg.RiskPct, "Risk per trade (0.005 = 0.5%)")
	cmd.Flags().Float64Var(&cfg.StopPips, "stop-pips", cfg.StopPips, "Stop loss in pips")
	cmd.Flags().Float64Var(&cfg.RR, "rr", cfg.RR, "Risk-reward multiple")

	cmd.Flags().StringVar(&ticksPath, "ticks", "", "Tick CSV (time,instrument,bid,ask)")

	cmd.Flags().Float64Var(&startingBalance, "starting-balance", 100000, "Starting balance")
	cmd.Flags().StringVar(&accountID, "account", "SIM-BACKTEST", "Account ID")
	cmd.Flags().BoolVar(&closeEnd, "close-end", true, "Close open trades at end")

	cmd.Flags().StringVar(&fromStr, "from", "", "Optional RFC3339 start time")
	cmd.Flags().StringVar(&toStr, "to", "", "Optional RFC3339 end time")

	checkInputs = func() error {
		if ticksPath == "" {
			return fmt.Errorf("-ticks is required")
		}
		if cfg.Instrument == "" {
			return fmt.Errorf("-instrument is required")
		}
		if cfg.FastPeriod <= 0 || cfg.SlowPeriod <= 0 || cfg.FastPeriod >= cfg.SlowPeriod {
			return fmt.Errorf("require 0 < fast < slow (got %d/%d)", cfg.FastPeriod, cfg.SlowPeriod)
		}
		if cfg.RiskPct <= 0 || cfg.RiskPct >= 1 {
			return fmt.Errorf("invalid -risk (got %v)", cfg.RiskPct)
		}
		if cfg.StopPips <= 0 {
			return fmt.Errorf("invalid -stop-pips")
		}
		if cfg.RR <= 0 {
			return fmt.Errorf("invalid -rr")
		}
		if startingBalance <= 0 {
			return fmt.Errorf("invalid -starting-balance")
		}
		if accountID == "" {
			return fmt.Errorf("invalid -account")
		}

		var err error
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
		return nil
	}

	return cmd
}
