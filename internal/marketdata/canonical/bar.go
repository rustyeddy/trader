package canonical

import (
	"errors"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/num"
)

// Errors, classifiable with errors.Is.
var (
	// ErrInvalidBar reports a bar whose shape is impossible: a zero price, a
	// high or low that does not contain the open and close, a negative volume,
	// or an availability time that is zero or before the bar opened.
	ErrInvalidBar = errors.New("canonical: invalid bar")
	// ErrCrossed reports a bar whose ask is below its bid at some corner (open,
	// high, low or close): a market anomaly, not a shape error.
	ErrCrossed = errors.New("canonical: crossed bid/ask")
)

// OHLC is open, high, low and close on one side of the market, or the midpoint
// of both (Bar.Mid).
type OHLC struct {
	Open, High, Low, Close num.Price
}

// Present reports whether the side carries prices: an OHLC with every price zero
// is a side the source did not give.
func (o OHLC) Present() bool { return o != OHLC{} }

// Bar is one canonical observation: up to three price series for one interval
// (bid, ask and mid), the provider's activity count, when the bar opened and the
// earliest moment a consumer may use it (ADR-090).
//
// Each series is optional: an absent series is the zero OHLC, and at least one
// must be present. A source that quotes both sides gives Bid and Ask and Mid is
// derived from them once, with MidOf; a source that gives a single price writes
// it to the series it is (often Mid) and leaves the others absent.
type Bar struct {
	// Time is the bar's opening instant.
	Time time.Time
	// AvailableAt is the earliest time the completed bar may be consumed. The
	// zero value means availability has not been established, which a stored
	// bar never has. It is modeled under a named policy (PolicyScheduledCloseV1),
	// not an observed provider timestamp.
	AvailableAt time.Time

	Bid OHLC
	Ask OHLC
	// Mid is the midpoint series. When Bid and Ask are both present it is
	// exactly MidOf(Bid, Ask); Validate enforces that.
	Mid OHLC

	// Volume is the provider's activity (tick) count, not a tradable quantity.
	Volume int64
}

// MidOf derives the midpoint series from bid and ask: the exact midpoint at each
// of open, high, low and close. Half a pip is representable, so nothing is
// rounded for prices with up to seven decimals. Open and close are preserved on
// both sides of a bar, so the midpoint is always recomputable from what is stored.
//
// The high of the result is the midpoint of the bid high and the ask high, which
// are the values of the provider's two candles; it need not equal the high of a
// tick-by-tick mid series.
func MidOf(bid, ask OHLC) (OHLC, error) {
	var m OHLC
	for _, c := range []struct {
		dst      *num.Price
		bid, ask num.Price
	}{
		{&m.Open, bid.Open, ask.Open},
		{&m.High, bid.High, ask.High},
		{&m.Low, bid.Low, ask.Low},
		{&m.Close, bid.Close, ask.Close},
	} {
		sum, err := c.bid.Add(c.ask)
		if err != nil {
			return OHLC{}, fmt.Errorf("canonical: mid: %w", err)
		}
		if *c.dst, err = sum.MulRate(half); err != nil {
			return OHLC{}, fmt.Errorf("canonical: mid: %w", err)
		}
	}
	return m, nil
}

var half = num.MustParseRate("0.5")

// IsComplete reports whether availability has been established, as the owner's
// design for the bar defines completeness (#619).
func (b Bar) IsComplete() bool { return !b.AvailableAt.IsZero() }

// IsAvailable reports whether the bar may be consumed at the given time.
func (b Bar) IsAvailable(at time.Time) bool {
	return !b.AvailableAt.IsZero() && !at.Before(b.AvailableAt)
}

// Validate reports ErrInvalidBar or ErrCrossed (wrapped, with what is wrong)
// unless the bar is a possible observation. It does not check alignment to an
// interval or that AvailableAt follows a policy; those depend on the interval
// (see Aligned and AvailableAt).
func (b Bar) Validate() error {
	if b.Time.IsZero() {
		return fmt.Errorf("%w: no opening time", ErrInvalidBar)
	}
	if b.AvailableAt.IsZero() {
		return fmt.Errorf("%w: %s has no availability time", ErrInvalidBar, b.Time.UTC().Format(time.RFC3339))
	}
	if b.AvailableAt.Before(b.Time) {
		return fmt.Errorf("%w: %s becomes available at %s, before it opens", ErrInvalidBar,
			b.Time.UTC().Format(time.RFC3339), b.AvailableAt.UTC().Format(time.RFC3339))
	}
	if b.Volume < 0 {
		return fmt.Errorf("%w: volume %d is negative", ErrInvalidBar, b.Volume)
	}
	if !b.Bid.Present() && !b.Ask.Present() && !b.Mid.Present() {
		return fmt.Errorf("%w: %s has no prices", ErrInvalidBar, b.Time.UTC().Format(time.RFC3339))
	}
	for _, s := range []struct {
		name string
		ohlc OHLC
	}{{"bid", b.Bid}, {"ask", b.Ask}, {"mid", b.Mid}} {
		if !s.ohlc.Present() {
			continue
		}
		if err := s.ohlc.validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidBar, s.name, err)
		}
	}
	if !b.Bid.Present() || !b.Ask.Present() {
		return nil // a single-sided source: nothing to cross or derive
	}
	for _, c := range []struct {
		name     string
		bid, ask num.Price
	}{
		{"open", b.Bid.Open, b.Ask.Open}, {"high", b.Bid.High, b.Ask.High},
		{"low", b.Bid.Low, b.Ask.Low}, {"close", b.Bid.Close, b.Ask.Close},
	} {
		if c.ask.Cmp(c.bid) < 0 {
			return fmt.Errorf("%w: at %s the %s ask %s is below the bid %s", ErrCrossed,
				b.Time.UTC().Format(time.RFC3339), c.name, c.ask, c.bid)
		}
	}
	want, err := MidOf(b.Bid, b.Ask)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBar, err)
	}
	if b.Mid != want {
		return fmt.Errorf("%w: at %s the mid is not the midpoint of the bid and ask", ErrInvalidBar, b.Time.UTC().Format(time.RFC3339))
	}
	return nil
}

// validate checks one side's shape. A price must be positive: num.Price cannot
// be negative (its parsers and arithmetic refuse it), so zero is the only
// non-positive value it can hold.
func (o OHLC) validate() error {
	for _, p := range []struct {
		name  string
		price num.Price
	}{{"open", o.Open}, {"high", o.High}, {"low", o.Low}, {"close", o.Close}} {
		if p.price.IsZero() {
			return fmt.Errorf("%s price is zero", p.name)
		}
	}
	if o.High.Cmp(num.Max(o.Open, o.Close)) < 0 || o.Low.Cmp(num.Min(o.Open, o.Close)) > 0 {
		return fmt.Errorf("high %s and low %s do not contain open %s and close %s", o.High, o.Low, o.Open, o.Close)
	}
	return nil
}
