package marketdata

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata/internal/provider/oanda"
)

// ActionKind names one unit of required work a Plan describes.
type ActionKind uint8

const (
	// ActionUnknown is ActionKind's zero value; Plan never produces an
	// Action with this Kind.
	ActionUnknown ActionKind = iota
	// ActionDownloadRaw means a raw provider partition must be acquired
	// (it is entirely missing, or exists but failed integrity checks)
	// or extended (it exists and is fine, but the calendar reports open
	// intervals after its last known record).
	ActionDownloadRaw
	// ActionNormalizeCanonical means a canonical partition must be
	// built or rebuilt directly from same-interval raw data (M1, H1,
	// H4, or D1).
	ActionNormalizeCanonical
	// ActionDeriveCanonical means a canonical W1 partition must be
	// built or rebuilt by resampling canonical D1 data (ADR-012).
	ActionDeriveCanonical
	// ActionRepairRaw means a raw partition file exists but failed
	// integrity checks (PartitionCoverageInvalid's raw-side analogue) —
	// its content cannot be trusted, merged with, or extended, only
	// entirely replaced. This is deliberately a distinct ActionKind
	// from ActionDownloadRaw, not the same Kind with a different Reason
	// string: repairing a corrupted raw artifact is a materially
	// different, more destructive operation than acquiring or extending
	// a trustworthy one (issue #80's "prevent accidental replacement of
	// an existing authoritative raw artifact unless an explicit repair
	// operation is authorized"), and Manager.Sync must be able to tell
	// them apart from the Kind alone rather than parsing Reason text.
	// Sync does not execute ActionRepairRaw at all — see its own doc
	// comment — so every ActionRepairRaw a Plan produces always
	// surfaces in SyncResult.Skipped; implementing the actual repair
	// operation remains future, separately-authorized work.
	ActionRepairRaw
)

// String returns a human-readable ActionKind name.
func (k ActionKind) String() string {
	switch k {
	case ActionUnknown:
		return "unknown"
	case ActionDownloadRaw:
		return "download-raw"
	case ActionNormalizeCanonical:
		return "normalize-canonical"
	case ActionDeriveCanonical:
		return "derive-canonical"
	case ActionRepairRaw:
		return "repair-raw"
	default:
		return fmt.Sprintf("ActionKind(%d)", uint8(k))
	}
}

// Action is one deterministic unit of required work: acquire raw data,
// or build/rebuild a canonical partition. Interval is the raw-native
// interval being downloaded for ActionDownloadRaw, the canonical
// interval being built for ActionNormalizeCanonical (always equal to
// the raw interval it is built from), or the canonical W1 interval for
// ActionDeriveCanonical. Executing an Action is a future, explicit
// synchronization/build operation's responsibility; Plan only describes
// it.
type Action struct {
	Kind       ActionKind
	Instrument instrument.ID
	Interval   Interval
	Year       int
	Month      time.Month
	Reason     string
}

// Plan is a deterministic, read-only description of the raw downloads
// and canonical builds required to satisfy a BarQuery-shaped request, as
// of the moment it was produced (issue #79, ADR-020). Plan performs no
// I/O beyond the same reads Coverage performs; it never downloads,
// builds, or publishes anything itself.
type Plan struct {
	Coverage Coverage
	// Actions is ordered raw (download-raw and repair-raw, interleaved
	// in Coverage.Partitions order), then normalize-canonical, then
	// derive-canonical — since later stages depend on earlier ones
	// having already run. This is "gated scheduling": an action for one
	// stage is only produced once that stage's own input is already
	// satisfiable (see the Plan method's doc comment), not a promise
	// that every dependency completes before the next Plan is computed.
	// Manager.Sync only ever executes ActionDownloadRaw; ActionRepairRaw
	// always ends up in Sync's SkippedAction list today (see its own
	// doc comment) — Plan still reports it so a caller can see the
	// distinction rather than a query that simply never converges.
	Actions []Action
}

// Plan analyzes query the same way Coverage does, then derives the
// deterministic sequence of Actions required to close every gap Coverage
// finds.
//
// # Gated scheduling
//
// An action for one stage is only produced when that stage's own input
// is already satisfiable: ActionNormalizeCanonical is never produced for
// a month whose raw partition is missing or failed integrity checks
// (only ActionDownloadRaw is, for that month); ActionDeriveCanonical is
// never produced for a W1 month whose underlying D1 range is not itself
// complete (Present or Closed throughout — see DatasetComplete). This
// reproduces the legacy planner's dependency-aware scheduling
// (Inventory -> WantList -> Plan -> Download/Build) without transplanting
// legacy code or coupling.
func (m *Manager) Plan(ctx context.Context, query BarQuery) (Plan, error) {
	if !m.configured() {
		return Plan{}, fmt.Errorf("marketdata: plan: %w: manager is not configured", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	if err := query.validate(); err != nil {
		return Plan{}, fmt.Errorf("marketdata: plan: %w", err)
	}

	listing, err := m.resolver.ResolveInstrument(query.Instrument, m.providerName, "")
	if err != nil {
		return Plan{}, fmt.Errorf("marketdata: plan: resolve listing: %w", err)
	}
	symbol := listing.Symbol()

	rawByKey, err := m.rawInventoryLookup(ctx, query.Interval)
	if err != nil {
		return Plan{}, fmt.Errorf("marketdata: plan: %w", err)
	}

	cov, err := m.coverage(ctx, query, symbol, rawByKey)
	if err != nil {
		return Plan{}, err
	}

	var actions []Action
	if query.Interval == W1 {
		actions, err = m.deriveActionsW1(ctx, query, cov, symbol)
	} else {
		actions, err = m.deriveActionsRawBuilt(query, cov, symbol, rawByKey)
	}
	if err != nil {
		return Plan{}, err
	}

	return Plan{Coverage: cov, Actions: actions}, nil
}

// deriveActionsRawBuilt derives Actions for a raw-built interval (M1,
// H1, H4, D1): for each touched month, a missing raw partition schedules
// ActionDownloadRaw and an integrity-failed one schedules
// ActionRepairRaw instead (see its own doc comment for why that is a
// distinct Kind) — either way blocking any canonical action that month;
// an OK raw partition may still additionally need ActionDownloadRaw with
// reason "extend" if it is the most recent touched month and the
// calendar reports open intervals past its last record. A canonical
// partition that is Missing, Invalid, or Stale schedules
// ActionNormalizeCanonical once its raw is confirmed OK.
func (m *Manager) deriveActionsRawBuilt(query BarQuery, cov Coverage, symbol string, rawByKey map[rawPartitionKey]oanda.Partition) ([]Action, error) {
	rawInterval, ok := intervalToRawInterval(query.Interval)
	if !ok {
		return nil, fmt.Errorf("marketdata: plan: %w: interval %s has no raw partition", ErrInvalidQuery, query.Interval)
	}

	var rawActions, normalizes []Action
	for i, pc := range cov.Partitions {
		p, found := rawByKey[rawPartitionKey{symbol, rawInterval, pc.Year, pc.Month}]
		switch {
		case !found:
			rawActions = append(rawActions, Action{
				Kind: ActionDownloadRaw, Instrument: query.Instrument, Interval: query.Interval,
				Year: pc.Year, Month: pc.Month, Reason: "missing",
			})
			continue // gated: nothing downstream can run without raw
		case p.Status != oanda.PartitionStatusOK:
			// A distinct Kind, not ActionDownloadRaw with a different
			// Reason: this raw file exists but cannot be trusted, so
			// nothing can extend or merge with it — only an explicitly
			// authorized repair can replace it. See ActionRepairRaw's
			// own doc comment.
			rawActions = append(rawActions, Action{
				Kind: ActionRepairRaw, Instrument: query.Instrument, Interval: query.Interval,
				Year: pc.Year, Month: pc.Month, Reason: fmt.Sprintf("raw partition %s", p.Status),
			})
			continue
		}

		normalizeReason := ""
		isLatestTouchedMonth := i == len(cov.Partitions)-1
		if isLatestTouchedMonth {
			needExtend, err := m.needsExtend(p, query.Range, query.Interval)
			if err != nil {
				return nil, err
			}
			if needExtend {
				rawActions = append(rawActions, Action{
					Kind: ActionDownloadRaw, Instrument: query.Instrument, Interval: query.Interval,
					Year: pc.Year, Month: pc.Month, Reason: "extend",
				})
				normalizeReason = "extend: rebuild once raw catches up"
			}
		}
		switch pc.Status {
		case PartitionCoverageMissing:
			normalizeReason = "missing"
		case PartitionCoverageInvalid:
			normalizeReason = "invalid: rebuild"
		case PartitionCoverageStale:
			normalizeReason = "stale: raw fingerprint changed"
		}
		if normalizeReason != "" {
			normalizes = append(normalizes, Action{
				Kind: ActionNormalizeCanonical, Instrument: query.Instrument, Interval: query.Interval,
				Year: pc.Year, Month: pc.Month, Reason: normalizeReason,
			})
		}
	}

	return append(rawActions, normalizes...), nil
}

// needsExtend reports whether p — an OK raw partition for the most
// recently touched month in the query — should be extended: either the
// calendar reports an open bar interval strictly after p's last known
// record and at or before min(now, query.Range.End), or p's own last
// record is itself still provider-incomplete (LastComplete false) — a
// design review's finding: without this second check, a partition whose
// tail candle OANDA had not yet finalized at fetch time would never be
// revisited even though the calendar has nothing new to report, freezing
// a provisional OHLC/volume in place permanently. See
// marketdata.Manager.Sync's own from-selection for the matching
// re-fetch-not-skip behavior once an extend is actually scheduled.
func (m *Manager) needsExtend(p oanda.Partition, queryRange TimeRange, interval Interval) (bool, error) {
	if p.RowCount > 0 && !p.LastComplete {
		return true, nil
	}

	upper := queryRange.End()
	if now := m.clock.Now(); now.Before(upper) {
		upper = now
	}

	cursor := p.LastTime
	if cursor.IsZero() {
		// An OK but empty raw partition (RowCount 0): there is no
		// recorded bound to walk forward from, so fall back to the
		// start of the partition's own filed month rather than an
		// arbitrarily distant past instant — bounding the walk below to
		// at most one month's worth of boundaries.
		cursor = time.Date(p.Year, p.Month, 1, 0, 0, 0, 0, time.UTC)
	}
	span, err := m.calendar.Bar(cursor, interval)
	if err != nil {
		return false, fmt.Errorf("marketdata: plan: %w", err)
	}
	next := span.End()
	for next.Before(upper) {
		nspan, err := m.calendar.Bar(next, interval)
		if err != nil {
			return false, fmt.Errorf("marketdata: plan: %w", err)
		}
		if m.calendar.Status(nspan.Start()) == StatusOpen {
			return true, nil
		}
		next = nspan.End()
	}
	return false, nil
}

// deriveActionsW1 derives Actions for the derived W1 interval: a month
// schedules ActionDeriveCanonical when it is not already
// PartitionCoverageCurrent (see below for the Current-with-gaps
// exception) and the underlying canonical D1 range for that same UTC
// month is itself complete (every expected D1 interval Present or
// Closed) — the ADR-012 single-hop dependency, reproduced without a
// general resampling-dependency graph since W1 is this package's only
// derived interval.
//
// The D1 prerequisite check calls the unexported coverage directly, with
// a nil raw lookup, rather than the public Coverage method: Coverage(D1)
// requires RawRoot (D1 is raw-built), but a Manager legitimately used
// for W1 planning only may never have configured RawRoot at all — W1
// itself has no raw side. A nil rawByKey simply means isStale can never
// confirm D1 staleness from this check (its existing "cannot verify,
// don't judge stale" default applies); that is an acceptable, narrower
// question than a direct Plan(D1) call would answer, not a silent
// correctness gap — a caller who cares about D1's own raw-fingerprint
// staleness can and should call Plan/Coverage for D1 directly.
//
// # A Current partition can still need reconvergence
//
// deriveAndPublish (issue #81) can leave a boundary week absent from an
// otherwise-published, PartitionCoverageCurrent W1 partition, when that
// week's D1 input (potentially spilling into the next month's D1
// partition — see w1SpansNextMonth) was not yet available at build
// time. The partition's own recorded lineage (Manifest.Parent) reflects
// only what it *did* draw from, so once the missing D1 data arrives,
// nothing about the W1 partition's own state changes — it stays
// Current, and unconditionally skipping every Current partition here
// would mean that boundary week never converges. w1CurrentPartitionReady
// reconsiders a Current partition specifically when Coverage's own
// W1-level Gaps (computed only for Current partitions, coverage.go) show
// such a gap, checking D1 completeness over the gap's true dependency —
// unclipped by query.Range, unlike the missing/invalid/stale branches
// below, which stay deliberately lazy about how much of the month a
// caller's own query asked about.
func (m *Manager) deriveActionsW1(ctx context.Context, query BarQuery, cov Coverage, symbol string) ([]Action, error) {
	var actions []Action
	for _, pc := range cov.Partitions {
		monthStart := time.Date(pc.Year, pc.Month, 1, 0, 0, 0, 0, time.UTC)
		monthEnd := monthStart.AddDate(0, 1, 0)

		if pc.Status == PartitionCoverageCurrent {
			needsDerive, err := m.w1CurrentPartitionNeedsDerive(ctx, query, cov, monthStart, monthEnd, symbol)
			if err != nil {
				return nil, err
			}
			if !needsDerive {
				continue
			}
			actions = append(actions, Action{
				Kind: ActionDeriveCanonical, Instrument: query.Instrument, Interval: W1,
				Year: pc.Year, Month: pc.Month, Reason: "gap: D1 input now available for a previously incomplete week",
			})
			continue
		}

		clipStart, clipEnd := monthStart, monthEnd
		if query.Range.Start().After(clipStart) {
			clipStart = query.Range.Start()
		}
		if query.Range.End().Before(clipEnd) {
			clipEnd = query.Range.End()
		}
		if !clipEnd.After(clipStart) {
			continue
		}
		d1Range, err := NewTimeRange(clipStart, clipEnd)
		if err != nil {
			return nil, fmt.Errorf("marketdata: plan: %w", err)
		}

		d1Query := BarQuery{Instrument: query.Instrument, Interval: D1, Range: d1Range}
		d1Cov, err := m.coverage(ctx, d1Query, symbol, nil)
		if err != nil {
			return nil, fmt.Errorf("marketdata: plan: derive: check D1 prerequisite: %w", err)
		}
		if !d1Complete(d1Cov) {
			continue // gated: D1 input is not yet satisfiable
		}

		reason := "missing"
		switch pc.Status {
		case PartitionCoverageInvalid:
			reason = "invalid: rebuild"
		case PartitionCoverageStale:
			reason = "stale: parent D1 revision changed"
		}
		actions = append(actions, Action{
			Kind: ActionDeriveCanonical, Instrument: query.Instrument, Interval: W1,
			Year: pc.Year, Month: pc.Month, Reason: reason,
		})
	}
	return actions, nil
}

// w1CurrentPartitionNeedsDerive reports whether a Current W1 partition
// for [monthStart, monthEnd) should still schedule ActionDeriveCanonical
// because at least one genuine W1-level Gap (cov.Gaps, computed only
// for Current partitions by coverage's own gapsForPartition) overlapping
// this month now has a satisfiable D1 dependency.
//
// Unlike the missing/invalid/stale branches in deriveActionsW1, this
// check does not require the D1 input to be gapless across the whole
// month (d1Complete's stricter bar): a Current partition's other weeks
// already published successfully, so only the specific gapped week(s)
// need to be re-checked, via weekIsD1Ready — the identical per-week
// readiness test deriveAndPublish itself applies at Build time — over a
// D1 Coverage queried through weekSpansForMonth's own coverageEnd, so a
// gap's spillover into the next month's D1 partition is visible here
// too. It also does not clip to query.Range the way the other branches
// deliberately do: a gap's true D1 dependency determines whether it can
// converge, independent of how narrowly the caller's own W1 query
// happened to be scoped. A Current partition with no overlapping gap
// needs no reconsideration at all, and never reaches the D1 check.
func (m *Manager) w1CurrentPartitionNeedsDerive(ctx context.Context, query BarQuery, cov Coverage, monthStart, monthEnd time.Time, symbol string) (bool, error) {
	gapSpans := overlappingGapSpans(cov.Gaps, monthStart, monthEnd)
	if len(gapSpans) == 0 {
		return false, nil
	}

	_, coverageEnd, err := weekSpansForMonth(m.calendar, monthStart, monthEnd)
	if err != nil {
		return false, fmt.Errorf("marketdata: plan: %w", err)
	}
	d1Range, err := NewTimeRange(monthStart, coverageEnd)
	if err != nil {
		return false, fmt.Errorf("marketdata: plan: %w", err)
	}
	d1Query := BarQuery{Instrument: query.Instrument, Interval: D1, Range: d1Range}
	d1Cov, err := m.coverage(ctx, d1Query, symbol, nil)
	if err != nil {
		return false, fmt.Errorf("marketdata: plan: derive: check D1 prerequisite: %w", err)
	}

	for _, gapSpan := range gapSpans {
		if weekIsD1Ready(d1Cov, gapSpan) {
			return true, nil
		}
	}
	return false, nil
}

// overlappingGapSpans returns the Span of every gap in gaps that
// overlaps the half-open range [start, end).
func overlappingGapSpans(gaps []Gap, start, end time.Time) []TimeRange {
	var spans []TimeRange
	for _, g := range gaps {
		if g.Span.Start().Before(end) && g.Span.End().After(start) {
			spans = append(spans, g.Span)
		}
	}
	return spans
}

// d1Complete reports whether cov (a D1 Coverage) describes a fully
// satisfiable resampling input: every touched month loaded as
// PartitionCoverageCurrent, and no Gaps at all.
func d1Complete(cov Coverage) bool {
	if len(cov.Gaps) > 0 {
		return false
	}
	for _, pc := range cov.Partitions {
		if pc.Status != PartitionCoverageCurrent {
			return false
		}
	}
	return true
}
