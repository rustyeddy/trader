package stooq

import (
	"time"

	"github.com/rustyeddy/trader/num"
)

// RawInterval is the provider-native interval token of a raw Stooq
// partition. Stooq's own history for equities is daily-only — there is
// no intraday Stooq data to represent — so RawD1 is, deliberately, the
// only value this package defines; see the package doc comment.
type RawInterval string

// RawD1 is the only raw interval token this package supports.
const RawD1 RawInterval = "d1"

// Record is one provider-native Stooq raw daily bar exactly as
// preserved in the archive: single-price OHLC (Stooq has no separate
// bid/ask history, unlike oanda.Record) and share volume. It is the
// input a later normalization stage turns into a canonical
// marketdata.Bar; Record itself is internal and never crosses the
// public marketdata API.
//
// A Record carries no instrument, interval, or provenance: those are
// identical for every row in a partition and are the caller's
// (marketdata's) concern, not this reader's.
type Record struct {
	// Time is the trading date this record represents, read verbatim
	// from the row's own date field and never reconstructed from row
	// position. It is always midnight UTC of that calendar date — see
	// the package doc comment's "Time semantics" section for why that
	// specific convention was chosen and what it does not yet claim.
	Time time.Time

	Open  num.Price
	High  num.Price
	Low   num.Price
	Close num.Price

	// Volume is the day's share volume — a real, meaningful trading
	// quantity for an equity, unlike oanda.Record.Volume (an activity/
	// tick count). It remains a plain int64, not num.Quantity: this
	// field is informational provenance carried through to
	// marketdata.Bar.Ticks, never an accounting or order quantity
	// (ADR-004's exact-type requirement applies only at the order/
	// accounting boundary, which this value never crosses as-is).
	Volume int64
}
