package oanda

import (
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// RawInterval is the provider-native interval token of a raw OANDA partition,
// for example RawH1. The reader deliberately stays independent of
// marketdata's canonical Interval type: it emits provider-native records, and
// mapping a RawInterval to a marketdata.Interval is a later normalization
// concern. Keeping this package free of the root marketdata package is also
// what lets marketdata.Manager own and wire it without an import cycle.
type RawInterval string

// The raw interval tokens this reader supports. The daily partition spells
// its token d1 in the file name but tf=d in the schema comment; both
// normalize to RawD1. W1 is not present in the raw corpus and has no token.
const (
	RawM1 RawInterval = "m1"
	RawH1 RawInterval = "h1"
	RawH4 RawInterval = "h4"
	RawD1 RawInterval = "d1"
)

// Meta is the file-level context shared by every Record a Reader yields: the
// resolved instrument, the raw interval, and the calendar month the raw
// partition covers. It mirrors marketdata.BarSet's level split — this
// metadata is identical for every row in a file, so it is carried once here
// rather than repeated on every Record.
type Meta struct {
	// Instrument is the raw partition's instrument, resolved from its
	// provider symbol through instrument's M1 facilities (see resolveSymbol).
	Instrument instrument.ID

	// Interval is the raw partition's interval token (RawM1, RawH1, RawH4, or
	// RawD1).
	Interval RawInterval

	// Year and Month are the calendar month the partition covers, taken from
	// the file path.
	Year  int
	Month time.Month

	// Symbol is the raw provider symbol as written in the archive (for
	// example "EURUSD"). It is diagnostic provenance, never identity —
	// Instrument is identity.
	Symbol string
}

// Record is one provider-native OANDA raw bar exactly as preserved in the
// archive: bid and ask OHLC as exact prices, OANDA's activity/tick count, its
// provider-declared completeness flag, and the provider-observed open time.
// It is the input a later normalization stage turns into a canonical
// marketdata.Bar; Record itself is internal and never crosses the public
// marketdata API.
//
// A Record carries no instrument or interval: those are identical for every
// row in a partition and live on the file's Meta.
type Record struct {
	// Time is the provider-observed opening instant of the bar, in UTC, read
	// verbatim from the row and never reconstructed from row position.
	Time time.Time

	BidOpen  num.Price
	BidHigh  num.Price
	BidLow   num.Price
	BidClose num.Price

	AskOpen  num.Price
	AskHigh  num.Price
	AskLow   num.Price
	AskClose num.Price

	// Volume is OANDA's activity/tick count for the bar. It is not a tradable
	// quantity, so it is a plain int64, not a num.Quantity (ADR-020).
	Volume int64

	// Complete is OANDA's own "this bar had closed" flag. It is preserved as
	// provider-declared metadata; distinguishing it from a calendar-derived
	// gap is a later normalization concern, not this reader's.
	Complete bool
}
