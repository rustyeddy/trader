// Package strategy is the public seam between Trader and a strategy that is not
// part of Trader: the callback a strategy implements and the order it may
// request. A strategy in another repository imports this package (and, if it
// wants, marketdata, num and instrument) and nothing under internal/.
//
// An Order here is a request, the requested stage of Trader's one order
// (ADR-093). It carries what a strategy may say and nothing Trader owns: no
// quantity, id, account, state or fill. Trader validates it again, sizes it,
// admits it and executes it; this package never does.
package strategy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// Strategy decides what to trade. OnBars is called once per availability instant
// with every bar that became available then: the bars are fresh and in the run's
// instrument order, and an instrument with no bar at that instant is absent. It
// returns the orders it wants, in the order they should be submitted, or none. It
// sees the whole group before any order is submitted. bars is only valid for the
// call. An error stops the run.
type Strategy interface {
	OnBars(ctx context.Context, bars []marketdata.Bar) ([]Order, error)
}

// Warmer is implemented by a strategy that can prefill its indicators. The bars
// it is given came before the first one the strategy trades: it learns from them
// and takes no position. A strategy that does not implement it is not warmed.
type Warmer interface {
	Warm(ctx context.Context, bars []marketdata.Bar)
}

// Action is what an order asks to do to the position in its instrument.
type Action string

// The actions.
const (
	Open   Action = "open"
	Add    Action = "add"
	Reduce Action = "reduce"
	Close  Action = "close"
	// Amend changes the protective stop, trail or target of the open position:
	// it only ever tightens a stop or a trail.
	Amend Action = "amend"
)

// Direction is the side of a position (not a buy or sell).
type Direction string

// The directions.
const (
	Long  Direction = "long"
	Short Direction = "short"
)

// OrderType is how an opening order is triggered. The empty type is Market.
type OrderType string

// The order types. Limit and Stop orders can only open.
const (
	Market OrderType = "market"
	Limit  OrderType = "limit"
	Stop   OrderType = "stop"
)

// MaxPrice is the largest price an order may carry.
var MaxPrice = num.MustParsePrice("10000")

// MaxRefLen is the longest Ref.
const MaxRefLen = 64

// ErrInvalidOrder marks an order that is not a legal request. It is a strategy
// bug, and fails the run.
var ErrInvalidOrder = errors.New("invalid order")

// Order is a request. A zero price is absent. The zero Type is Market.
type Order struct {
	// Instrument is the symbol, for example "EURUSD".
	Instrument string
	Action     Action
	// Direction is Long or Short for an Open and empty for every other action.
	Direction Direction
	Type      OrderType

	// Price is the reference price an Open is sized from (for a Limit or Stop
	// order, its trigger price). Required for Open; absent otherwise.
	Price num.Price
	// StopLoss is required for an Open, on the losing side of Price; for an
	// Amend it is the new stop level.
	StopLoss num.Price
	// TakeProfit is optional for an Open, on the winning side of Price; for an
	// Amend it is the new target.
	TakeProfit num.Price
	// Trail is the distance the protective stop keeps behind the market, for an
	// Open or an Amend.
	Trail num.Price
	// Expires is when a waiting Open lapses. It must be after the frame the order
	// was decided on. Zero means good until cancelled.
	Expires time.Time

	// Ref is the strategy's own label, echoed in Trader's records. It is not an
	// identifier. 1 to MaxRefLen printable ASCII characters, or empty.
	Ref string
}

// Validate checks that o is a legal request, using only what the order and the
// frame it was decided on say. frameAt is the availability instant of that frame;
// a zero frameAt skips the expiry check. Whether the instrument is part of the
// run, and everything that depends on the account or its position, is Trader's to
// check. The error wraps ErrInvalidOrder and names the member.
func (o Order) Validate(frameAt time.Time) error {
	bad := func(member, format string, args ...any) error {
		return fmt.Errorf("%w: %s: %s", ErrInvalidOrder, member, fmt.Sprintf(format, args...))
	}
	if o.Instrument == "" {
		return bad("instrument", "required")
	}
	switch o.Action {
	case Open, Add, Reduce, Close, Amend:
	default:
		return bad("action", "%q is not open, add, reduce, close or amend", o.Action)
	}
	switch o.Type {
	case "", Market:
	case Limit, Stop:
		if o.Action != Open {
			return bad("type", "a %s order can only open", o.Type)
		}
	default:
		return bad("type", "%q is not market, limit or stop", o.Type)
	}
	if err := o.checkRef(); err != nil {
		return err
	}
	for member, p := range map[string]num.Price{"price": o.Price, "stop_loss": o.StopLoss, "take_profit": o.TakeProfit, "trail": o.Trail} {
		if p.Cmp(MaxPrice) > 0 {
			return bad(member, "%s is above %s", p, MaxPrice)
		}
	}

	switch o.Action {
	case Open:
		return o.validateOpen(frameAt, bad)
	case Amend:
		if o.Direction != "" {
			return bad("direction", "an amend takes no direction")
		}
		if !o.Price.IsZero() {
			return bad("price", "an amend takes no price")
		}
		if !o.Expires.IsZero() {
			return bad("expires", "only an open expires")
		}
		if o.StopLoss.IsZero() && o.TakeProfit.IsZero() && o.Trail.IsZero() {
			return bad("action", "an amend needs a stop_loss, take_profit or trail")
		}
		return nil
	default: // add, reduce, close
		if o.Direction != "" {
			return bad("direction", "%s takes no direction", o.Action)
		}
		for member, set := range map[string]bool{"price": !o.Price.IsZero(), "stop_loss": !o.StopLoss.IsZero(),
			"take_profit": !o.TakeProfit.IsZero(), "trail": !o.Trail.IsZero(), "expires": !o.Expires.IsZero()} {
			if set {
				return bad(member, "%s takes no %s", o.Action, member)
			}
		}
		return nil
	}
}

func (o Order) validateOpen(frameAt time.Time, bad func(string, string, ...any) error) error {
	if o.Direction != Long && o.Direction != Short {
		return bad("direction", "an open needs long or short, got %q", o.Direction)
	}
	if o.Price.IsZero() {
		return bad("price", "an open needs a reference price")
	}
	if o.StopLoss.IsZero() {
		return bad("stop_loss", "an open needs a stop loss")
	}
	long := o.Direction == Long
	if (long && o.StopLoss.Cmp(o.Price) >= 0) || (!long && o.StopLoss.Cmp(o.Price) <= 0) {
		return bad("stop_loss", "%s stop %s is not on the losing side of %s", o.Direction, o.StopLoss, o.Price)
	}
	if !o.TakeProfit.IsZero() && ((long && o.TakeProfit.Cmp(o.Price) <= 0) || (!long && o.TakeProfit.Cmp(o.Price) >= 0)) {
		return bad("take_profit", "%s target %s is not on the winning side of %s", o.Direction, o.TakeProfit, o.Price)
	}
	if !o.Expires.IsZero() && !frameAt.IsZero() && !o.Expires.After(frameAt) {
		return bad("expires", "%s is not after the frame at %s", o.Expires.UTC().Format(time.RFC3339), frameAt.UTC().Format(time.RFC3339))
	}
	return nil
}

func (o Order) checkRef() error {
	if o.Ref == "" {
		return nil
	}
	if len(o.Ref) > MaxRefLen {
		return fmt.Errorf("%w: ref: longer than %d characters", ErrInvalidOrder, MaxRefLen)
	}
	for i := 0; i < len(o.Ref); i++ {
		if c := o.Ref[i]; c < 0x20 || c > 0x7e {
			return fmt.Errorf("%w: ref: character %d is not printable ASCII", ErrInvalidOrder, i)
		}
	}
	return nil
}
