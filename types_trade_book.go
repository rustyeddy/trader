package trader

type TradeBook struct {
	Trades map[string]*Trade
}

func (tb *TradeBook) Get(id string) *Trade {
	trade, _ := tb.Trades[id]
	return trade
}
