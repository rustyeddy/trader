package marketdata

import (
	"context"
	"fmt"
	"time"
)

// Canonical build version identifiers, recorded verbatim in every
// Manifest a build publishes (issue #81, ADR-020). These are the first
// real values these fields ever hold: every prior issue's fixtures used
// placeholder strings ("builder-v1" and so on) precisely because no
// build operation existed yet to give them meaning. Bump the relevant
// constant whenever this package's build/validation/resampling/calendar
// logic changes in a way that affects how already-published bytes
// should be interpreted — that is what lets a future coverage check
// eventually compare a stored Manifest's version against "the version
// running now," a comparison #79 deliberately deferred for exactly this
// reason.
const (
	builderVersion          = "builder-v1"
	validatorVersion        = "validator-v1"
	resamplerVersionCurrent = "resampler-v1"
	// calendarVersionCurrent bumped to v2 for issue #99/ADR-021: Bar's
	// sub-day (M1/H1/H4) alignment is now anchored to the FX daily
	// rollover instead of UTC midnight, changing H4's actual boundaries
	// (M1/H1 land on the same instants either way, so no previously
	// published M1/H1/D1/W1 data is affected in substance — but any
	// canonical dataset's CalendarVersion should still name the exact
	// Bar implementation that produced it).
	calendarVersionCurrent = "fxcalendar-v2"
	// canonicalSchemaVersion is Manifest.SchemaVersion's current value:
	// the canonical Bar/BarSet field layout this build writes.
	canonicalSchemaVersion = 1
)

// BuildResult summarizes one Manager.Build call: every Action executed
// and every Action skipped, the same shape SyncResult (#80) already
// established for the identical reason — a caller can account for the
// full Plan it supplied, not just the successful subset.
type BuildResult struct {
	Published []PublishResult
	Skipped   []SkippedAction
}

// PublishResult reports the outcome of executing one
// ActionNormalizeCanonical or ActionDeriveCanonical entry.
type PublishResult struct {
	Action   Action
	Manifest Manifest
	BarCount int
}

// Build executes exactly the ActionNormalizeCanonical and
// ActionDeriveCanonical entries in plan — never a caller-supplied
// instrument/interval/range, and never its own recomputed plan —
// materializing canonical data and publishing it through the canonical
// store (issue #81, ADR-020). Bars, Coverage, Plan, and Sync remain
// completely untouched; Build is never invoked implicitly by any of
// them.
//
// Build itself does not require RawRoot to be configured: only
// normalizeAndPublish (ActionNormalizeCanonical) actually reads raw
// partitions, so only it checks RawRoot, and only when an
// ActionNormalizeCanonical entry is actually present. deriveAndPublish
// (ActionDeriveCanonical, W1 from canonical D1) never touches raw data
// at all — matching Plan's own deriveActionsW1, which already documents
// working correctly with no RawRoot configured — so a W1-only Plan, or
// a Plan whose only raw-dependent entries are ActionDownloadRaw/
// ActionRepairRaw (both reported in Skipped, never executed here), must
// not fail before those entries can even be reported.
//
// # Only canonical builds
//
// ActionDownloadRaw and ActionRepairRaw entries are reported in
// BuildResult.Skipped, not executed and not silently dropped:
// acquiring raw data is Sync's job (#80), not Build's. This is the
// mirror image of Sync's own "only raw downloads" scope split — between
// them, every ActionKind Plan can produce now has exactly one Manager
// method that will execute it, and neither method executes the other's
// kind.
//
// # Normalize versus derive
//
// ActionNormalizeCanonical (M1/H1/H4/D1, built directly from raw) and
// ActionDeriveCanonical (W1, resampled from canonical D1) are handled by
// normalizeAndPublish (build_normalize.go) and deriveAndPublish
// (resample.go) respectively — see each one's own doc comment for how it
// decides what to publish, what to skip, and what to abort on.
func (m *Manager) Build(ctx context.Context, plan Plan) (BuildResult, error) {
	if !m.configured() {
		return BuildResult{}, fmt.Errorf("marketdata: build: %w: manager is not configured", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return BuildResult{}, err
	}

	var result BuildResult
	for _, action := range plan.Actions {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var pr PublishResult
		var err error
		switch action.Kind {
		case ActionNormalizeCanonical:
			pr, err = m.normalizeAndPublish(ctx, action)
		case ActionDeriveCanonical:
			pr, err = m.deriveAndPublish(ctx, action)
		default:
			result.Skipped = append(result.Skipped, SkippedAction{
				Action: action,
				Reason: fmt.Sprintf("%s is not a canonical build action; executing it is Sync's responsibility, not Build's", action.Kind),
			})
			continue
		}
		if err != nil {
			return result, fmt.Errorf("marketdata: build: %s %s %04d-%02d: %w",
				action.Instrument, action.Interval, action.Year, int(action.Month), err)
		}
		result.Published = append(result.Published, pr)
	}
	return result, nil
}

// publishCanonicalMonth builds the partitionKey for
// (m.providerName, symbol, interval, year, month), publishes (manifest,
// bs) through the canonical store, and invalidates any cached entry for
// that key — a republish must never leave a stale cached Bars/Coverage
// read behind, the exact seam barCache.invalidate (#78) was added for.
func (m *Manager) publishCanonicalMonth(ctx context.Context, symbol string, interval Interval, year int, month time.Month, manifest Manifest, bs BarSet) error {
	key := partitionKey{
		provider: m.providerName, symbol: symbol, instrument: manifest.Instrument,
		interval: interval, year: year, month: month,
	}
	if err := m.store.publish(ctx, key, manifest, bs); err != nil {
		return err
	}
	m.cache.invalidate(key)
	return nil
}
