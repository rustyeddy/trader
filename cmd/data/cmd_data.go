package data

import (
	traderpkg "github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
)

func New(rc *traderpkg.RootConfig) *cobra.Command {
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
