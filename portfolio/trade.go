package portfolio

import (
	"github.com/rustyeddy/trader/types"
)

type Trade struct {
	ID         string
	Common     CommonPortfolio
	OpenPrice  types.Price // EntryPrice - OpenPrice = slippage
	FillPrice  types.Price
	ClosePrice types.Price // ExitPrice - closePrice = slippage
	ExitPrice  types.Price
	EntryTime  types.Timestamp
	ExitTime   types.Timestamp
	PNL        types.Money // account currency (best-effort)
}

type Trades struct {
	trades []Trade
}

func (t *Trades) Add(trade Trade) {
	t.trades = append(t.trades, trade)
}

func (t *Trades) Len() int {
	return len(t.trades)
}

func (t *Trades) Get(i int) *Trade {
	if i >= t.Len() {
		return nil
	}
	return &t.trades[i]
}
