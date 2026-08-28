package backtest

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/strategy"
)

// ErrDuplicateRequirement reports that NewReplay was given two
// DataRequirements naming the same (Instrument, Interval) pair.
// Allowing duplicates would make the canonical (timestamp, instrument,
// interval) merge order ambiguous for that pair, so NewReplay rejects
// it outright rather than silently picking one requirement's copy over
// the other's.
var ErrDuplicateRequirement = errors.New("backtest: duplicate data requirement")

// FailedRequirement pairs one DataRequirement that NewReplay could not
// open with the error marketdata.Manager.Bars itself returned for it.
type FailedRequirement struct {
	Requirement strategy.DataRequirement
	Err         error
}

// CoverageError reports that one or more DataRequirements could not be
// satisfied over Replay's requested span because Manager.Bars reported
// marketdata.ErrDataUnavailable for them. It carries every such failing
// requirement NewReplay found, not merely the first, so a caller (and
// a future run manifest) can see the complete picture in one pass
// rather than discovering failures one NewReplay retry at a time.
//
// Every contained FailedRequirement.Err is guaranteed to wrap
// marketdata.ErrDataUnavailable — see NewReplay's classification rule.
// Any other kind of Bars failure (invalid query, listing-resolution
// failure, marketdata.ErrInconsistentData, and so on) aborts NewReplay
// immediately instead of being accumulated here, so a *CoverageError
// never blurs "data not available" together with a configuration or
// data-integrity problem (issue #212 review).
//
// CoverageError implements Unwrap() []error over its contained causes,
// so errors.Is(err, marketdata.ErrDataUnavailable) and similar checks
// against a *CoverageError use Go's normal multi-error traversal rather
// than a hand-rolled, unconditional match.
type CoverageError struct {
	Failures []FailedRequirement
}

// Error summarizes every failing requirement.
func (e *CoverageError) Error() string {
	if len(e.Failures) == 1 {
		f := e.Failures[0]
		return fmt.Sprintf("backtest: replay: %s %s: %s",
			f.Requirement.Instrument, f.Requirement.Interval, f.Err)
	}
	return fmt.Sprintf("backtest: replay: coverage unavailable for %d requirement(s)", len(e.Failures))
}

// Unwrap returns every contained failure's error, letting errors.Is and
// errors.As traverse into the real causes instead of a manufactured
// sentinel.
func (e *CoverageError) Unwrap() []error {
	errs := make([]error, len(e.Failures))
	for i, f := range e.Failures {
		errs[i] = f.Err
	}
	return errs
}

// Replay merges the canonical bar history for a set of
// strategy.DataRequirements into one deterministic, chronologically
// ordered stream over one requested span. It reads only already-
// published canonical data through marketdata.Manager.Bars — never a
// provider or the network — so a reproducible backtest never silently
// changes its own input set (issue #212, M5-04, ADR-035).
//
// Replay ignores DataRequirement.WarmupBars entirely: Next ever
// returns bars from exactly [span.Start, span.End), never silently
// widened to cover warm-up history. Deciding how much additional
// history a strategy needs before its first real decision is the
// scheduler/runner's concern (#213/#214), not Replay's.
//
// Replay is not safe for concurrent use.
type Replay struct {
	streams []*replayStream
	done    bool
	closed  bool
}

type replayStream struct {
	req    strategy.DataRequirement
	reader *marketdata.BarReader
	peeked *marketdata.Bar
	eof    bool
}

type requirementKey struct {
	instrument instrument.ID
	interval   marketdata.Interval
}

// NewReplay validates requirements and opens one marketdata.BarReader
// per requirement, over span, via manager.Bars — the same call Replay
// itself later drains through Next.
//
// Requirements naming the same (Instrument, Interval) pair are
// rejected with a wrapped ErrDuplicateRequirement before any reader is
// opened.
//
// NewReplay uses manager.Bars itself, rather than manager.Coverage, as
// the authority for whether a requirement is replayable. Bars' own
// contract is strictly canonical-store-only: it proves complete
// coverage of the requested range from persisted canonical partition
// Manifest.Span data alone and returns a wrapped
// marketdata.ErrDataUnavailable with no reader if it cannot. Coverage,
// by contrast, additionally inspects the raw provider archive (it
// requires marketdata.Config.RawRoot and classifies a canonical
// partition "stale" against the raw archive's current fingerprint) —
// signals a data-maintenance question, not "is this persisted
// canonical revision complete and replayable," and would make replay
// depend on raw-archive state outside the canonical input actually
// being replayed. See issue #212's review.
//
// NewReplay opens every requirement's reader before deciding success
// or failure, so a *CoverageError names every requirement that failed
// with marketdata.ErrDataUnavailable — never only the first. Any
// requirement that fails with a different kind of error (an invalid
// query, a listing-resolution failure, marketdata.ErrInconsistentData,
// and so on) aborts NewReplay immediately with that error, unwrapped —
// such a failure is a configuration or data-integrity problem, not a
// "coverage unavailable" condition, and must not be blurred together
// with one (issue #212 review). Either way, every reader already
// successfully opened is closed before NewReplay returns an error, so
// a failed call never leaks an open reader.
func NewReplay(ctx context.Context, manager *marketdata.Manager, requirements []strategy.DataRequirement, span marketdata.TimeRange) (*Replay, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	seen := make(map[requirementKey]struct{}, len(requirements))
	for _, req := range requirements {
		key := requirementKey{instrument: req.Instrument, interval: req.Interval}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("%w: %s %s", ErrDuplicateRequirement, req.Instrument, req.Interval)
		}
		seen[key] = struct{}{}
	}

	streams := make([]*replayStream, 0, len(requirements))
	var failures []FailedRequirement
	for _, req := range requirements {
		reader, err := manager.Bars(ctx, marketdata.BarQuery{
			Instrument: req.Instrument,
			Interval:   req.Interval,
			Range:      span,
		})
		if err != nil {
			if !errors.Is(err, marketdata.ErrDataUnavailable) {
				for _, s := range streams {
					s.reader.Close()
				}
				return nil, fmt.Errorf("backtest: replay: %s %s: %w", req.Instrument, req.Interval, err)
			}
			failures = append(failures, FailedRequirement{Requirement: req, Err: err})
			continue
		}
		streams = append(streams, &replayStream{req: req, reader: reader})
	}

	if len(failures) > 0 {
		for _, s := range streams {
			s.reader.Close()
		}
		return nil, &CoverageError{Failures: failures}
	}

	return &Replay{streams: streams}, nil
}

// Next returns the next bar event in the merged stream, in canonical
// (bar timestamp, instrument ID, interval) order. This order is
// intrinsic to the data itself, never the order requirements were
// supplied to NewReplay in, so constructing the same requirement set
// in a different input order can never change replay order or, in
// turn, backtest results.
//
// Next returns io.EOF once every underlying stream is exhausted, and
// continues returning io.EOF on every subsequent call — it never
// exposes internal reader state once done.
func (r *Replay) Next(ctx context.Context) (strategy.BarEvent, error) {
	if err := ctx.Err(); err != nil {
		return strategy.BarEvent{}, err
	}
	if r.done {
		return strategy.BarEvent{}, io.EOF
	}

	for _, s := range r.streams {
		if err := s.fill(ctx); err != nil {
			return strategy.BarEvent{}, err
		}
	}

	best := -1
	for i, s := range r.streams {
		if s.peeked == nil {
			continue
		}
		if best == -1 || lessStream(s, r.streams[best]) {
			best = i
		}
	}
	if best == -1 {
		r.done = true
		return strategy.BarEvent{}, io.EOF
	}

	s := r.streams[best]
	bar := *s.peeked
	s.peeked = nil
	return strategy.BarEvent{Instrument: s.req.Instrument, Interval: s.req.Interval, Bar: bar}, nil
}

// fill ensures s has a peeked bar buffered, unless s is already
// exhausted. It leaves an already-buffered peek untouched.
func (s *replayStream) fill(ctx context.Context) error {
	if s.peeked != nil || s.eof {
		return nil
	}
	b, err := s.reader.Next(ctx)
	if err != nil {
		if errors.Is(err, io.EOF) {
			s.eof = true
			return nil
		}
		return err
	}
	s.peeked = &b
	return nil
}

// lessStream reports whether a's peeked bar sorts before b's, under the
// canonical (timestamp, instrument ID, interval) tie-break. Both a and
// b must have a non-nil peeked bar. The interval component compares
// Unit() then Count() — Interval's intrinsic fields — never String(),
// which is documented as display-only and carries no stability
// obligation (issue #212 review).
func lessStream(a, b *replayStream) bool {
	if !a.peeked.Time.Equal(b.peeked.Time) {
		return a.peeked.Time.Before(b.peeked.Time)
	}
	if ai, bi := a.req.Instrument.String(), b.req.Instrument.String(); ai != bi {
		return ai < bi
	}
	if au, bu := a.req.Interval.Unit(), b.req.Interval.Unit(); au != bu {
		return au < bu
	}
	return a.req.Interval.Count() < b.req.Interval.Count()
}

// Close releases every reader Replay opened. It is idempotent: a
// second and later call is a no-op returning nil. After Close, Next
// always returns io.EOF.
func (r *Replay) Close() error {
	r.done = true
	if r.closed {
		return nil
	}
	r.closed = true

	var errs []error
	for _, s := range r.streams {
		if err := s.reader.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
