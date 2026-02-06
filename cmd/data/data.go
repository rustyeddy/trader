package data

import (
	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/cmd/config"
)

func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data",
		Short: "Download/prepare datasets (OANDA → CSV, transforms, etc.)",
	}

	cmd.AddCommand(
		newOandaCmd(rc),
		newOandaTicksCmd(rc),
		newOandaCandlesCmd(rc),
	)

	return cmd
}
