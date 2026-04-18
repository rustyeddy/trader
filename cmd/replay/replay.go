package replay

import (
	traderpkg "github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
)

func New(rc *traderpkg.RootConfig) *cobra.Command {
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
