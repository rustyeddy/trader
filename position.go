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

	// openOrder is the id of the open that made the position and plannedRisk what
	// that order planned to lose when it was sized (see orderRisk); a later amend
	// or trail of the stop changes neither. They are only recorded.
	openOrder   string
	plannedRisk num.Money
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
