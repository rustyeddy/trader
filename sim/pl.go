package sim

func pnlUnits(side int, entry, exit int32) int64 {
	// positive is profit in scaled price units
	return int64(side) * int64(exit-entry)
}

func UnrealizedPL(t Trade, currentPrice float64, quoteToAccount float64) float64 {
	plQuote := t.Units * (currentPrice - t.EntryPrice)
	return plQuote * quoteToAccount
}
