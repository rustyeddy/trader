package data

import (
	"github.com/rustyeddy/trader/cmd/config"
	"github.com/spf13/cobra"
)

func newOandaCmd(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oanda",
		Short: "OANDA dataset tools",
	}

	cmd.AddCommand(
		newOandaTicksCmd(rc),
		newOandaCandlesCmd(rc),
	)

	return cmd
}
