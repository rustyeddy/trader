package tradehistory

import (
	"errors"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Errors reported by Reconstruct.
var (
	// ErrMixedStreams reports activities from more than one provider
	// environment account. A reconstruction is for one stream.
	ErrMixedStreams = errors.New("tradehistory: activities span more than one stream")
	// ErrInvalidActivity reports an input activity that fails Validate.
	ErrInvalidActivity = errors.New("tradehistory: invalid activity")
	// ErrConflict reports two input activities with the same Key but
	// different content.
	ErrConflict = errors.New("tradehistory: the same activity appears with different content")
)

// Leg is one entry or exit: a quantity of an instrument at a price, from one
// activity. A fill that the broker split into per-trade effects yields one leg
// per effect (with that trade's exact price and ID); otherwise it yields one
// leg per portion of the fill.
type Leg struct {
	// Source is the activity the leg came from.
	Source accountactivity.Key
	At     time.Time
	// Quantity is the leg's absolute size.
	Quantity num.Quantity
	// Price keeps the provenance of the figure used: a per-trade price when
	// the broker supplied one, else the fill-level price, else unknown.
	Price accountactivity.Value[num.Price]
	// BrokerTradeID is the broker's trade the leg belongs to, when the
	// broker supplied per-trade effects.
	BrokerTradeID string
	// Split is true when the division of a fill between closing and opening
	// was derived from the net position because the broker gave no per-trade
	// units, not taken from broker facts.
	Split bool
}

// Trade is one completed position cycle.
type Trade struct {
	// ID is deterministic: the first entry's activity key and the side.
	ID     string
	Stream accountactivity.SyncKey
	// Instrument is the canonical instrument.
	Instrument instrument.ID
	// Side is the side of the entries: Buy for a long trade, Sell for a short.
	Side order.Side

	OpenedAt, ClosedAt time.Time
	Entries, Exits     []Leg
	// EntryQuantity and ExitQuantity are exact and equal for a completed trade.
	EntryQuantity, ExitQuantity num.Quantity
	// AvgEntryPrice and AvgExitPrice are Derived weighted averages, unknown
	// when a leg has no price or the quote currency is unknown.
	AvgEntryPrice, AvgExitPrice accountactivity.Value[num.Price]

	// GrossPL is the sum of broker-reported realized P/L.
	GrossPL accountactivity.Value[num.Money]
	// SpreadCost is already embedded in GrossPL and is not added into Net.
	SpreadCost accountactivity.Value[num.Money]
	Commission accountactivity.Value[num.Money]
	Financing  accountactivity.Value[num.Money]
	OtherFees  accountactivity.Value[num.Money]
	// Net is GrossPL + Commission + Financing + OtherFees, known only when
	// all four are.
	Net accountactivity.Value[num.Money]

	// Sources are the activities the trade was derived from, in the order
	// they were first used.
	Sources []accountactivity.Key
}

// OpenPosition is a position still open at the end of the data: the entries
// so far, any partial exits, and what remains. It is not a completed trade
// and its costs are not summarized.
type OpenPosition struct {
	ID         string
	Stream     accountactivity.SyncKey
	Instrument instrument.ID
	Side       order.Side
	OpenedAt   time.Time
	Entries    []Leg
	Exits      []Leg
	Remaining  num.Quantity
	// AvgEntryPrice is the Derived weighted average of the entries so far.
	AvgEntryPrice accountactivity.Value[num.Price]
	Sources       []accountactivity.Key
}

// Orphan is an exit, or part of one, with no known open position to close:
// the data window starts after the entry. Its costs are not attributed to
// any trade.
type Orphan struct {
	Source   accountactivity.Key
	Quantity num.Quantity
}

// Result is the full reconstruction.
type Result struct {
	// Completed trades, ordered by close time then ID.
	Completed []Trade
	// Open positions, ordered by instrument, side and open time.
	Open []OpenPosition
	// Unattributed lists standalone financing and fee activities, which
	// belong to no individual trade yet.
	Unattributed []accountactivity.Key
	// Orphans lists exits with no known entry.
	Orphans []Orphan
}
