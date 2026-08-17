package marketdata

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func actionsOfKind(actions []Action, kind ActionKind) []Action {
	var out []Action
	for _, a := range actions {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

func TestPlan_DownloadMissingRawGatesNormalize(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir()) // no raw at all
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	plan, err := mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)

	downloads := actionsOfKind(plan.Actions, ActionDownloadRaw)
	require.Len(t, downloads, 1)
	assert.Equal(t, "missing", downloads[0].Reason)
	assert.Empty(t, actionsOfKind(plan.Actions, ActionNormalizeCanonical), "no normalize without raw")
}

func TestPlan_RawMalformedGatesNormalize(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	// A raw partition with a valid schema line but a corrupted column
	// header: oanda.Inspect reports it PartitionStatusMalformed.
	writeMalformedRawPartition(t, rawRoot, "EURUSD", "h1", 2024, time.January)

	plan, err := mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)

	downloads := actionsOfKind(plan.Actions, ActionDownloadRaw)
	require.Len(t, downloads, 1)
	assert.Contains(t, downloads[0].Reason, "malformed")
	assert.Empty(t, actionsOfKind(plan.Actions, ActionNormalizeCanonical))
}

func TestPlan_NormalizeMissingCanonicalWhenRawOK(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	// A one-hour range with a matching single raw row, so raw's
	// LastTime already reaches the range's end and "extend" cannot
	// trigger — this test is about the missing-canonical case only.
	span, err := NewTimeRange(aWeekday(0), aWeekday(1))
	require.NoError(t, err)
	writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January, rawRow(aWeekday(0), true))

	plan, err := mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)

	assert.Empty(t, actionsOfKind(plan.Actions, ActionDownloadRaw), "raw is OK and current, no extend needed within range")
	normalizes := actionsOfKind(plan.Actions, ActionNormalizeCanonical)
	require.Len(t, normalizes, 1)
	assert.Equal(t, "missing", normalizes[0].Reason)
}

func TestPlan_NormalizeStaleCanonical(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)
	writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January, rawRow(aWeekday(0), true))

	bars := []Bar{barAt(t, aWeekday(0))}
	publishCanonicalMonth(t, mgr, H1, 2024, time.January, span, bars, validRawFingerprint, nil) // deliberately wrong fingerprint

	plan, err := mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)

	normalizes := actionsOfKind(plan.Actions, ActionNormalizeCanonical)
	require.Len(t, normalizes, 1)
	assert.Contains(t, normalizes[0].Reason, "stale")
}

func TestPlan_NoActionsWhenCurrentAndComplete(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	// A range fully in the past relative to the clock, so "extend" never
	// triggers.
	start := aWeekday(0)
	end := aWeekday(1)
	span, err := NewTimeRange(start, end)
	require.NoError(t, err)

	rawPath := writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January, rawRow(start, true))
	fp := rawFingerprint(t, rawPath)
	bars := []Bar{barAt(t, start)}
	publishCanonicalMonth(t, mgr, H1, 2024, time.January, span, bars, fp, nil)

	plan, err := mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	assert.Empty(t, plan.Actions)
	assert.Empty(t, plan.Coverage.Gaps)
}

func TestPlan_ExtendCurrentMonthWhenRawLagsCalendar(t *testing.T) {
	rawRoot := t.TempDir()
	// Clock set well past the raw partition's last record, so the query
	// range (extending to "now") has open calendar intervals raw hasn't
	// caught up to.
	now := aWeekday(6)
	mgr, err := New(Config{
		Clock:        clock.NewSimulated(now),
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     testResolver(t),
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	span, err := NewTimeRange(aWeekday(0), now)
	require.NoError(t, err)
	rawPath := writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January, rawRow(aWeekday(0), true))
	fp := rawFingerprint(t, rawPath)
	bars := []Bar{barAt(t, aWeekday(0))}
	publishCanonicalMonth(t, mgr, H1, 2024, time.January, span, bars, fp, nil)

	plan, err := mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)

	downloads := actionsOfKind(plan.Actions, ActionDownloadRaw)
	require.Len(t, downloads, 1)
	assert.Equal(t, "extend", downloads[0].Reason)
	normalizes := actionsOfKind(plan.Actions, ActionNormalizeCanonical)
	require.Len(t, normalizes, 1)
	assert.Contains(t, normalizes[0].Reason, "extend")
}

func TestPlan_DeriveW1GatedOnD1Completeness(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	// D1 is incomplete: no D1 partition published at all.
	plan, err := mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: W1, Range: span})
	require.NoError(t, err)
	assert.Empty(t, actionsOfKind(plan.Actions, ActionDeriveCanonical), "D1 not complete: no derive yet")

	// Publish a complete D1 partition covering the one calendar day
	// query.Range falls within. The D1 bar's Time must be the actual
	// FXCalendar day boundary (a 17:00 New York rollover), not an
	// arbitrary UTC hour — D1 gap detection classifies presence by that
	// exact boundary instant.
	dayBoundary, err := mgr.calendar.Bar(aWeekday(0), D1)
	require.NoError(t, err)
	d1Bars := []Bar{barAt(t, dayBoundary.Start())}
	publishCanonicalMonth(t, mgr, D1, 2024, time.January, dayBoundary, d1Bars, validRawFingerprint, nil)

	plan2, err := mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: W1, Range: span})
	require.NoError(t, err)
	derives := actionsOfKind(plan2.Actions, ActionDeriveCanonical)
	require.Len(t, derives, 1)
	assert.Equal(t, "missing", derives[0].Reason)
	assert.Equal(t, W1, derives[0].Interval)
}

func TestPlan_ActionOrderingIsDownloadThenNormalizeThenDerive(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	span, err := NewTimeRange(aWeekday(0), aWeekday(1))
	require.NoError(t, err)
	// No raw, no canonical -> only a download action, but assert the
	// documented ordering contract holds for the general shape too.
	plan, err := mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)

	lastKind := ActionKind(0)
	for _, a := range plan.Actions {
		assert.GreaterOrEqual(t, int(a.Kind), int(lastKind), "actions must be ordered download < normalize < derive")
		lastKind = a.Kind
	}
}

func TestPlan_InvalidQuery(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	_, err := mgr.Plan(context.Background(), BarQuery{})
	assert.ErrorIs(t, err, ErrInvalidQuery)
}

func TestPlan_CancelledContext(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	span, err := NewTimeRange(aWeekday(0), aWeekday(1))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = mgr.Plan(ctx, BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPlan_NotConfigured(t *testing.T) {
	var mgr Manager
	span, err := NewTimeRange(aWeekday(0), aWeekday(1))
	require.NoError(t, err)
	_, err = mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestPlan_ExtendWithEmptyRawPartitionFallsBackToFiledMonth(t *testing.T) {
	rawRoot := t.TempDir()
	now := aWeekday(6)
	mgr, err := New(Config{
		Clock:        clock.NewSimulated(now),
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     testResolver(t),
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	// An OK but empty raw partition: zero rows, so Partition.LastTime is
	// the zero value. writeRawPartition with no rows still produces a
	// valid header-only file oanda.Inspect reports PartitionStatusOK
	// for.
	writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January)

	span, err := NewTimeRange(aWeekday(0), now)
	require.NoError(t, err)

	plan, err := mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)

	downloads := actionsOfKind(plan.Actions, ActionDownloadRaw)
	require.Len(t, downloads, 1)
	assert.Equal(t, "extend", downloads[0].Reason)
}

// TestPlan_DeriveW1WorksWithoutRawRootConfigured is the regression for a
// review finding: deriveActionsW1's D1 prerequisite check previously
// went through the public Coverage method, which requires RawRoot for
// D1 (a raw-built interval) — even though a Manager used only for W1
// planning has no reason to configure RawRoot at all, since W1 itself
// has no raw side.
func TestPlan_DeriveW1WorksWithoutRawRootConfigured(t *testing.T) {
	mgr := newTestManagerWithRaw(t, "") // no RawRoot configured
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	dayBoundary, err := mgr.calendar.Bar(aWeekday(0), D1)
	require.NoError(t, err)
	d1Bars := []Bar{barAt(t, dayBoundary.Start())}
	publishCanonicalMonth(t, mgr, D1, 2024, time.January, dayBoundary, d1Bars, validRawFingerprint, nil)

	plan, err := mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: W1, Range: span})
	require.NoError(t, err, "Plan(W1) must not require RawRoot")
	derives := actionsOfKind(plan.Actions, ActionDeriveCanonical)
	require.Len(t, derives, 1)
}

func TestPlan_UnsupportedRawIntervalErrors(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	odd, err := NewInterval(UnitHour, 7) // valid Interval, but not one of M1/H1/H4/D1
	require.NoError(t, err)
	span, err := NewTimeRange(aWeekday(0), aWeekday(1))
	require.NoError(t, err)

	_, err = mgr.Plan(context.Background(), BarQuery{Instrument: eurusd(), Interval: odd, Range: span})
	assert.ErrorIs(t, err, ErrInvalidQuery)
}

func TestPlan_CancelledMidLoop(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	span, err := NewTimeRange(
		time.Date(2024, time.January, 31, 23, 0, 0, 0, time.UTC),
		time.Date(2024, time.February, 1, 2, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	ctx := &nthCancelContext{Context: context.Background(), remaining: 3}
	_, err = mgr.Plan(ctx, BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestActionKind_String(t *testing.T) {
	assert.Equal(t, "download-raw", ActionDownloadRaw.String())
	assert.Equal(t, "normalize-canonical", ActionNormalizeCanonical.String())
	assert.Equal(t, "derive-canonical", ActionDeriveCanonical.String())
	assert.Contains(t, ActionKind(99).String(), "ActionKind")
}
