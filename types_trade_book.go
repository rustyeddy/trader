package trader

type tradeBook struct {
	Trades map[string]*Trade
}

func (tb *tradeBook) Get(id string) *Trade {
	trade, _ := tb.Trades[id]
	return trade
}
