package risk

import "math"

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// PlannedRiskUSD computes absolute $ risk if stop is hit.
// NOTE: For EUR/USD pip-value simplicity, you can treat 1 pip = 0.0001 in price terms.
// For general FX, you’ll eventually want instrument metadata and quote conversion.
func PlannedRiskUSD(units, entry, stop, quoteToAccountRate float64) float64 {
	// price move in quote currency per 1 unit of base:
	move := abs(entry - stop) // in quote currency (e.g., USD per EUR)
	// P/L in quote currency = units * move
	plQuote := units * move
	// Convert quote currency -> account currency (often 1.0 for USD account trading EUR/USD)
	return plQuote * quoteToAccountRate
}

func RR(entry, stop, takeProfit float64) float64 {
	risk := abs(entry - stop)
	reward := abs(takeProfit - entry)
	if risk == 0 {
		return 0
	}
	return reward / risk
}

func RiskPct(plannedRiskUSD, equity float64) float64 {
	if equity <= 0 {
		return math.Inf(1)
	}
	return plannedRiskUSD / equity
}
