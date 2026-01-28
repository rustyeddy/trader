package data

import (
	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/internal/cli/config"
)

func newOandaCmd(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oanda",
		Short: "OANDA dataset tools",
	}

	cmd.AddCommand(
		newOandaTicksCmd(rc),
	)

	return cmd
}
