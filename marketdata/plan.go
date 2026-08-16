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
	// Actions is ordered download-raw, then normalize-canonical, then
	// derive-canonical — each bucket internally chronological, matching
	// Coverage.Partitions — since later stages depend on earlier ones
	// having already run. This is "gated scheduling": an action for one
	// stage is only produced once that stage's own input is already
	// satisfiable (see the Plan method's doc comment), not a promise
	// that every dependency completes before the next Plan is computed.
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
		actions, err = m.deriveActionsW1(ctx, query, cov)
	} else {
		actions, err = m.deriveActionsRawBuilt(query, cov, symbol, rawByKey)
	}
	if err != nil {
		return Plan{}, err
	}

	return Plan{Coverage: cov, Actions: actions}, nil
}

// deriveActionsRawBuilt derives Actions for a raw-built interval (M1,
// H1, H4, D1): for each touched month, a missing or integrity-failed raw
// partition schedules ActionDownloadRaw and blocks any canonical action
// that month; an OK raw partition may still additionally need
// ActionDownloadRaw with reason "extend" if it is the most recent
// touched month and the calendar reports open intervals past its last
// record. A canonical partition that is Missing, Invalid, or Stale
// schedules ActionNormalizeCanonical once its raw is confirmed OK.
func (m *Manager) deriveActionsRawBuilt(query BarQuery, cov Coverage, symbol string, rawByKey map[rawPartitionKey]oanda.Partition) ([]Action, error) {
	rawInterval, ok := intervalToRawInterval(query.Interval)
	if !ok {
		return nil, fmt.Errorf("marketdata: plan: %w: interval %s has no raw partition", ErrInvalidQuery, query.Interval)
	}

	var downloads, normalizes []Action
	for i, pc := range cov.Partitions {
		p, found := rawByKey[rawPartitionKey{symbol, rawInterval, pc.Year, pc.Month}]
		switch {
		case !found:
			downloads = append(downloads, Action{
				Kind: ActionDownloadRaw, Instrument: query.Instrument, Interval: query.Interval,
				Year: pc.Year, Month: pc.Month, Reason: "missing",
			})
			continue // gated: nothing downstream can run without raw
		case p.Status != oanda.PartitionStatusOK:
			downloads = append(downloads, Action{
				Kind: ActionDownloadRaw, Instrument: query.Instrument, Interval: query.Interval,
				Year: pc.Year, Month: pc.Month, Reason: fmt.Sprintf("raw partition %s: needs re-acquisition", p.Status),
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
				downloads = append(downloads, Action{
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

	return append(downloads, normalizes...), nil
}

// needsExtend reports whether p — an OK raw partition for the most
// recently touched month in the query — should be extended: whether the
// calendar reports any open bar interval strictly after p's last known
// record and at or before min(now, query.Range.End).
func (m *Manager) needsExtend(p oanda.Partition, queryRange TimeRange, interval Interval) (bool, error) {
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
// schedules ActionDeriveCanonical only when it is not already
// PartitionCoverageCurrent and the underlying canonical D1 range for
// that same UTC month is itself complete (every expected D1 interval
// Present or Closed) — the ADR-012 single-hop dependency, reproduced
// without a general resampling-dependency graph since W1 is this
// package's only derived interval.
func (m *Manager) deriveActionsW1(ctx context.Context, query BarQuery, cov Coverage) ([]Action, error) {
	var actions []Action
	for _, pc := range cov.Partitions {
		if pc.Status == PartitionCoverageCurrent {
			continue
		}

		monthStart := time.Date(pc.Year, pc.Month, 1, 0, 0, 0, 0, time.UTC)
		monthEnd := monthStart.AddDate(0, 1, 0)
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

		d1Cov, err := m.Coverage(ctx, BarQuery{Instrument: query.Instrument, Interval: D1, Range: d1Range})
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
