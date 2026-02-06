package data

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/broker/oanda"
	"github.com/rustyeddy/trader/cli/config"
)

func newOandaCandlesCmd(rc *config.RootConfig) *cobra.Command {
	var (
		env        string
		token      string
		instrument string
		gran       string
		price      string // M|B|A|BA

		fromStr string
		toStr   string
		count   int

		outPath string
		baseURL string
	)

	cmd := &cobra.Command{
		Use:   "candles",
		Short: "Download OANDA instrument candles and write CSV",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = rc

			if token == "" {
				token = strings.TrimSpace(os.Getenv("OANDA_TOKEN"))
			}
			if token == "" {
				return fmt.Errorf("missing token: set --token or env OANDA_TOKEN")
			}
			if env == "" {
				env = "practice"
			}
			if instrument == "" {
				return fmt.Errorf("missing --instrument (e.g. EUR_USD)")
			}
			if gran == "" {
				return fmt.Errorf("missing --granularity (e.g. M1, H1, D)")
			}
			if price == "" {
				price = "M"
			}
			if outPath == "" {
				return fmt.Errorf("missing --out")
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
						return fmt.Errorf("bad --from: %w", err)
					}
				}
			}
			if toStr != "" {
				to, err = time.Parse(time.RFC3339, toStr)
				if err != nil {
					to, err = time.Parse(time.RFC3339Nano, toStr)
					if err != nil {
						return fmt.Errorf("bad --to: %w", err)
					}
				}
			}
			if !from.IsZero() && !to.IsZero() && !from.Before(to) {
				return fmt.Errorf("--from must be before --to")
			}

			// Must specify either (from+to) or count
			if count <= 0 && (from.IsZero() || to.IsZero()) {
				return fmt.Errorf("provide either --count or both --from and --to")
			}

			resolvedBaseURL := baseURL
			if resolvedBaseURL == "" {
				resolvedBaseURL = strings.TrimSpace(os.Getenv("OANDA_BASE_URL"))
			}
			if resolvedBaseURL == "" {
				var err error
				resolvedBaseURL, err = oanda.BaseURL(env)
				if err != nil {
					return err
				}
			}

			f, err := os.Create(outPath)
			if err != nil {
				return err
			}
			defer f.Close()

			client := &oanda.Client{
				BaseURL: resolvedBaseURL,
				Token:   token,
			}

			ctx := context.Background()

			n, err := client.DownloadCandlesToCSV(ctx, oanda.CandlesOptions{
				Instrument:  instrument,
				Granularity: gran,
				Price:       price,
				From:        from,
				To:          to,
				Count:       count,
			}, f)
			if err != nil {
				return err
			}

			fmt.Printf("Wrote %d candles to %s\n", n, outPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	cmd.Flags().StringVar(&token, "token", "", "OANDA API token (or env OANDA_TOKEN)")

	cmd.Flags().StringVar(&instrument, "instrument", "EUR_USD", "Instrument (e.g. EUR_USD)")
	cmd.Flags().StringVar(&gran, "granularity", "H1", "Granularity (e.g. S5, M1, H1, D)")
	cmd.Flags().StringVar(&price, "price", "M", "Price component: M (mid), B (bid), A (ask), BA (bid+ask)")

	cmd.Flags().StringVar(&fromStr, "from", "", "RFC3339 start time (inclusive)")
	cmd.Flags().StringVar(&toStr, "to", "", "RFC3339 end time (exclusive)")
	cmd.Flags().IntVar(&count, "count", 0, "Number of candles (alternative to from/to)")

	cmd.Flags().StringVar(&baseURL, "base-url", "", "Override OANDA base URL (for testing)")
	cmd.Flags().StringVar(&outPath, "out", "", "Output CSV path")

	return cmd
}
