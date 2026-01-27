package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "trader",
	Short: "A professional-grade FX trading simulator and research platform",
	Long: `Trader is a comprehensive trading simulator and research platform written in Go.

It provides tools for:
  - Backtesting trading strategies with historical data
  - Running simulations with custom configurations
  - Managing trade journals and equity curves
  - Downloading market data from OANDA
  - Risk-based position sizing
  - FX-correct P/L accounting

Complete documentation is available at https://github.com/rustyeddy/trader`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Global flags can be added here if needed
	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.trader.yaml)")
}
