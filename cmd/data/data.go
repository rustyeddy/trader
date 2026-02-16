package data

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/cmd/config"
	"github.com/rustyeddy/trader/pricing"
)

func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data",
		Short: "Download/prepare datasets (OANDA → CSV, transforms, etc.)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fname := "../testdata/DAT_ASCII_EURUSD_M1_2025.csv"
			cs, err := pricing.NewCandleSet(fname)
			if err != nil {
				fmt.Fprintf(os.Stderr, "test data %s\n", err)
				return err
			}
			cs.Stats()
			return nil
		},
	}

	cmd.AddCommand(
		newOandaCmd(rc),
		newOandaTicksCmd(rc),
		newOandaCandlesCmd(rc),
	)

	return cmd
}
