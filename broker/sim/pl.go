package sim

import "github.com/rustyeddy/trader/market"

func pnlUnits(side int, entry, exit int32) int64 {
	// positive is profit in scaled price units
	return int64(side) * int64(exit-entry)
}

func UnrealizedPL(t Trade, currentPrice market.Price, quoteToAccount market.Price) market.Cash {
	plQuote := t.Units * (currentPrice - t.EntryPrice)
	return Cash(plQuote * quoteToAccount)
}
