package tradertest

import (
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// DefaultAsOf is a fixed reference time for builders in this package
// that need one. It carries no significance beyond being deterministic
// and reused, matching every M1 test fixture's habit of hard-coding one
// literal date rather than calling time.Now.
var DefaultAsOf = time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)

// SnapshotParams builds an account.Snapshot. AccountID is required;
// Broker, Currency, AsOf, and Equity default to "OANDA", "USD",
// DefaultAsOf, and "10000". CashBalances defaults to a single entry
// holding Equity in Currency. BuyingPower and MarginAvailable default
// to Equity; MarginUsed, RealizedPnL, UnrealizedPnL, Fees, and
// Financing default to zero — the common case of an account test only
// cares about one or two of these fields and would otherwise have to
// populate all nine by hand.
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
		p.AsOf = DefaultAsOf
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
