package sim

import "github.com/rustyeddy/trader/types"

func pnlUnits(side int, entry, exit int32) types.Units {
	// positive is profit in scaled price units
	return types.Units(side) * types.Units(exit-entry)
}

func UnrealizedPL(t Trade, currentPrice types.Price, quoteToAccount types.Rate) types.Money {
	plQuote := types.Money(t.Units) * types.Money(currentPrice-t.EntryPrice)
	return types.Money(int64(plQuote) * int64(quoteToAccount))
}
