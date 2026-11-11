package external

import (
	"fmt"

	"github.com/rustyeddy/trader/internal/account"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// ToWireAccountSnapshot converts s into its deliberately minimal v1
// wire form (strategy.proto's own AccountSnapshot doc comment): only
// account_id/currency/as_of_unix_nanos plus positions — the exact
// slice of account.Snapshot the current in-process strategies
// (strategy/smatrend, strategy/emacross) actually read via
// View.Account().Positions(). Equity, RealizedPnL, UnrealizedPnL,
// OpenOrders, CashBalances, BuyingPower, and margin are intentionally
// not carried; there is no lossless way back to a full
// account.Snapshot from this message, so this package exposes no
// FromWireAccountSnapshot (see doc.go).
func ToWireAccountSnapshot(s account.Snapshot) (*v1.AccountSnapshot, error) {
	// account.Snapshot{} (and any other value that skipped
	// account.NewSnapshot's own validation) is never a valid domain
	// snapshot (account.Snapshot's own doc comment) — review finding:
	// this must reject that rather than silently serializing empty
	// identity/currency and a zero timestamp as if they were real,
	// since the result is embedded in every BarEvent/FillEvent this
	// package builds.
	if s.AccountID().IsZero() {
		return nil, fmt.Errorf("%w: account snapshot: account id must be set", ErrInvalidWireValue)
	}
	if !s.Currency().IsValid() {
		return nil, fmt.Errorf("%w: account snapshot: currency must be valid", ErrInvalidWireValue)
	}
	if s.AsOf().IsZero() {
		return nil, fmt.Errorf("%w: account snapshot: as-of time must be set", ErrInvalidWireValue)
	}

	positions := s.Positions()
	wire := make([]*v1.PositionSnapshot, len(positions))
	for i, p := range positions {
		wp, err := toWirePositionSnapshot(p)
		if err != nil {
			return nil, fmt.Errorf("external: account snapshot: position %d: %w", i, err)
		}
		wire[i] = wp
	}
	return &v1.AccountSnapshot{
		AccountId:     s.AccountID().String(),
		Currency:      s.Currency().String(),
		AsOfUnixNanos: s.AsOf().UTC().UnixNano(),
		Positions:     wire,
	}, nil
}

// toWirePositionSnapshot converts one order.Position to its
// deliberately minimal v1 wire form: instrument_id, side, and
// avg_price only (Quantity and the full Listing are omitted; see
// ToWireAccountSnapshot's own doc comment).
func toWirePositionSnapshot(p runtimeorder.Position) (*v1.PositionSnapshot, error) {
	side, err := toWirePositionSide(p.Side)
	if err != nil {
		return nil, err
	}
	return &v1.PositionSnapshot{
		InstrumentId: p.Listing.InstrumentID().String(),
		Side:         side,
		AvgPrice:     priceOrEmpty(p.AvgPrice),
	}, nil
}
