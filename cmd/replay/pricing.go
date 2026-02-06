package replay

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/cmd/config"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/sim"
)

func newPricingCmd(rc *config.RootConfig) *cobra.Command {
	var (
		ticksPath string

		startingBalance float64
		accountID       string
		closeEnd        bool

		fromStr string
		toStr   string
	)

	cmd := &cobra.Command{
		Use:   "pricing",
		Short: "Replay pricing ticks from CSV (time,instrument,bid,ask[,event,p1,p2,p3,p4])",
		RunE: func(cmd *cobra.Command, args []string) error {
			if ticksPath == "" {
				return fmt.Errorf("-ticks is required")
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
			defer feed.Close()

			for {
				p, ok, err := feed.Next()
				if err != nil {
					return err
				}
				if !ok {
					break
				}
				if err := engine.UpdatePrice(p); err != nil {
					return err
				}
			}

			if closeEnd {
				_ = engine.CloseAll(ctx, "EndOfReplay")
			}

			acct, _ := engine.GetAccount(ctx)
			fmt.Printf("Done. balance=%.2f equity=%.2f\n", acct.Balance, acct.Equity)
			return nil
		},
	}

	cmd.Flags().StringVar(&ticksPath, "ticks", "", "CSV path")
	cmd.Flags().Float64Var(&startingBalance, "starting-balance", 100000, "Starting balance")
	cmd.Flags().StringVar(&accountID, "account", "SIM-REPLAY", "Account ID")
	cmd.Flags().BoolVar(&closeEnd, "close-end", false, "Close open trades at end")

	cmd.Flags().StringVar(&fromStr, "from", "", "Optional RFC3339 start time")
	cmd.Flags().StringVar(&toStr, "to", "", "Optional RFC3339 end time")

	return cmd
}
