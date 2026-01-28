package data

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/internal/cli/config"
	"github.com/rustyeddy/trader/internal/oanda"
)

func newOandaTicksCmd(rc *config.RootConfig) *cobra.Command {
	var (
		env         string
		token       string
		accountID   string
		instruments string
		outPath     string

		durationStr string
		maxTicks    int
		baseURL     string
	)

	cmd := &cobra.Command{
		Use:   "ticks",
		Short: "Stream OANDA pricing and write ticks CSV (time,instrument,bid,ask)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				token = strings.TrimSpace(os.Getenv("OANDA_TOKEN"))
			}
			if token == "" {
				return fmt.Errorf("missing token: set --token or env OANDA_TOKEN")
			}
			if accountID == "" {
				accountID = strings.TrimSpace(os.Getenv("OANDA_ACCOUNT_ID"))
			}
			if accountID == "" {
				return fmt.Errorf("missing account id: set --account or env OANDA_ACCOUNT_ID")
			}
			if instruments == "" {
				return fmt.Errorf("missing --instruments (e.g. EUR_USD,USD_JPY)")
			}
			if outPath == "" {
				return fmt.Errorf("missing --out")
			}
			if env == "" {
				env = "practice"
			}

			var dur time.Duration
			if durationStr != "" {
				d, err := time.ParseDuration(durationStr)
				if err != nil {
					return fmt.Errorf("bad --duration %q: %w", durationStr, err)
				}
				dur = d
			}

			instList := splitCSV(instruments)
			if len(instList) == 0 {
				return fmt.Errorf("no instruments parsed from %q", instruments)
			}

			ctx := context.Background()
			if dur > 0 {
				var cancel func()
				ctx, cancel = context.WithTimeout(ctx, dur)
				defer cancel()
			}

			// Resolve base URL: flag > env var > env-derived
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

			opts := oanda.PricingStreamOptions{
				AccountID:   accountID,
				Instruments: instList,
			}

			n, err := client.StreamPricingToCSV(ctx, opts, f, maxTicks)
			if err != nil {
				return err
			}

			fmt.Printf("Wrote %d ticks to %s\n", n, outPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	cmd.Flags().StringVar(&token, "token", "", "OANDA API token (or env OANDA_TOKEN)")
	cmd.Flags().StringVar(&accountID, "account", "", "OANDA account id (or env OANDA_ACCOUNT_ID)")
	cmd.Flags().StringVar(&instruments, "instruments", "EUR_USD", "Comma-separated instruments (e.g. EUR_USD,USD_JPY)")
	cmd.Flags().StringVar(&outPath, "out", "", "Output CSV path")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Override OANDA base URL (for testing)")

	cmd.Flags().StringVar(&durationStr, "duration", "", "Stop after duration (e.g. 30s, 5m). If empty, runs until max ticks or Ctrl-C")
	cmd.Flags().IntVar(&maxTicks, "max", 0, "Stop after N ticks (0 = unlimited until duration/Ctrl-C)")

	_ = rc // keep for future shared config/logging; unused for now

	return cmd
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
