package data

import (
	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/config"
)

func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data",
		Short: "Download tick data and build candles",
	}

	cmd.AddCommand(
		newDownloadTicksCmd(rc),
		newBuildCandlesCmd(rc),
		newSyncCmd(rc),
	)

	return cmd
}
