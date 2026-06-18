package data

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	traderpkg "github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/service"
)

// defaultInstruments is the full set tracked in the candle store.
var defaultInstruments = []string{
	"AUD_CAD", "AUD_CHF", "AUD_JPY", "AUD_NZD", "AUD_USD",
	"CAD_JPY", "CHF_JPY",
	"EUR_AUD", "EUR_CAD", "EUR_CHF", "EUR_GBP", "EUR_JPY", "EUR_NZD", "EUR_USD",
	"GBP_AUD", "GBP_CAD", "GBP_JPY", "GBP_NZD", "GBP_USD",
	"NZD_JPY", "NZD_USD",
	"USD_CAD", "USD_CHF", "USD_JPY",
}

var defaultTimeframes = []string{"M1", "H1", "D"}

func newUpdateCmd(rc *traderpkg.RootConfig) *cobra.Command {
	var (
		instrumentsCSV string
		timeframesCSV  string
		token          string
		env            string
		rawDir         string
		dryRun         bool
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Catch-up download: fill OANDA candle data from last stored date through yesterday",
		Long: `Download OANDA candles for every instrument+timeframe, starting from the day
after the last non-zero candle already on disk and ending at yesterday (last
complete UTC day).

By default all 24 tracked instruments and three timeframes (M1, H1, D) are
updated. Use --instruments and --timeframes to restrict the scope.

Examples:
  trader data update
  trader data update --instruments EUR_USD,GBP_USD
  trader data update --timeframes H1,D
  trader data update --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = rc

			instruments := splitCSV(instrumentsCSV)
			if len(instruments) == 0 {
				instruments = defaultInstruments
			}

			timeframes := splitCSV(timeframesCSV)
			if len(timeframes) == 0 {
				timeframes = defaultTimeframes
			}

			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Dry run — would update %d instruments × %d timeframes:\n",
					len(instruments), len(timeframes))
				for _, inst := range instruments {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", inst)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Timeframes: %s\n", strings.Join(timeframes, ", "))
				return nil
			}

			svc, err := service.New(service.Config{Env: env, Token: token})
			if err != nil {
				var amb service.AmbiguousAccountError
				if errors.As(err, &amb) {
					fmt.Fprintln(cmd.OutOrStdout(), "Multiple accounts — specify one with --account-id:")
					for _, id := range amb.Accounts {
						fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", id)
					}
				}
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Updating %d instruments × %d timeframes through yesterday...\n\n",
				len(instruments), len(timeframes))

			result, err := svc.UpdateOandaCandles(context.Background(), service.UpdateOandaCandlesRequest{
				Instruments: instruments,
				Timeframes:  timeframes,
				RawDir:      rawDir,
				OnProgress: func(msg string) {
					fmt.Fprintln(out, msg)
				},
			})
			if err != nil {
				return err
			}

			// Summary table.
			var totalCandles, errCount int
			for _, r := range result.Results {
				totalCandles += r.CandlesWritten
				if r.Err != nil {
					errCount++
				}
			}
			fmt.Fprintf(out, "\nDone. %d candles written across %d pairs. %d errors.\n",
				totalCandles, len(result.Results), errCount)
			return nil
		},
	}

	cmd.Flags().StringVar(&instrumentsCSV, "instruments", "", "Comma-separated instruments to update (default: all 24)")
	cmd.Flags().StringVar(&timeframesCSV, "timeframes", "", "Comma-separated timeframes (default: M1,H1,D)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	cmd.Flags().StringVar(&rawDir, "raw-dir", "/srv/trading/data/raw", "Root for raw bid+ask preservation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be downloaded without fetching")
	return cmd
}
