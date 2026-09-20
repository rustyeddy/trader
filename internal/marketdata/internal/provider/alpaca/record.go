package alpaca

import (
	"time"

	"github.com/rustyeddy/trader/num"
)

// RawInterval is the provider-native interval token of a raw Alpaca
// partition. Phase 1 (ADR-047) only needs daily equity bars, so RawD1
// is, deliberately, the only value this package defines — mirroring
// stooq.RawInterval's identical single-value shape for the identical
// reason.
type RawInterval string

// RawD1 is the only raw interval token this package supports.
const RawD1 RawInterval = "d1"

// Record is one provider-native Alpaca raw daily bar, already
// normalized by this package to the same shape and time convention
// stooq.Record uses: single-price OHLC (Alpaca's historical bars API
// returns one consolidated price series, not a bid/ask spread) and
// share volume, with Time re-anchored to midnight UTC of the bar's own
// trading date (see the package doc comment's "Timestamp
// normalization" section — this is not simply the raw fetched
// timestamp). Record itself is internal and never crosses the public
// marketdata API.
//
// A Record carries no instrument, interval, or provenance: those are
// identical for every row in a partition and are the caller's
// (marketdata's) concern, not this package's.
type Record struct {
	Time time.Time

	Open  num.Price
	High  num.Price
	Low   num.Price
	Close num.Price

	// Volume is the day's share volume, a real trading quantity for an
	// equity. It remains a plain int64, not num.Quantity — informational
	// provenance carried through to marketdata.Bar.Ticks, never an
	// accounting or order quantity (ADR-004's exact-type requirement
	// applies only at the order/accounting boundary), matching
	// stooq.Record.Volume's identical convention.
	Volume int64
}
