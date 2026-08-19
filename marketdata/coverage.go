package marketdata

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata/internal/provider/oanda"
)

// PartitionCoverageStatus classifies one canonical partition file's own
// standing — whether it exists at all, can be trusted, and is current —
// orthogonal to IntervalState (#74), which classifies individual bar
// intervals within a partition that exists and can be trusted.
type PartitionCoverageStatus uint8

const (
	// PartitionCoverageUnknown is PartitionCoverageStatus's zero value;
	// Coverage never leaves a PartitionCoverage with this status.
	PartitionCoverageUnknown PartitionCoverageStatus = iota
	// PartitionCoverageMissing means no canonical partition has been
	// published for this month at all.
	PartitionCoverageMissing
	// PartitionCoverageInvalid means a canonical partition file exists
	// for this month but failed to load or validate (a corrupted or
	// hand-edited file, or a store/manifest invariant violation).
	PartitionCoverageInvalid
	// PartitionCoverageStale means the canonical partition loads and
	// validates, but disagrees with its current input: its
	// Manifest.RawFingerprint no longer matches the current raw
	// partition's fingerprint (raw-built intervals), or its
	// Manifest.Parent.Revision no longer matches the underlying D1
	// partition's current Revision (the derived W1 interval).
	PartitionCoverageStale
	// PartitionCoverageCurrent means the canonical partition loads,
	// validates, and agrees with its current input. This says nothing
	// about whether every calendar-open bar interval within the month
	// is actually Present — see Coverage.Gaps for that.
	PartitionCoverageCurrent
)

// String returns a human-readable PartitionCoverageStatus name.
func (s PartitionCoverageStatus) String() string {
	switch s {
	case PartitionCoverageUnknown:
		return "unknown"
	case PartitionCoverageMissing:
		return "missing"
	case PartitionCoverageInvalid:
		return "invalid"
	case PartitionCoverageStale:
		return "stale"
	case PartitionCoverageCurrent:
		return "current"
	default:
		return fmt.Sprintf("PartitionCoverageStatus(%d)", uint8(s))
	}
}

// PartitionCoverage is the coverage analysis for one UTC calendar month
// of one (instrument, interval).
type PartitionCoverage struct {
	Year   int
	Month  time.Month
	Status PartitionCoverageStatus
	// Manifest is non-nil only when Status is PartitionCoverageStale or
	// PartitionCoverageCurrent — the two statuses that mean the
	// partition actually loaded. It is an independent clone
	// (cloneManifest): mutating it never affects Manager's cache or a
	// later call.
	Manifest *Manifest
	// RawIncompleteCount is the number of raw records in the
	// corresponding raw partition whose provider "complete" flag was
	// false (oanda.Partition.IncompleteCount). It is always 0 for the
	// derived W1 interval, which has no raw partition of its own, and
	// for a month with no raw partition on disk at all.
	//
	// This is the only currently-producible "provider-incomplete"
	// signal: marketdata.Bar carries no completeness flag (#72/#73), so
	// that information does not survive into published canonical data.
	// IntervalStateIncomplete therefore never appears in Coverage.Gaps
	// today — see the package doc comment.
	RawIncompleteCount int
}

// Gap is one maximal contiguous run of bar intervals within a queried
// range that are neither calendar-closed nor present: Missing,
// Incomplete, InProgress, or Unexpected (see IntervalState, #74). A
// closed market is not a Gap; that is the entire point of
// IntervalStateClosed.
type Gap struct {
	State IntervalState
	Span  TimeRange
}

// Coverage is Manager's read-only analysis of what canonical data exists
// for one instrument/interval/range, and why any of it is absent
// (issue #79, ADR-020). It performs no write or network side effects:
// Coverage only reads the raw archive (oanda.Inspect) and the canonical
// store (through Manager's own cache), exactly like Bars.
type Coverage struct {
	Instrument instrument.ID
	Interval   Interval
	Range      TimeRange
	// Partitions holds one entry per UTC calendar month Range touches,
	// in chronological order.
	Partitions []PartitionCoverage
	// Gaps holds every Gap found within a PartitionCoverageCurrent
	// month, across every touched month, in chronological order. A
	// Missing, Invalid, or Stale partition contributes no Gaps of its
	// own: its PartitionCoverage.Status already says everything Gaps
	// would say less precisely.
	Gaps []Gap
}

// rawPartitionKey indexes a raw-archive inventory lookup by exactly the
// fields oanda.Partition and marketdata's own partitionKey share.
type rawPartitionKey struct {
	symbol   string
	interval oanda.RawInterval
	year     int
	month    time.Month
}

// intervalToRawInterval maps a canonical Interval to the raw partition
// interval token it is built directly from. It reports ok=false for W1,
// which has no raw-native partition (ADR-012: W1 is derived from
// canonical D1), and for any other interval this package does not
// support.
func intervalToRawInterval(i Interval) (oanda.RawInterval, bool) {
	switch i {
	case M1:
		return oanda.RawM1, true
	case H1:
		return oanda.RawH1, true
	case H4:
		return oanda.RawH4, true
	case D1:
		return oanda.RawD1, true
	default:
		return "", false
	}
}

// rawInventoryLookup returns a lookup map of every raw partition
// Inspect finds under m.rawRoot, keyed for direct use by Coverage/Plan.
// It returns a nil map with no error for W1, since raw inspection does
// not apply to a derived interval, and a wrapped ErrInvalidConfig if
// m.rawRoot is empty for any interval that does need it.
func (m *Manager) rawInventoryLookup(ctx context.Context, interval Interval) (map[rawPartitionKey]oanda.Partition, error) {
	if _, ok := intervalToRawInterval(interval); !ok {
		return nil, nil
	}
	if m.rawRoot == "" {
		return nil, fmt.Errorf("%w: raw root is not configured", ErrInvalidConfig)
	}
	inv, err := oanda.Inspect(ctx, m.rawRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect raw archive: %w", err)
	}
	lookup := make(map[rawPartitionKey]oanda.Partition, len(inv.Partitions))
	for _, p := range inv.Partitions {
		lookup[rawPartitionKey{p.Symbol, p.Interval, p.Year, p.Month}] = p
	}
	return lookup, nil
}

// Coverage analyzes query against the raw archive and canonical store,
// returning a deterministic report of what exists, what can be trusted,
// and what is missing — without downloading, building, or otherwise
// changing anything. See Plan for the accompanying description of what
// work would be required to close the gaps Coverage finds.
func (m *Manager) Coverage(ctx context.Context, query BarQuery) (Coverage, error) {
	if !m.configured() {
		return Coverage{}, fmt.Errorf("marketdata: coverage: %w: manager is not configured", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return Coverage{}, err
	}
	if err := query.validate(); err != nil {
		return Coverage{}, fmt.Errorf("marketdata: coverage: %w", err)
	}

	listing, err := m.resolver.ResolveInstrument(query.Instrument, m.providerName, "")
	if err != nil {
		return Coverage{}, fmt.Errorf("marketdata: coverage: resolve listing: %w", err)
	}

	rawByKey, err := m.rawInventoryLookup(ctx, query.Interval)
	if err != nil {
		return Coverage{}, fmt.Errorf("marketdata: coverage: %w", err)
	}

	return m.coverage(ctx, query, listing.Symbol(), rawByKey)
}

// coverage is Coverage's implementation, factored out so Plan can reuse
// it with a raw inventory lookup it already computed once, rather than
// walking the raw archive a second time.
func (m *Manager) coverage(ctx context.Context, query BarQuery, symbol string, rawByKey map[rawPartitionKey]oanda.Partition) (Coverage, error) {
	rawInterval, rawApplicable := intervalToRawInterval(query.Interval)
	keys := monthPartitionKeys(m.providerName, symbol, query.Instrument, query.Interval, query.Range)
	cov := Coverage{Instrument: query.Instrument, Interval: query.Interval, Range: query.Range}

	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return Coverage{}, err
		}

		man, bs, loadErr := m.loadPartition(ctx, key)
		pc := PartitionCoverage{Year: key.year, Month: key.month}

		switch {
		case loadErr == nil:
			stale, err := m.isStale(ctx, key, man, symbol, rawByKey, rawInterval, rawApplicable)
			if err != nil {
				return Coverage{}, err
			}
			if stale {
				pc.Status = PartitionCoverageStale
			} else {
				pc.Status = PartitionCoverageCurrent
			}
			mc := cloneManifest(man)
			pc.Manifest = &mc
		case errors.Is(loadErr, os.ErrNotExist):
			pc.Status = PartitionCoverageMissing
		default:
			if ctxErr := ctx.Err(); ctxErr != nil {
				return Coverage{}, ctxErr
			}
			pc.Status = PartitionCoverageInvalid
		}

		if rawApplicable {
			if p, found := rawByKey[rawPartitionKey{symbol, rawInterval, key.year, key.month}]; found {
				pc.RawIncompleteCount = p.IncompleteCount
			}
		}

		cov.Partitions = append(cov.Partitions, pc)

		if pc.Status == PartitionCoverageCurrent {
			gaps, err := m.gapsForPartition(key, bs, query.Range)
			if err != nil {
				return Coverage{}, err
			}
			cov.Gaps = append(cov.Gaps, gaps...)
		}
	}

	return cov, nil
}

// isStale reports whether the canonical partition man was loaded from
// under key disagrees with its current input. See PartitionCoverageStale
// for the two mechanisms (raw fingerprint / parent revision). A raw or
// parent partition that itself cannot be found or trusted is not, by
// itself, grounds to call the child stale: that condition is already
// surfaced through the raw partition's own status (RawIncompleteCount
// and, for the raw-built case, a Plan action) or, for the derived case,
// through the child's own D1-completeness gating in Plan.
func (m *Manager) isStale(ctx context.Context, key partitionKey, man Manifest, symbol string, rawByKey map[rawPartitionKey]oanda.Partition, rawInterval oanda.RawInterval, rawApplicable bool) (bool, error) {
	if key.interval == W1 {
		parentKey := key
		parentKey.interval = D1
		parentMan, _, err := m.loadPartition(ctx, parentKey)
		if err != nil {
			return false, nil
		}
		if man.Parent == nil {
			// Unreachable in practice: Manifest.Validate rejects a
			// W1-shaped (ResamplerVersion != "none") manifest with a
			// nil Parent, and load validates before returning. Handled
			// defensively rather than assumed.
			return false, nil
		}

		// man's own Parent.Revision may be a composite over two D1
		// manifests, not just this same-month one — deriveAndPublish
		// records a composite whenever man's final published bar's
		// week extends past its own Span's month boundary
		// (w1SpansNextMonth). Recomputing that same rule here, from
		// man's own stored LastBar/Span, is what lets this check agree
		// with deriveAndPublish about which case applies; see
		// deriveAndPublish's own doc comment for why the two share one
		// function instead of encoding the rule twice.
		spansNext, err := w1SpansNextMonth(m.calendar, man.LastBar, man.Span.End())
		if err != nil {
			return false, err
		}
		contributing := []Manifest{parentMan}
		if spansNext {
			nextStart := man.Span.End()
			nextKey := parentKey
			nextKey.year = nextStart.Year()
			nextKey.month = nextStart.Month()
			nextMan, _, err := m.loadPartition(ctx, nextKey)
			if err != nil {
				// The spillover D1 partition this W1 partition was
				// actually built from can no longer be found or
				// trusted. Matching the same-month case just above,
				// that is not, by itself, grounds to call the child
				// stale — a missing/invalid parent is surfaced through
				// the parent's own status, not by this check.
				return false, nil
			}
			contributing = append(contributing, nextMan)
		}
		revision, _ := combineParentLineage(contributing)
		return man.Parent.Revision != revision, nil
	}

	if !rawApplicable {
		return false, nil
	}
	p, found := rawByKey[rawPartitionKey{symbol, rawInterval, key.year, key.month}]
	if !found || p.Status != oanda.PartitionStatusOK {
		return false, nil
	}
	return man.RawFingerprint != p.Fingerprint, nil
}

// gapAccumulator merges consecutive same-IntervalState, contiguous
// boundary spans into one Gap, shared by gapsForPartition's two
// classification strategies (per-boundary ClassifyInterval for M1/H1/
// H4/D1, and the separate week-level walk for W1 — see gapsForPartition).
type gapAccumulator struct {
	gaps []Gap
	open *Gap
}

// add records span's classified state. A Present or Closed span closes
// any run in progress and contributes no Gap of its own. Any other
// state either extends the open run (when it is the same state and
// exactly contiguous with it) or starts a new one.
func (a *gapAccumulator) add(state IntervalState, span TimeRange) error {
	if state == IntervalStatePresent || state == IntervalStateClosed {
		a.flush()
		return nil
	}
	if a.open != nil && a.open.State == state && a.open.Span.End().Equal(span.Start()) {
		merged, err := NewTimeRange(a.open.Span.Start(), span.End())
		if err != nil {
			return err
		}
		a.open.Span = merged
		return nil
	}
	a.flush()
	g := Gap{State: state, Span: span}
	a.open = &g
	return nil
}

// flush closes any open run, appending it to gaps.
func (a *gapAccumulator) flush() {
	if a.open != nil {
		a.gaps = append(a.gaps, *a.open)
		a.open = nil
	}
}

// firstBoundaryAtOrAfter returns the start of the first interval
// boundary at or after at. cal.Bar(at, interval) can return a boundary
// that started strictly before at, when at itself does not fall exactly
// on a boundary edge (interval is not UTC-clock-aligned — D1/W1 — or at
// is simply mid-boundary); using that boundary's own Start() as a walk's
// starting cursor would then produce a Gap whose Span begins before the
// range actually being asked about, and — for a multi-month query —
// would make the following month's walk reprocess (and double-report)
// the same boundary a previous month's walk already covered. Advancing
// to the boundary's End() in that case is what fixes both: a design
// review caught the first symptom; tracing it further surfaced the
// second, more important one.
func firstBoundaryAtOrAfter(cal Calendar, at time.Time, interval Interval) (time.Time, error) {
	span, err := cal.Bar(at, interval)
	if err != nil {
		return time.Time{}, err
	}
	if span.Start().Before(at) {
		return span.End(), nil
	}
	return span.Start(), nil
}

// gapsForPartition walks every expected bar boundary within key's UTC
// calendar month, clipped to queryRange, and returns the merged Gaps
// found.
//
// W1 is handled separately from every raw-built interval (M1/H1/H4/D1):
// FXCalendar.Bar's W1 span is a full seven-day week
// [Sunday 17:00, next Sunday 17:00) that necessarily contains the
// Friday-to-Sunday closure within it — it is not, contrary to an
// earlier ADR-020 assumption written during #74, uniformly one Calendar
// Status throughout. ClassifyInterval's uniformStatus check therefore
// always rejects a genuine W1 span with ErrIntervalStraddlesBoundary,
// and reusing it here would make Coverage error on every W1 query.
// gapsForWeeklyPartition instead classifies each week directly as
// InProgress, Present, or Missing — a week is never itself
// "calendar-closed" the way a single day or hour can be, so that branch
// of IntervalState simply does not apply at the week level; whether the
// days inside a given week were open is a D1-level question Plan's own
// D1-completeness gating already answers before scheduling a W1 derive.
func (m *Manager) gapsForPartition(key partitionKey, bs BarSet, queryRange TimeRange) ([]Gap, error) {
	monthStart := time.Date(key.year, key.month, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	clipStart, clipEnd := monthStart, monthEnd
	if queryRange.Start().After(clipStart) {
		clipStart = queryRange.Start()
	}
	if queryRange.End().Before(clipEnd) {
		clipEnd = queryRange.End()
	}
	if !clipEnd.After(clipStart) {
		return nil, nil
	}

	// Keyed by UnixNano, not time.Time itself: a loaded Bar.Time is
	// always UTC-located (parseBarRow calls .UTC()), but Calendar.Bar's
	// returned boundary is not — FXCalendar's D1/W1 arithmetic returns a
	// New York-located time.Time. Both represent the same instant, but
	// time.Time equality (as used by a map key) is location-sensitive,
	// not instant-sensitive; UnixNano collapses that difference away.
	present := make(map[int64]bool, len(bs.Bars))
	for _, b := range bs.Bars {
		present[b.Time.UTC().UnixNano()] = true
	}

	if key.interval == W1 {
		return m.gapsForWeeklyPartition(present, clipStart, clipEnd)
	}

	cursor, err := firstBoundaryAtOrAfter(m.calendar, clipStart, key.interval)
	if err != nil {
		return nil, fmt.Errorf("marketdata: coverage: %w", err)
	}
	now := m.clock.Now()

	acc := &gapAccumulator{}
	for cursor.Before(clipEnd) {
		span, err := m.calendar.Bar(cursor, key.interval)
		if err != nil {
			return nil, fmt.Errorf("marketdata: coverage: %w", err)
		}
		state, err := ClassifyInterval(m.calendar, span, now, present[span.Start().UTC().UnixNano()], true)
		if err != nil {
			return nil, fmt.Errorf("marketdata: coverage: %w", err)
		}
		if err := acc.add(state, span); err != nil {
			return nil, fmt.Errorf("marketdata: coverage: %w", err)
		}
		cursor = span.End()
	}
	acc.flush()
	return acc.gaps, nil
}

// gapsForWeeklyPartition classifies each W1 week boundary in
// [clipStart, clipEnd) directly — see gapsForPartition's doc comment for
// why it cannot reuse ClassifyInterval.
func (m *Manager) gapsForWeeklyPartition(present map[int64]bool, clipStart, clipEnd time.Time) ([]Gap, error) {
	cursor, err := firstBoundaryAtOrAfter(m.calendar, clipStart, W1)
	if err != nil {
		return nil, fmt.Errorf("marketdata: coverage: %w", err)
	}
	now := m.clock.Now()

	acc := &gapAccumulator{}
	for cursor.Before(clipEnd) {
		span, err := m.calendar.Bar(cursor, W1)
		if err != nil {
			return nil, fmt.Errorf("marketdata: coverage: %w", err)
		}
		var state IntervalState
		switch {
		case !span.Elapsed(now):
			state = IntervalStateInProgress
		case present[span.Start().UTC().UnixNano()]:
			state = IntervalStatePresent
		default:
			state = IntervalStateMissing
		}
		if err := acc.add(state, span); err != nil {
			return nil, fmt.Errorf("marketdata: coverage: %w", err)
		}
		cursor = span.End()
	}
	acc.flush()
	return acc.gaps, nil
}
