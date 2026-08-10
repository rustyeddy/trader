package tradertest

import (
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// defaultAsOf is the fixed reference time DefaultAsOf returns. It
// carries no significance beyond being deterministic and reused,
// matching every M1 test fixture's habit of hard-coding one literal
// date rather than calling time.Now. It is unexported and returned
// only through a function, not an exported var: a mutable package-level
// variable would let one test's assignment leak into every other test
// sharing this default, which is exactly the kind of global mutable
// state a determinism-focused package must not introduce.
var defaultAsOf = time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)

// DefaultAsOf returns the fixed reference time builders in this package
// use when AsOf is left zero. Tests that need to compare against the
// default explicitly (rather than just accepting whatever a builder
// produced) can call this instead of hard-coding the literal date
// themselves.
func DefaultAsOf() time.Time { return defaultAsOf }

// SnapshotParams builds an account.Snapshot. AccountID is required;
// Broker, Currency, AsOf, and Equity default to "OANDA", "USD",
// DefaultAsOf(), and "10000". CashBalances defaults, when nil, to a
// single entry holding Equity in Currency; an explicitly empty
// (non-nil) slice is preserved as-is rather than defaulted. BuyingPower
// and MarginAvailable default to Equity; MarginUsed, RealizedPnL,
// UnrealizedPnL, Fees, and Financing default to zero — the common case
// of an account test only cares about one or two of these fields and
// would otherwise have to populate all nine by hand.
type SnapshotParams struct {
	AccountID       id.AccountID
	Broker          string
	Currency        string
	AsOf            time.Time
	Cursor          string
	CashBalances    []num.Money
	Equity          string
	BuyingPower     string
	MarginUsed      string
	MarginAvailable string
	RealizedPnL     string
	UnrealizedPnL   string
	Fees            string
	Financing       string
	Positions       []order.Position
	OpenOrders      []order.Order
}

// NewSnapshot returns a valid account.Snapshot built from p, filling in
// defaults for zero-valued fields.
func NewSnapshot(p SnapshotParams) (account.Snapshot, error) {
	if p.Broker == "" {
		p.Broker = "OANDA"
	}
	if p.Currency == "" {
		p.Currency = "USD"
	}
	if p.AsOf.IsZero() {
		p.AsOf = defaultAsOf
	}
	if p.Equity == "" {
		p.Equity = "10000"
	}
	if p.BuyingPower == "" {
		p.BuyingPower = p.Equity
	}
	if p.MarginAvailable == "" {
		p.MarginAvailable = p.Equity
	}
	for _, s := range []*string{&p.MarginUsed, &p.RealizedPnL, &p.UnrealizedPnL, &p.Fees, &p.Financing} {
		if *s == "" {
			*s = "0"
		}
	}

	currency, err := num.ParseCurrency(p.Currency)
	if err != nil {
		return account.Snapshot{}, err
	}

	equity, err := num.ParseMoney(p.Equity, currency)
	if err != nil {
		return account.Snapshot{}, err
	}
	buyingPower, err := num.ParseMoney(p.BuyingPower, currency)
	if err != nil {
		return account.Snapshot{}, err
	}
	marginUsed, err := num.ParseMoney(p.MarginUsed, currency)
	if err != nil {
		return account.Snapshot{}, err
	}
	marginAvailable, err := num.ParseMoney(p.MarginAvailable, currency)
	if err != nil {
		return account.Snapshot{}, err
	}
	realizedPnL, err := num.ParseMoney(p.RealizedPnL, currency)
	if err != nil {
		return account.Snapshot{}, err
	}
	unrealizedPnL, err := num.ParseMoney(p.UnrealizedPnL, currency)
	if err != nil {
		return account.Snapshot{}, err
	}
	fees, err := num.ParseMoney(p.Fees, currency)
	if err != nil {
		return account.Snapshot{}, err
	}
	financing, err := num.ParseMoney(p.Financing, currency)
	if err != nil {
		return account.Snapshot{}, err
	}

	cashBalances := p.CashBalances
	if cashBalances == nil {
		cashBalances = []num.Money{equity}
	}

	return account.NewSnapshot(account.SnapshotParams{
		AccountID:       p.AccountID,
		Broker:          p.Broker,
		Currency:        currency,
		AsOf:            p.AsOf,
		Cursor:          p.Cursor,
		CashBalances:    cashBalances,
		Equity:          equity,
		BuyingPower:     buyingPower,
		MarginUsed:      marginUsed,
		MarginAvailable: marginAvailable,
		RealizedPnL:     realizedPnL,
		UnrealizedPnL:   unrealizedPnL,
		Fees:            fees,
		Financing:       financing,
		Positions:       p.Positions,
		OpenOrders:      p.OpenOrders,
	})
}

// MustNewSnapshot is like NewSnapshot but panics on error.
func MustNewSnapshot(p SnapshotParams) account.Snapshot {
	s, err := NewSnapshot(p)
	if err != nil {
		panic(err)
	}
	return s
}
