package risk

// EUR_USD → quote = USD → QuoteToAccount = 1.0
// USD_JPY → quote = JPY → QuoteToAccount = 1 / USDJPY_mid

import "math"

type Inputs struct {
	Equity         float64
	RiskPct        float64 // 0.005
	EntryPrice     float64
	StopPrice      float64
	PipLocation    int
	QuoteToAccount float64 // USD quote → 1.0, JPY quote → JPYUSD
}

type Result struct {
	Units       float64
	StopPips    float64
	RiskAmount  float64
}

func pipSize(loc int) float64 {
	return math.Pow(10, float64(loc))
}

// PipSize returns the pip size for a given pip location.
// This is a public helper function for calculating pip sizes.
func PipSize(loc int) float64 {
	return pipSize(loc)
}

func Calculate(in Inputs) Result {
	pip := pipSize(in.PipLocation)
	stopPips := math.Abs(in.EntryPrice-in.StopPrice) / pip

	riskAmt := in.Equity * in.RiskPct
	pipValuePerUnit := pip * in.QuoteToAccount

	units := riskAmt / (stopPips * pipValuePerUnit)

	return Result{
		Units:      math.Floor(units),
		StopPips:  stopPips,
		RiskAmount: riskAmt,
	}
}
