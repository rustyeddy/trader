package trader

import (
	"time"

	"github.com/rustyeddy/trader/num"
)

type position struct {
	instrument string
	direction  direction
	units      num.Quantity
	entryPrice num.Price
	entryTime  time.Time // the bar the open filled on
	markPrice  num.Price // latest price the position was valued at: the fill, then each bar's close
}

// trade is a position that actually closed. An open position is not a trade
// yet, and is not a win or a loss.
type trade struct {
	instrument string
	direction  direction
	units      num.Quantity
	entryPrice num.Price
	entryTime  time.Time
	exitPrice  num.Price
	exitTime   time.Time
	reason     orderReason // what closed it
	pnl        num.Money   // realized, exact; floored to cents only when printed
}
