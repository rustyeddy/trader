package marketdata

import (
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
)

// ErrInvalidRequest marks a request whose DatasetRequest is missing a
// required field or describes a malformed range. It is never returned
// alongside a partial result.
var ErrInvalidRequest = errors.New("service/marketdata: invalid request")

// DatasetRequest identifies the canonical dataset an operation acts on:
// an instrument, an interval, and a half-open time range. It is the
// common shape every M2.5 MarketData use case (Bars, Coverage, Plan,
// Sync, Build, Update; issues #105-#107) embeds, mirroring
// marketdata.BarQuery's fields without depending on how a transport
// adapter obtained them — a request is built from already-parsed
// transport input (never a raw string interval or an unparsed date),
// but parsing is as far as the transport adapter's responsibility
// goes. Domain semantic validity — is this instrument known, is this
// range actually well-formed — is Validate's and the underlying domain
// constructors' responsibility, not something a transport adapter is
// expected to re-check first.
type DatasetRequest struct {
	// Instrument is the canonical instrument identity to act on. Required.
	Instrument instrument.ID
	// Interval is the canonical bar interval to act on. Required.
	Interval marketdata.Interval
	// Range is the half-open [Start, End) time span to act on. Required.
	Range marketdata.TimeRange
}

// Validate reports whether r is well-formed enough to attempt,
// returning a wrapped ErrInvalidRequest for the first problem found.
func (r DatasetRequest) Validate() error {
	if r.Instrument.IsZero() {
		return fmt.Errorf("%w: instrument is zero", ErrInvalidRequest)
	}
	if !r.Interval.Valid() {
		return fmt.Errorf("%w: interval is invalid", ErrInvalidRequest)
	}
	if r.Range.Start().IsZero() || r.Range.End().IsZero() || !r.Range.End().After(r.Range.Start()) {
		return fmt.Errorf("%w: range is invalid", ErrInvalidRequest)
	}
	return nil
}

// query translates r into the marketdata.BarQuery every read operation
// (Bars, Coverage, Plan) ultimately calls its corresponding
// *marketdata.Manager method with. It is the one place DatasetRequest's
// fields are mapped onto BarQuery's, so every operation's translation
// stays identical by construction rather than by convention.
func (r DatasetRequest) query() marketdata.BarQuery {
	return marketdata.BarQuery{
		Instrument: r.Instrument,
		Interval:   r.Interval,
		Range:      r.Range,
	}
}

// BarsRequest is the request for the read-only Bars use case (issue
// #105): the canonical Bar data for one dataset.
type BarsRequest struct {
	DatasetRequest
}

// CoverageRequest is the request for the read-only Coverage use case
// (issue #105): coverage and gap reporting for one dataset.
type CoverageRequest struct {
	DatasetRequest
}

// PlanRequest is the request for the read-only Plan use case (issue
// #105): the acquisition/build work required to make one dataset
// available. Plan is read-only itself — it never performs the work it
// describes; see Sync and Build (issue #106) for the corresponding
// mutating operations.
type PlanRequest struct {
	DatasetRequest
}
