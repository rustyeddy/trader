package replay

import (
	"github.com/rustyeddy/trader/cmd/config"
	"github.com/spf13/cobra"
)

func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay datasets through the sim engine",
	}

	cmd.AddCommand(
		newPricingCmd(rc),
		newEventsCmd(rc),
	)

	return cmd
}
