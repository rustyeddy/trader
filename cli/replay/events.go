package replay

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/cli/config"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/sim"
)

func newEventsCmd(rc *config.RootConfig) *cobra.Command {
	var (
		path string

		startingBalance float64
		accountID       string
		closeEnd        bool

		fromStr string
		toStr   string
	)

	cmd := &cobra.Command{
		Use:   "events",
		Short: "Replay pricing + scripted events from CSV (time,instrument,bid,ask,event,p1,p2,p3,p4)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
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

			feed, err := NewCSVEventsFeed(path, from, to)
			if err != nil {
				return err
			}
			defer feed.Close()

			for {
				row, ok, err := feed.Next()
				if err != nil {
					return err
				}
				if !ok {
					break
				}

				if err := engine.UpdatePrice(row.Price); err != nil {
					return err
				}

				if strings.TrimSpace(row.Event) != "" {
					if err := applyEvent(ctx, engine, row); err != nil {
						return err
					}
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

	cmd.Flags().StringVar(&path, "ticks", "", "CSV path")
	cmd.Flags().Float64Var(&startingBalance, "starting-balance", 100000, "Starting balance")
	cmd.Flags().StringVar(&accountID, "account", "SIM-REPLAY", "Account ID")
	cmd.Flags().BoolVar(&closeEnd, "close-end", false, "Close open trades at end")

	cmd.Flags().StringVar(&fromStr, "from", "", "Optional RFC3339 start time")
	cmd.Flags().StringVar(&toStr, "to", "", "Optional RFC3339 end time")

	return cmd
}

// applyEvent should implement the same event semantics you already have in cmd/replay.
// Send me your current event names + how p1-p4 are interpreted and I'll lock this down.
func applyEvent(ctx context.Context, eng *sim.Engine, row EventRow) error {
	switch strings.ToUpper(strings.TrimSpace(row.Event)) {
	case "OPEN":
		return fmt.Errorf("OPEN not implemented yet: p1=%q p2=%q p3=%q p4=%q", row.P1, row.P2, row.P3, row.P4)
	case "CLOSE":
		return fmt.Errorf("CLOSE not implemented yet: p1=%q p2=%q p3=%q p4=%q", row.P1, row.P2, row.P3, row.P4)
	default:
		return fmt.Errorf("unknown event %q", row.Event)
	}
}
