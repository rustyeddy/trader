package trades

import (
	"github.com/rustyeddy/trader/cmd/config"
	"github.com/spf13/cobra"
)

func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trade",
		Short: "trade will display trades from the sim engine",
		RunE: func(cmd *cobra.Command, args []string) error {
			// This is where we open the trades and start checking them out

			// 1. Open DB trades table

			// 2. Start querying the trades table

			// 3. Loop through each trade and print them out

			// 4. Provide a total summary and stats

			return nil
		},
	}
	return cmd
}
