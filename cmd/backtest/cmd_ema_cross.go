package backtest

import (
	"github.com/rustyeddy/trader/market/strategies"
	"github.com/spf13/cobra"
)

var cfg = strategies.EMACrossConfig{}

var CMDBacktestEMACross = &cobra.Command{
	Use:   "ema-cross",
	Short: "Run EMA Cross backtest strategy on H1",
	RunE:  RunEMACross,
}

func init() {

	scfg := strategies.StrategyConfig{
		Balance: 1000,
		Stop:    20,
		Take:    40,
		RR:      0.02,

		File: "data/prices/derived/eur_usd/d1/eur_usd-d1-2025.csv",
	}
	cfg.StrategyConfig = scfg

	cmd := CMDBacktestEMACross
	cmd.Flags().StringVar(&cfg.File, "file", "", "Path to Dukascopy-style M1 candles file (semicolon-separated)")
}

func RunEMACross(cmd *cobra.Command, args []string) error {

	return nil
}
