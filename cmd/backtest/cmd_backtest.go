package backtest

import (
	"github.com/rustyeddy/trader/cmd/config"
	"github.com/spf13/cobra"
)

func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backtest",
		Short: "Run backtests on historical data",
	}

	cmd.AddCommand(
		newEmaCrossCmd(rc),
		newEMAADXCmd(rc),
		newH1EmaCrossCmd(rc),
	)

	return cmd
}
