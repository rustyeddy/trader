package sim

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// Observation is one market observation Broker.Advance evaluates
// pending limit/stop orders against (issue #150/M3-07, ADR-026). It is
// simulator-owned, not marketdata.Bar: Advance needs only a small
// execution observation, not marketdata's full canonical/provenance/
// storage-shaped representation, and keeping this narrow leaves room
// for a future tick-shaped observation without touching marketdata or
// the public broker port. The first implementation happens to be
// OHLC-shaped; the name deliberately does not say "Bar."
type Observation struct {
	// Listing identifies which listing this observation describes.
	// Only pending orders for this exact Listing are evaluated by the
	// Advance call this Observation is passed to.
	Listing instrument.Listing
	Open    num.Price
	High    num.Price
	Low     num.Price
	Close   num.Price
	// Time is this observation's own time. Advance does not use it to
	// timestamp any Event or Snapshot it produces (see Deps.Clock's
	// doc comment) — it exists for validation and for a caller's own
	// bookkeeping.
	Time time.Time
}

// validate reports whether o is well-formed: Listing must be
// constructed, Time must be non-zero, and Low <= Open, Close <= High.
func (o Observation) validate() error {
	if o.Listing.InstrumentID().IsZero() {
		return fmt.Errorf("%w: listing must be constructed", ErrInvalidObservation)
	}
	if o.Time.IsZero() {
		return fmt.Errorf("%w: time must be set", ErrInvalidObservation)
	}
	if o.High.Cmp(o.Low) < 0 {
		return fmt.Errorf("%w: high %s is below low %s", ErrInvalidObservation, o.High, o.Low)
	}
	if o.Open.Cmp(o.Low) < 0 || o.Open.Cmp(o.High) > 0 {
		return fmt.Errorf("%w: open %s is outside [low %s, high %s]", ErrInvalidObservation, o.Open, o.Low, o.High)
	}
	if o.Close.Cmp(o.Low) < 0 || o.Close.Cmp(o.High) > 0 {
		return fmt.Errorf("%w: close %s is outside [low %s, high %s]", ErrInvalidObservation, o.Close, o.Low, o.High)
	}
	return nil
}
