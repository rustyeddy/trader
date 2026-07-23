package data

import (
	"fmt"
	"os"

	datasvc "github.com/rustyeddy/trader/service/data"
	"github.com/spf13/cobra"
)

func newCandlesCmd() *cobra.Command {
	var (
		instrument string
		timeframe  string
		from       string
		to         string
		source     string
	)

	cmd := &cobra.Command{
		Use:   "candles",
		Short: "Print local candles in canonical CSV format",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := &datasvc.Service{}
			result, err := svc.CandlesCSV(cmd.Context(), datasvc.CandlesCSVRequest{
				Instrument: instrument,
				Timeframe:  timeframe,
				From:       from,
				To:         to,
				Source:     source,
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(os.Stdout, result.CSV)
			return err
		},
	}

	cmd.Flags().StringVar(&instrument, "instrument", "", "FX pair, e.g. EURUSD")
	cmd.Flags().StringVar(&timeframe, "timeframe", "H1", "Candle timeframe: M1, H1, or D1")
	cmd.Flags().StringVar(&from, "from", "", "Start date inclusive (YYYY-MM-DD)")
	cmd.Flags().StringVar(&to, "to", "", "End date inclusive (YYYY-MM-DD); defaults to now/latest available")
	cmd.Flags().StringVar(&source, "source", "", "Data source override (default: oanda)")
	_ = cmd.MarkFlagRequired("instrument")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}
