package account

import (
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Snapshot is one broker account's authoritative observed state at one
// point in time. It is immutable: every field is either a plain value
// or reachable only through an accessor that returns a defensive copy,
// so nothing a caller does with a returned value can change what
// Snapshot itself holds. Construct one with NewSnapshot.
type Snapshot struct {
	accountID id.AccountID
	broker    string
	currency  num.Currency
	asOf      time.Time
	cursor    string

	cashBalances []num.Money

	equity          num.Money
	buyingPower     num.Money
	marginUsed      num.Money
	marginAvailable num.Money
	realizedPnL     num.Money
	unrealizedPnL   num.Money
	fees            num.Money
	financing       num.Money

	positions  []order.Position
	openOrders []order.Order
}

// SnapshotParams supplies NewSnapshot's input. Its fields mirror
// Snapshot's, but are exported and mutable since SnapshotParams is a
// plain, short-lived construction argument, not the immutable value
// itself.
type SnapshotParams struct {
	// AccountID identifies the Trader-managed account this snapshot
	// describes. Must be non-zero.
	AccountID id.AccountID
	// Broker names the broker or provider that reported this snapshot,
	// for example "OANDA". Must be non-empty.
	Broker string
	// Currency is the account's home/settlement currency. Equity,
	// BuyingPower, MarginUsed, MarginAvailable, RealizedPnL,
	// UnrealizedPnL, Fees, and Financing must all be denominated in it.
	Currency num.Currency
	// AsOf is when the broker observed this state. Must be non-zero.
	AsOf time.Time
	// Cursor is an opaque broker-supplied version or cursor token,
	// carried but not interpreted by this package.
	Cursor string

	// CashBalances is the raw cash ledger balance per currency. An FX
	// account may legitimately hold several; no currency may repeat.
	CashBalances []num.Money

	// Equity is the broker-reported net liquidation value.
	Equity num.Money
	// BuyingPower is funds available to open new positions.
	BuyingPower num.Money
	// MarginUsed is margin currently committed to open positions.
	MarginUsed num.Money
	// MarginAvailable is margin available for new positions.
	MarginAvailable num.Money
	// RealizedPnL is cumulative realized profit and loss.
	RealizedPnL num.Money
	// UnrealizedPnL is current unrealized profit and loss on open
	// positions.
	UnrealizedPnL num.Money
	// Fees is cumulative fees observed as of AsOf.
	Fees num.Money
	// Financing is cumulative financing/carry cost or credit observed
	// as of AsOf.
	Financing num.Money

	// Positions is this account's open positions. Every entry's
	// AccountID must equal AccountID, every entry's Listing.Provider
	// must case-insensitively equal Broker, and no two entries may name
	// the same (instrument, provider, venue) listing.
	Positions []order.Position
	// OpenOrders is this account's outstanding orders. Every entry's
	// Request.AccountID must equal AccountID, every entry's
	// Request.Listing.Provider must case-insensitively equal Broker, no
	// entry may already be in a terminal Status, and no two entries may
	// share an OrderID.
	OpenOrders []order.Order
}

// NewSnapshot validates params and returns an immutable Snapshot.
func NewSnapshot(params SnapshotParams) (Snapshot, error) {
	if params.AccountID.IsZero() {
		return Snapshot{}, fmt.Errorf("%w: account id must be set", ErrInvalidSnapshot)
	}
	if params.Broker == "" {
		return Snapshot{}, fmt.Errorf("%w: broker must be set", ErrInvalidSnapshot)
	}
	if !params.Currency.IsValid() {
		return Snapshot{}, fmt.Errorf("%w: currency must be valid", ErrInvalidSnapshot)
	}
	if params.AsOf.IsZero() {
		return Snapshot{}, fmt.Errorf("%w: as-of time must be set", ErrInvalidSnapshot)
	}

	cashBalances, err := checkCashBalances(params.CashBalances)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: cash balances: %v", ErrInvalidSnapshot, err)
	}

	homeFields := map[string]num.Money{
		"equity":           params.Equity,
		"buying power":     params.BuyingPower,
		"margin used":      params.MarginUsed,
		"margin available": params.MarginAvailable,
		"realized pnl":     params.RealizedPnL,
		"unrealized pnl":   params.UnrealizedPnL,
		"fees":             params.Fees,
		"financing":        params.Financing,
	}
	for name, m := range homeFields {
		if !m.IsValid() {
			return Snapshot{}, fmt.Errorf("%w: %s must be valid money", ErrInvalidSnapshot, name)
		}
		if !m.Currency().Equal(params.Currency) {
			return Snapshot{}, fmt.Errorf("%w: %s currency %s does not match account currency %s",
				ErrInvalidSnapshot, name, m.Currency(), params.Currency)
		}
	}

	positions, err := checkPositions(params.AccountID, params.Broker, params.Positions)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: positions: %v", ErrInvalidSnapshot, err)
	}

	openOrders, err := checkOpenOrders(params.AccountID, params.Broker, params.OpenOrders)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: open orders: %v", ErrInvalidSnapshot, err)
	}

	return Snapshot{
		accountID:       params.AccountID,
		broker:          params.Broker,
		currency:        params.Currency,
		asOf:            params.AsOf,
		cursor:          params.Cursor,
		cashBalances:    cashBalances,
		equity:          params.Equity,
		buyingPower:     params.BuyingPower,
		marginUsed:      params.MarginUsed,
		marginAvailable: params.MarginAvailable,
		realizedPnL:     params.RealizedPnL,
		unrealizedPnL:   params.UnrealizedPnL,
		fees:            params.Fees,
		financing:       params.Financing,
		positions:       positions,
		openOrders:      openOrders,
	}, nil
}

func checkCashBalances(balances []num.Money) ([]num.Money, error) {
	seen := make(map[string]struct{}, len(balances))
	cloned := make([]num.Money, len(balances))
	for i, m := range balances {
		if !m.IsValid() {
			return nil, fmt.Errorf("entry %d is not valid money", i)
		}
		code := m.Currency().String()
		if _, ok := seen[code]; ok {
			return nil, fmt.Errorf("duplicate currency %s", code)
		}
		seen[code] = struct{}{}
		cloned[i] = m
	}
	return cloned, nil
}

type listingKey struct {
	instrumentID instrument.ID
	provider     string
	venue        string
}

func keyFor(l instrument.Listing) listingKey {
	return listingKey{instrumentID: l.InstrumentID(), provider: l.Provider(), venue: l.Venue()}
}

func checkPositions(accountID id.AccountID, broker string, positions []order.Position) ([]order.Position, error) {
	seen := make(map[listingKey]struct{}, len(positions))
	cloned := make([]order.Position, len(positions))
	for i, p := range positions {
		validated, err := order.NewPosition(p)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		if validated.AccountID != accountID {
			return nil, fmt.Errorf("entry %d: account id %s does not match snapshot account id %s",
				i, validated.AccountID, accountID)
		}
		if !strings.EqualFold(validated.Listing.Provider(), broker) {
			return nil, fmt.Errorf("entry %d: listing provider %s does not match snapshot broker %s",
				i, validated.Listing.Provider(), broker)
		}
		key := keyFor(validated.Listing)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("entry %d: duplicate listing %s/%s/%s",
				i, key.instrumentID, key.provider, key.venue)
		}
		seen[key] = struct{}{}
		cloned[i] = clonePosition(validated)
	}
	return cloned, nil
}

func checkOpenOrders(accountID id.AccountID, broker string, orders []order.Order) ([]order.Order, error) {
	seen := make(map[id.OrderID]struct{}, len(orders))
	cloned := make([]order.Order, len(orders))
	for i, o := range orders {
		validated, err := order.NewOrder(o)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		if validated.Request.AccountID != accountID {
			return nil, fmt.Errorf("entry %d: account id %s does not match snapshot account id %s",
				i, validated.Request.AccountID, accountID)
		}
		if !strings.EqualFold(validated.Request.Listing.Provider(), broker) {
			return nil, fmt.Errorf("entry %d: listing provider %s does not match snapshot broker %s",
				i, validated.Request.Listing.Provider(), broker)
		}
		if validated.Status.Terminal() {
			return nil, fmt.Errorf("entry %d: order %s is in terminal status %s and is not open",
				i, validated.Request.OrderID, validated.Status)
		}
		orderID := validated.Request.OrderID
		if _, ok := seen[orderID]; ok {
			return nil, fmt.Errorf("entry %d: duplicate order id %s", i, orderID)
		}
		seen[orderID] = struct{}{}
		cloned[i] = cloneOrder(validated)
	}
	return cloned, nil
}

// clonePosition returns a copy of p that shares no pointer state with
// it, so a caller holding the returned value cannot mutate p through
// AvgPrice.
func clonePosition(p order.Position) order.Position {
	cloned := p
	if p.AvgPrice != nil {
		v := *p.AvgPrice
		cloned.AvgPrice = &v
	}
	return cloned
}

// cloneOrder returns a copy of o that shares no pointer or slice state
// with it: every *num.Price/*num.Quantity field, Rejection, and the
// AppliedFillIDs/AppliedBrokerFillIDs slices are independently
// allocated.
func cloneOrder(o order.Order) order.Order {
	cloned := o
	if o.Request.LimitPrice != nil {
		v := *o.Request.LimitPrice
		cloned.Request.LimitPrice = &v
	}
	if o.Request.StopPrice != nil {
		v := *o.Request.StopPrice
		cloned.Request.StopPrice = &v
	}
	if o.AcceptedQuantity != nil {
		v := *o.AcceptedQuantity
		cloned.AcceptedQuantity = &v
	}
	if o.AcceptedLimitPrice != nil {
		v := *o.AcceptedLimitPrice
		cloned.AcceptedLimitPrice = &v
	}
	if o.AcceptedStopPrice != nil {
		v := *o.AcceptedStopPrice
		cloned.AcceptedStopPrice = &v
	}
	if o.AvgFillPrice != nil {
		v := *o.AvgFillPrice
		cloned.AvgFillPrice = &v
	}
	if o.Rejection != nil {
		v := *o.Rejection
		cloned.Rejection = &v
	}
	if o.AppliedFillIDs != nil {
		cloned.AppliedFillIDs = append([]id.FillID(nil), o.AppliedFillIDs...)
	}
	if o.AppliedBrokerFillIDs != nil {
		cloned.AppliedBrokerFillIDs = append([]string(nil), o.AppliedBrokerFillIDs...)
	}
	return cloned
}

// AccountID identifies the account this snapshot describes.
func (s Snapshot) AccountID() id.AccountID { return s.accountID }

// Broker names the broker or provider that reported this snapshot.
func (s Snapshot) Broker() string { return s.broker }

// Currency is the account's home/settlement currency.
func (s Snapshot) Currency() num.Currency { return s.currency }

// AsOf is when the broker observed this state.
func (s Snapshot) AsOf() time.Time { return s.asOf }

// Cursor is an opaque broker-supplied version or cursor token.
func (s Snapshot) Cursor() string { return s.cursor }

// CashBalances returns a copy of the account's raw cash ledger balances
// by currency. Mutating the returned slice does not affect s.
func (s Snapshot) CashBalances() []num.Money {
	return append([]num.Money(nil), s.cashBalances...)
}

// Equity is the broker-reported net liquidation value.
func (s Snapshot) Equity() num.Money { return s.equity }

// BuyingPower is funds available to open new positions.
func (s Snapshot) BuyingPower() num.Money { return s.buyingPower }

// MarginUsed is margin currently committed to open positions.
func (s Snapshot) MarginUsed() num.Money { return s.marginUsed }

// MarginAvailable is margin available for new positions.
func (s Snapshot) MarginAvailable() num.Money { return s.marginAvailable }

// RealizedPnL is cumulative realized profit and loss.
func (s Snapshot) RealizedPnL() num.Money { return s.realizedPnL }

// UnrealizedPnL is current unrealized profit and loss on open positions.
func (s Snapshot) UnrealizedPnL() num.Money { return s.unrealizedPnL }

// Fees is cumulative fees observed as of AsOf.
func (s Snapshot) Fees() num.Money { return s.fees }

// Financing is cumulative financing/carry cost or credit observed as of
// AsOf.
func (s Snapshot) Financing() num.Money { return s.financing }

// Positions returns a deep copy of the account's open positions.
// Mutating the returned slice, or any *num.Price it reaches through
// AvgPrice, does not affect s.
func (s Snapshot) Positions() []order.Position {
	cloned := make([]order.Position, len(s.positions))
	for i, p := range s.positions {
		cloned[i] = clonePosition(p)
	}
	return cloned
}

// OpenOrders returns a deep copy of the account's outstanding orders.
// Mutating the returned slice, or any pointer/slice field it reaches,
// does not affect s.
func (s Snapshot) OpenOrders() []order.Order {
	cloned := make([]order.Order, len(s.openOrders))
	for i, o := range s.openOrders {
		cloned[i] = cloneOrder(o)
	}
	return cloned
}
