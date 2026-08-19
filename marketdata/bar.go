package marketdata

import (
	"errors"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/num"
)

// PriceBasis records what a BarSet's OHLC prices represent. FX providers
// quote a bid and an ask; a canonical BarSet records one basis for all its
// bars so a consumer is never left guessing whether Close is a bid, an
// ask, or a mid. M2's canonical FX bars use BasisBid (see ADR-020).
type PriceBasis uint8

const (
	// BasisUnknown is PriceBasis's zero value. Reserving zero for an
	// unset basis means an uninitialized BarSet can never be silently
	// read as bid-priced data.
	BasisUnknown PriceBasis = iota
	// BasisBid means OHLC are bid prices. This is M2's canonical FX
	// basis.
	BasisBid
	// BasisMid means OHLC are mid prices (bid plus half the spread).
	// Not persisted by M2; reserved so a future derived series can label
	// itself honestly.
	BasisMid
	// BasisAsk means OHLC are ask prices.
	BasisAsk
)

// String returns a human-readable PriceBasis name.
func (b PriceBasis) String() string {
	switch b {
	case BasisUnknown:
		return "unknown"
	case BasisBid:
		return "bid"
	case BasisMid:
		return "mid"
	case BasisAsk:
		return "ask"
	default:
		return fmt.Sprintf("PriceBasis(%d)", uint8(b))
	}
}

// valid reports whether b names a defined, usable basis (not the zero
// value).
func (b PriceBasis) valid() bool {
	switch b {
	case BasisBid, BasisMid, BasisAsk:
		return true
	default:
		return false
	}
}

// Sentinel errors returned (wrapped) by Bar.Validate, so callers can
// classify a validation failure without parsing its message.
var (
	// ErrBarTime marks a Bar whose Time is the zero value. A canonical
	// Bar's Time is its authoritative observed open; a zero Time means
	// the observation was never anchored, which is exactly the
	// index-reconstruction mistake ADR-020 and legacy #179 guard against.
	ErrBarTime = errors.New("marketdata: bar time is zero")
	// ErrBarOHLC marks a Bar whose open, high, low, and close do not form
	// a valid candle (high below low, or open/close outside [low, high]).
	ErrBarOHLC = errors.New("marketdata: bar OHLC out of order")
	// ErrBarSpread marks a Bar whose average spread exceeds its maximum
	// spread. Negative prices and spreads are not checked here: num.Price
	// is non-negative by construction (ParsePrice, Sub, and MulRate all
	// reject negatives), so a negative value cannot reach this type.
	ErrBarSpread = errors.New("marketdata: bar spread invalid")
	// ErrBarTicks marks a Bar with a negative tick count.
	ErrBarTicks = errors.New("marketdata: bar ticks negative")
)

// Bar is one canonical, observed FX bar for a single instrument and
// interval. It is the per-observation half of the market-data model; the
// homogeneous metadata that applies to a whole range of bars (instrument,
// interval, span, price basis) lives on BarSet, not repeated on every Bar.
//
// Bar is a plain record with exported fields, validated at the
// normalization/store boundary via Validate rather than through a
// constructor. Its zero value is not a usable bar (Validate reports
// ErrBarTime), which is deliberate: canonical storage never persists a
// dummy zero-filled bar to stand in for a closed or missing interval, so
// a Bar that exists always represents an observation that happened. A
// missing interval is an absent Bar, described by coverage, not a zero row.
//
// OHLC are bid-basis in M2; BarSet.Basis records the basis for a whole
// collection. AvgSpread and MaxSpread are the mean and maximum of
// (ask - bid) taken at the open, high, low, and close — the only spread
// summary reconstructible from OANDA's bid/ask OHLC, since no per-tick
// spread stream is preserved. What that spread means for a simulated fill
// is deferred to M5.
//
// Ticks is OANDA's per-bar tick count (its "volume" column, which is a
// tick count, not traded volume). It is a plain int64, not num.Quantity:
// a tick count is a dimensionless count, not an exact tradable quantity,
// and ADR-004 defines no count type.
type Bar struct {
	// Time is the bar's authoritative observed and session-aligned open,
	// stored verbatim. It is never reconstructed from array position or
	// fixed-duration arithmetic; doing so is what silently mislabeled
	// bars across DST transitions in the legacy implementation (#179).
	Time      time.Time
	Open      num.Price
	High      num.Price
	Low       num.Price
	Close     num.Price
	AvgSpread num.Price
	MaxSpread num.Price
	Ticks     int64
}

// Validate reports whether b is a well-formed canonical bar, returning a
// wrapped sentinel error for the first invariant it violates and nil when
// b is valid. It is intended to run at the boundary where provider data is
// normalized into canonical bars and where stored bars are read back.
func (b Bar) Validate() error {
	if b.Time.IsZero() {
		return fmt.Errorf("marketdata: bar validate: %w", ErrBarTime)
	}
	// OHLC shape. High == Low is permitted: a flat/doji bar is normal in
	// a thin-market minute.
	if b.High.Cmp(b.Low) < 0 {
		return fmt.Errorf("marketdata: bar validate: high %s below low %s: %w", b.High, b.Low, ErrBarOHLC)
	}
	if b.Open.Cmp(b.Low) < 0 || b.Open.Cmp(b.High) > 0 {
		return fmt.Errorf("marketdata: bar validate: open %s outside [low %s, high %s]: %w", b.Open, b.Low, b.High, ErrBarOHLC)
	}
	if b.Close.Cmp(b.Low) < 0 || b.Close.Cmp(b.High) > 0 {
		return fmt.Errorf("marketdata: bar validate: close %s outside [low %s, high %s]: %w", b.Close, b.Low, b.High, ErrBarOHLC)
	}
	if b.AvgSpread.Cmp(b.MaxSpread) > 0 {
		return fmt.Errorf("marketdata: bar validate: avg spread %s exceeds max spread %s: %w", b.AvgSpread, b.MaxSpread, ErrBarSpread)
	}
	if b.Ticks < 0 {
		return fmt.Errorf("marketdata: bar validate: ticks %d is negative: %w", b.Ticks, ErrBarTicks)
	}
	return nil
}

// halfRate is 0.5, parsed once at package init rather than on every Mid
// call (issue #85): MustParseRate's cost is only ever paid once for this
// fixed, programmer-controlled constant.
var halfRate = num.MustParseRate("0.5")

// Mid returns the mid close price, derived as Close plus half of AvgSpread.
// It assumes bid-basis OHLC (M2's canonical basis); the per-corner ask is
// not retained, so this is the mid of the close specifically, computed
// from the average spread rather than an exact close-time spread. Callers
// needing a different basis or an exact spread must work from raw data.
func (b Bar) Mid() (num.Price, error) {
	half, err := b.AvgSpread.MulRate(halfRate)
	if err != nil {
		return num.Price{}, fmt.Errorf("marketdata: bar mid: halving avg spread: %w", err)
	}
	mid, err := b.Close.Add(half)
	if err != nil {
		return num.Price{}, fmt.Errorf("marketdata: bar mid: %w", err)
	}
	return mid, nil
}

// Range returns b's half-open [start, end) trading range for interval,
// resolved through cal from b's observed open Time. Because b stores the
// aligned open verbatim, the range is derived rather than stored, and DST
// transitions resolve correctly through the calendar rather than through
// fixed-duration arithmetic.
func (b Bar) Range(interval Interval, cal Calendar) (TimeRange, error) {
	if cal == nil {
		return TimeRange{}, fmt.Errorf("marketdata: bar range: nil calendar")
	}
	return cal.Bar(b.Time, interval)
}
