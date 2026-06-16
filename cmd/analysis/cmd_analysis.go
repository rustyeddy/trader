package analysis

import (
	"fmt"
	"os"
	"text/tabwriter"

	trader "github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/service"
	"github.com/spf13/cobra"
)

func New(_ *trader.RootConfig) *cobra.Command {
	var (
		filePath string
		all      bool
	)

	cmd := &cobra.Command{
		Use:   "analysis",
		Short: "Parse a ChatGPT forex analysis CSV and print trade candidates and watchlist",
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("open %s: %w", filePath, err)
			}
			defer f.Close()

			rows, err := service.ParseAnalysisCSV(f)
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "PAIR\tSTATUS\tTREND\tVOLATILITY\tSUPPORT\tRESISTANCE")
			fmt.Fprintln(w, "----\t------\t-----\t----------\t-------\t----------")

			for _, r := range rows {
				if !all && r.Status == trader.StatusNoTrade {
					continue
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s–%s\t%s–%s\n",
					r.Pair, r.Status, r.Trend, r.Volatility,
					r.SupportLow.String(), r.SupportHigh.String(),
					r.ResistanceLow.String(), r.ResistanceHigh.String(),
				)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "Path to the ChatGPT forex analysis CSV")
	cmd.Flags().BoolVar(&all, "all", false, "Include 'No Trade' rows")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}
