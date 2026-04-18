package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	traderpkg "github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/config"
)

func newDownloadTicksCmd(rc *config.RootConfig) *cobra.Command {
	return newSyncLikeCmd(rc, "download-ticks", "Download missing tick files", true, false)
}

func newBuildCandlesCmd(rc *config.RootConfig) *cobra.Command {
	return newSyncLikeCmd(rc, "build-candles", "Build candles from existing data", false, true)
}

func newSyncCmd(rc *config.RootConfig) *cobra.Command {
	return newSyncLikeCmd(rc, "sync", "Download ticks and build candles", true, true)
}

func newSyncLikeCmd(
	rc *config.RootConfig,
	use string,
	short string,
	download bool,
	build bool,
) *cobra.Command {
	var (
		instrumentsCSV string
		fromStr        string
		toStr          string
	)

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = rc

			instruments := splitCSV(instrumentsCSV)
			if len(instruments) == 0 {
				return fmt.Errorf("missing --instruments (example: EURUSD,USDJPY,GBPUSD)")
			}

			start, err := parseMonthStart(fromStr)
			if err != nil {
				return fmt.Errorf("bad --from: %w", err)
			}

			end, err := parseMonthEndExclusive(toStr)
			if err != nil {
				return fmt.Errorf("bad --to: %w", err)
			}

			if !start.Before(end) {
				return fmt.Errorf("--from must be before --to")
			}

			dm := traderpkg.NewDataManager(instruments, start, end)
			dm.Init()

			return dm.Sync(context.Background(), download, build)
		},
	}

	cmd.Flags().StringVar(&instrumentsCSV, "instruments", "", "Comma-separated instruments (e.g. EURUSD,USDJPY)")
	cmd.Flags().StringVar(&fromStr, "from", "", "Start month inclusive, format YYYY-MM")
	cmd.Flags().StringVar(&toStr, "to", "", "End month inclusive, format YYYY-MM")

	_ = cmd.MarkFlagRequired("instruments")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")

	return cmd
}

func parseMonthStart(s string) (time.Time, error) {
	t, err := time.Parse("2006-01", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC), nil
}

func parseMonthEndExclusive(s string) (time.Time, error) {
	t, err := time.Parse("2006-01", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, err
	}
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start.AddDate(0, 1, 0), nil
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
