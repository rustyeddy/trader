package data

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/config"
	accountsvc "github.com/rustyeddy/trader/service/account"
	datasvc "github.com/rustyeddy/trader/service/data"
)

func newOandaDownloadCmd(rc *config.RootConfig) *cobra.Command {
	var (
		instrument string
		timeframe  string
		fromStr    string
		toStr      string
		token      string
		env        string
		rawDir     string
	)

	cmd := &cobra.Command{
		Use:   "oanda",
		Short: "Download candles from OANDA into the canonical candle store",
		RunE: func(cmd *cobra.Command, args []string) error {
			from, err := time.Parse("2006-01-02", fromStr)
			if err != nil {
				return fmt.Errorf("bad --from %q: %w", fromStr, err)
			}
			to, err := time.Parse("2006-01-02", toStr)
			if err != nil {
				return fmt.Errorf("bad --to %q: %w", toStr, err)
			}

			ctx := context.Background()
			client, err := oanda.NewClient(env, token)
			if err != nil {
				var amb accountsvc.AmbiguousAccountError
				if errors.As(err, &amb) {
					fmt.Println("Multiple accounts found — specify one with --account-id:")
					for _, id := range amb.Accounts {
						fmt.Printf("  %s\n", id)
					}
				}
				return err
			}

			result, err := (&datasvc.Service{OANDA: client}).DownloadOandaCandles(ctx, datasvc.DownloadOandaCandlesRequest{
				Instrument: instrument,
				Timeframe:  timeframe,
				From:       from,
				To:         to,
				RawDir:     rawDir,
				OnProgress: func(line string) { fmt.Println(line) },
			})
			if err != nil {
				return err
			}
			fmt.Printf("✓ %d months processed, %d candles written.\n",
				result.MonthsProcessed, result.CandlesWritten)
			return nil
		},
	}

	cmd.Flags().StringVar(&instrument, "instrument", "", "OANDA-format instrument, e.g. USD_JPY (required)")
	cmd.Flags().StringVar(&timeframe, "timeframe", "", "Timeframe: M1, H1, D (required)")
	cmd.Flags().StringVar(&fromStr, "from", "", "Start date YYYY-MM-DD inclusive (required)")
	cmd.Flags().StringVar(&toStr, "to", "", "End date YYYY-MM-DD inclusive (required)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (falls back to ~/.config/oanda/pat.txt)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	cmd.Flags().StringVar(&rawDir, "raw-dir", "/srv/trading/data/raw", "Root directory for raw bid+ask candle preservation")
	_ = cmd.MarkFlagRequired("instrument")
	_ = cmd.MarkFlagRequired("timeframe")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")

	return cmd
}
