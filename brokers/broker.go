// Package brokers holds the Broker interface — the execution-venue
// contract shared across concrete implementations (brokers/oanda,
// eventually brokers/sim, brokers/alpaca, ...). See
// docs/Manual/architecture-broker-account-order.org.
package brokers

import (
	"context"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/brokers/sim"
	"github.com/rustyeddy/trader/market"
)

// Broker is the execution-venue contract: placing/closing orders, reading
// account state, and streaming fills/transactions. Deliberately excludes
// market data (candles, live pricing) — see DataProvider (not yet defined;
// docs/Manual/architecture-broker-account-order.org). Resist adding
// data-fetching methods here just because one concrete implementation
// (OANDA) happens to also serve market data.
//
// Method signatures mirror oanda.Client's existing methods exactly (same
// oanda.* DTOs) so *oanda.Client satisfies this with zero changes to the
// oanda package. A known, deliberate limitation of this phase: a future
// Alpaca/Interactive Brokers implementation would need to produce
// oanda-shaped response structs until those DTOs are made broker-agnostic
// in a later phase.
//
// brokers/sim also satisfies Broker — a simulated fill against tracked
// prices instead of a real network round-trip. See
// docs/Manual/architecture-broker-account-order.org, phase 4.
type Broker interface {
	GetAccountSummary(ctx context.Context, accountID string) (*oanda.AccountSummary, error)
	GetAccountDetails(ctx context.Context, accountID string) (*oanda.AccountDetails, error)
	GetAccountChanges(ctx context.Context, accountID string, sinceID int64) (*oanda.AccountChangesResult, error)
	GetAccounts(ctx context.Context) ([]oanda.AccountRef, error)
	GetTransactions(ctx context.Context, accountID string, sinceID int64) ([]oanda.Transaction, int64, error)
	StreamTransactions(ctx context.Context, accountID string, opts oanda.StreamOptions) (<-chan oanda.TxEvent, error)
	GetOpenTrades(ctx context.Context, accountID string) ([]oanda.OpenTrade, error)
	CloseTrade(ctx context.Context, accountID, tradeID string, units int64) (*oanda.CloseTradeResult, error)
	UpdateTradeStop(ctx context.Context, accountID, tradeID string, stopPrice, takePrice float64) error
	SubmitMarketOrder(ctx context.Context, accountID, instrument string, units int64, stopPrice float64) (*oanda.OrderResult, error)
}

// compile-time assertions: *oanda.Client and *sim.Sim both satisfy Broker.
var (
	_ Broker = (*oanda.Client)(nil)
	_ Broker = (*sim.Sim)(nil)
)

// PriceUpdater is implemented by Broker implementations that need to be
// told the current price to fill/monitor resting orders against (Sim).
// Not part of Broker itself — a real venue like oanda.Client doesn't need
// to be told prices, it has its own market data. Callers that need this
// (backtest driving Sim bar-by-bar) type-assert Broker against it.
type PriceUpdater interface {
	UpdatePrice(tick market.Tick) error
}

var _ PriceUpdater = (*sim.Sim)(nil)
