package backtest

import (
	"fmt"

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
	cmd := CMDBacktestEMACross
	cmd.Flags().StringVar(&cfg.File, "file", "", "Path to Dukascopy-style M1 candles file (semicolon-separated)")
	// cmd.Flags().StringVar(&instrument, "instrument", "EUR_USD", "Instrument (e.g. EUR_USD)")
	// cmd.Flags().IntVar(&fast, "fast", 9, "Fast EMA period")
	// cmd.Flags().IntVar(&slow, "slow", 21, "Slow EMA period")
	// cmd.Flags().Float64Var(&minSpread, "min-spread", 0, "Min |fast-slow| (price units) required to signal; 0 disables")
	// cmd.Flags().IntVar(&minValid, "min-valid", 50, "Minimum valid M1 bars per hour to keep an H1 candle")

}

func RunEMACross(cmd *cobra.Command, args []string) error {

	fmt.Println("EMA Cross backtest strategy")

	return nil
}
