package main

import (
	"fmt"

	"github.com/rustyeddy/trader/risk"
)

func main() {
	size := risk.Calculate(risk.Inputs{
		Equity:         10000,
		RiskPct:        0.01,
		EntryPrice:     1.1000,
		StopPrice:      1.0950,
		PipLocation:    -4,
		QuoteToAccount: 1.0,
	})
	fmt.Printf("units=%0.f stopPips=%0.1f\n", size.Units, size.StopPips)
}
