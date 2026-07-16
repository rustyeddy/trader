package signalreplay

import (
	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/config"
)

var rootCfg *config.RootConfig

// New returns the top-level "signalreplay" cobra command.
func New(rc *config.RootConfig) *cobra.Command {
	rootCfg = rc
	cmd := &cobra.Command{
		Use:   "signalreplay",
		Short: "Backtest trader review scanner signals as a synthetic strategy",
		Long: `signalreplay is an analysis harness: it replays a "trader review" sweep
CSV through the existing backtest runner to measure whether the scanner's
"tradeable" classification has edge. It is not a live trading strategy.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(cmdGen(), cmdRun(), cmdReport())
	return cmd
}
