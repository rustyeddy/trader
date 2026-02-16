package backtest

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	CMDBacktest.AddCommand(CMDBacktestEMACross)
}

var CMDBacktest = &cobra.Command{
	Use:   "backtest",
	Short: "Run backtests on historical data",
	RunE: func(cmd *cobra.Command, args []string) error {

		fmt.Println("")
		fmt.Println("***********************************")
		fmt.Println("TODO: list out backtest strategies")
		fmt.Println("***********************************")

		return nil
	},
}
