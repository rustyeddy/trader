package backtest

import (
	"github.com/spf13/cobra"
)

var tmplStrategyOpts = newCandleCmdCommon()

func runTmplStrategyConfig(cmd *cobra.Command) error {
	return runConfiguredStrategyCommand(cmd, "template", &tmplStrategyOpts, nil)
}
