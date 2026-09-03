package emacross

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/order"
)

// memoryRecorder is a minimal in-memory journal.Recorder, used only to
// assert on the KindSignal records this package's own decision-
// evidence capability produces (issue #253, EMA-08) without needing a
// real storage adapter.
type memoryRecorder struct {
	records []journal.Record
}

func (r *memoryRecorder) Record(ctx context.Context, rec journal.Record) error {
	r.records = append(r.records, rec)
	return nil
}

func (r *memoryRecorder) Close() error { return nil }

// TestStrategy_RecordsNoSignalsWithoutJournal proves Journal is a
// genuinely optional capability: an Environment that never sets it
// (every other test in this package) must not panic or otherwise
// behave differently — recordSignal's own nil check is exercised by
// every other existing test already; this test exists only to name
// that property explicitly.
func TestStrategy_RecordsNoSignalsWithoutJournal(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})
	for i, close := range emaFixtureCloses {
		intents, _ := h.onBar(i+1, close)
		_ = intents
	}
	// No assertion beyond "did not panic" is possible here — h has no
	// journal to inspect — which is exactly the point.
}

// TestStrategy_RecordsDecisionEvidence replays the same worked fixture
// TestStrategy_FullFixtureMatchesEMA01WorkedExample already proves the
// exact intents for, and additionally asserts the journaled
// decision-evidence trail: no signal before both EMAs are ready (bars
// 1-4), one signal per bar from bar 5 on, "none"/"none" on every bar
// without a crossover, and the exact cross/action/correlation on bars
// 7 and 12 — proving a reviewer can trace the bar-7 and bar-12
// intents back to the exact crossover condition that caused them
// (issue #253's own acceptance criterion).
func TestStrategy_RecordsDecisionEvidence(t *testing.T) {
	runID := mustRunID(t)
	rec := &memoryRecorder{}
	h := newTestHarnessWithJournal(t, Config{FastPeriod: 3, SlowPeriod: 5}, rec, runID)

	var allIntents []order.Intent
	for i, close := range emaFixtureCloses {
		intents, _ := h.onBar(i+1, close)
		allIntents = append(allIntents, intents...)
		switch i + 1 {
		case 7:
			h.setPosition(order.Long)
		case 12:
			h.setPosition(order.Short)
		}
	}

	// Bars 1-4: fast is ready (period 3) but slow (period 5) is not —
	// no decision evidence is recorded before both are ready.
	require.Len(t, rec.records, 10, "one signal per bar from bar 5 (both EMAs ready) through bar 14")

	for _, r := range rec.records {
		require.Equal(t, journal.KindSignal, r.Kind)
		require.NotNil(t, r.Signal)
		assert.Equal(t, runID, r.RunID)
		assert.Equal(t, Name, r.Signal.Strategy)
	}

	// bar 5 (rec.records[0]): the first bar with a defined relation at
	// all — Decision 3 says no crossover can be detected here.
	assert.Equal(t, "none", rec.records[0].Signal.Values["cross"])
	assert.Equal(t, "none", rec.records[0].Signal.Values["action"])
	assert.Equal(t, "none", rec.records[0].Signal.Values["prev_relation"], "bar 5 has no prior relation to compare against")

	// bar 7 is rec.records[2] (bars 5,6,7,...): the bullish cross.
	bar7 := rec.records[2]
	assert.Equal(t, "bullish", bar7.Signal.Values["cross"])
	assert.Equal(t, "enter-long", bar7.Signal.Values["action"])
	assert.Equal(t, "below", bar7.Signal.Values["prev_relation"])
	assert.Equal(t, "above", bar7.Signal.Values["curr_relation"])
	require.Len(t, allIntents, 3, "one Enter (bar 7) plus one Exit+Enter reversal (bar 12)")
	assert.Equal(t, allIntents[0].Metadata.CorrelationID, bar7.Metadata.CorrelationID,
		"the signal must correlate with the exact intent it caused")

	// bar 12 is rec.records[7]: the bearish reversal.
	bar12 := rec.records[7]
	assert.Equal(t, "bearish", bar12.Signal.Values["cross"])
	assert.Equal(t, "reverse", bar12.Signal.Values["action"])
	assert.Equal(t, "above", bar12.Signal.Values["prev_relation"])
	assert.Equal(t, "below", bar12.Signal.Values["curr_relation"])
	assert.Equal(t, allIntents[1].Metadata.CorrelationID, bar12.Metadata.CorrelationID)
	assert.Equal(t, allIntents[2].Metadata.CorrelationID, bar12.Metadata.CorrelationID,
		"both legs of the reversal must share the signal's own correlation id")

	// Every other bar: no cross, no action, zero correlation id (no
	// execution-side counterpart exists to correlate with).
	for i, r := range rec.records {
		if r.Signal == bar7.Signal || r.Signal == bar12.Signal {
			continue
		}
		assert.Equalf(t, "none", r.Signal.Values["cross"], "record %d", i)
		assert.Equalf(t, "none", r.Signal.Values["action"], "record %d", i)
		assert.Truef(t, r.Metadata.CorrelationID.IsZero(), "record %d: no-action bar must have a zero correlation id", i)
	}
}

func mustRunID(t *testing.T) id.RunID {
	t.Helper()
	c := clock.NewSimulated(testStart)
	ids := id.NewGenerator(c, id.NewDeterministic(9, 10))
	runID, err := id.GenerateRunID(ids)
	require.NoError(t, err)
	return runID
}

// erroringRecorder is a journal.Recorder that always fails, used to
// prove OnBar propagates a journaling failure rather than silently
// discarding it (journal.Recorder's own "never a discarded write"
// contract).
type erroringRecorder struct{}

func (erroringRecorder) Record(ctx context.Context, rec journal.Record) error {
	return errBoom
}
func (erroringRecorder) Close() error { return nil }

func TestStrategy_RecordSignalFailurePropagates(t *testing.T) {
	h := newTestHarnessWithJournal(t, Config{FastPeriod: 3, SlowPeriod: 5}, erroringRecorder{}, mustRunID(t))

	// Bars 1-4 (not yet ready) never reach recordSignal at all.
	for i, close := range emaFixtureCloses[:4] {
		event, view, _ := h.buildBar(i+1, close)
		_, err := h.strategy.OnBar(context.Background(), event, view)
		require.NoError(t, err)
	}

	event, view, _ := h.buildBar(5, emaFixtureCloses[4])
	_, err := h.strategy.OnBar(context.Background(), event, view)
	require.ErrorIs(t, err, errBoom)
}
