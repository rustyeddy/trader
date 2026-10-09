package backtest

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/cmd/internal/backtestcfg"
	"github.com/rustyeddy/trader/cmd/trader/internal/clictx"
	"github.com/rustyeddy/trader/internal/config"
)

// buildRunConfig resolves a backtestcfg.RunConfig from any flags actually Changed
// on cmd, layered under flags.config (if set) and the TRADER_BACKTEST_*/
// TRADER_STRATEGY_* environment variables, via the same config.Load
// every Trader composition root uses (cmd/trader/data/service.go's
// buildDatasetConfig is the identical pattern this mirrors, including
// why only Changed flags are ever placed in Overrides).
//
// --symbol is repeatable (multi-instrument, issue #224) but backtestcfg.RunConfig's
// own Symbol field is a single string: a config file describes one
// instrument with backtest.symbol, or a comma-separated universe with
// backtest.symbols (issue #469). Whether --config may be combined with
// more than one --symbol depends on the resolved strategy, so that rule
// lives in runBacktest, after this has loaded the config.
func buildRunConfig(cmd *cobra.Command, flags runFlags) (backtestcfg.RunConfig, error) {
	overrides := map[string]string{}
	if cmd.Flags().Changed("symbol") && len(flags.symbols) == 1 {
		overrides["symbol"] = flags.symbols[0]
	}
	if cmd.Flags().Changed("symbol") {
		// Explicit --symbol flags replace the config's whole universe, so
		// a config's backtest.symbols can never combine with them.
		overrides["symbols"] = ""
	}
	if cmd.Flags().Changed("interval") {
		overrides["interval"] = flags.interval
	}
	if cmd.Flags().Changed("from") {
		overrides["from"] = flags.from
	}
	if cmd.Flags().Changed("to") {
		overrides["to"] = flags.to
	}
	if cmd.Flags().Changed("currency") {
		overrides["currency"] = flags.currency
	}
	if cmd.Flags().Changed("starting-cash") {
		overrides["starting-cash"] = flags.startingCash
	}
	if cmd.Flags().Changed("risk-fraction") {
		overrides["risk-fraction"] = flags.riskFraction
	}
	if cmd.Flags().Changed("adverse-distance") {
		overrides["adverse-distance"] = flags.adverse
	}
	if cmd.Flags().Changed("initial-margin-ratio") {
		overrides["initial-margin-ratio"] = flags.initialMarginRatio
	}
	if cmd.Flags().Changed("strategy-name") {
		overrides["strategy-name"] = flags.strategyName
	}
	if cmd.Flags().Changed("fast-period") {
		overrides["fast-period"] = fmt.Sprintf("%d", flags.fastPeriod)
	}
	if cmd.Flags().Changed("slow-period") {
		overrides["slow-period"] = fmt.Sprintf("%d", flags.slowPeriod)
	}
	if cmd.Flags().Changed("allowed-side") {
		overrides["allowed-side"] = flags.allowedSide
	}
	if cmd.Flags().Changed("quantity") {
		overrides["quantity"] = flags.quantity
	}
	if cmd.Flags().Changed("buy-date") {
		overrides["buy-date"] = flags.buyDate
	}
	if cmd.Flags().Changed("sell-date") {
		overrides["sell-date"] = flags.sellDate
	}
	if cmd.Flags().Changed("strategy-exec") {
		overrides["strategy-exec"] = flags.strategyExec
	}
	if cmd.Flags().Changed("strategy-config") {
		overrides["strategy-config"] = flags.strategyConfig
	}
	if cmd.Flags().Changed("data-store-root") {
		overrides["data-store-root"] = flags.dataStoreRoot
	}
	if cmd.Flags().Changed("data-raw-root") {
		overrides["data-raw-root"] = flags.dataRawRoot
	}
	if cmd.Flags().Changed("provider") {
		overrides["provider"] = flags.provider
	}

	return backtestcfg.LoadRunConfig(config.Options{
		EnvPrefix: clictx.EnvPrefix,
		Environ:   os.Environ(),
		FilePath:  flags.config,
		Overrides: overrides,
	})
}
