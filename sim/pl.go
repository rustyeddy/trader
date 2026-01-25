package sim

func UnrealizedPL(t Trade, currentPrice float64, quoteToAccount float64) float64 {
	plQuote := t.Units * (currentPrice - t.EntryPrice)
	return plQuote * quoteToAccount
}
