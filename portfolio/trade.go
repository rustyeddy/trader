package portfolio

import (
	"github.com/rustyeddy/trader/types"
)

type Trade struct {
	ID         string
	Instrument string
	Side       types.Side
	Units      types.Units
	PNL        types.Money // account currency (best-effort)
	Reason     string
	EntryPrice types.Price
	ExitPrice  types.Price
	EntryTime  types.Timestamp
	ExitTime   types.Timestamp
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
