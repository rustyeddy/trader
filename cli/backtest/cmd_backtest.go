package backtest

import (
	"github.com/rustyeddy/trader/cli/config"
	"github.com/spf13/cobra"
)

func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backtest",
		Short: "Run backtests on historical data",
	}

	cmd.AddCommand(
		newEmaCrossCmd(rc),
	)

	return cmd
}
