package backtest_test

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/journal/jsonl"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/journal"
)

// TestRunner_JournalsFullRunInCausalOrder is the end-to-end proof for
// issue #218 (M5-10): a real backtest run, journaled through a real
// JSONLWriter, produces a file that can be read back in order and
// audited — the issue's own acceptance criteria.
func TestRunner_JournalsFullRunInCausalOrder(t *testing.T) {
	strat := mustEnterOnFirstBarStrategy(t)
	params := mustRunnerParams(t, strat)

	path := filepath.Join(t.TempDir(), "run.jsonl")
	w, err := jsonl.NewWriter(path)
	require.NoError(t, err)
	params.Journal = w

	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)
	result, err := runner.Run(context.Background())
	require.NoError(t, err)
	require.NoError(t, w.Close())

	r, err := jsonl.OpenReader(path)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	var entries []journal.Entry
	for {
		e, err := r.Next(context.Background())
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		entries = append(entries, e)
	}
	require.NotEmpty(t, entries)

	// The file starts with RunStarted and ends with RunCompleted,
	// bracketing everything else — the explicit lifecycle markers issue
	// #218's review asked for, letting a reader distinguish a genuinely
	// completed run from a syntactically valid partial one.
	assert.Equal(t, journal.KindRunStarted, entries[0].Kind)
	assert.True(t, entries[0].RunStarted.RunID.Equal(result.Manifest.RunID()))
	assert.NotEmpty(t, entries[0].RunStarted.Header, "header carries the run's own manifest")

	last := entries[len(entries)-1]
	assert.Equal(t, journal.KindRunCompleted, last.Kind)
	assert.Equal(t, uint64(len(entries)-1), last.RunCompleted.EntryCount)

	// Sequence is strictly increasing across the whole file, regardless
	// of which caller (Scheduler mid-run, Runner at the bookends)
	// produced each entry.
	for i, e := range entries {
		assert.Equal(t, uint64(i+1), e.Sequence)
		assert.True(t, e.RunID.Equal(result.Manifest.RunID()))
	}

	// Every represented stage of the intent lifecycle is present:
	// mustEnterOnFirstBarStrategy enters once per instrument, and
	// next-bar-open eligibility fills both, so a full
	// intent -> proposal -> decision -> request -> order/fill/account
	// chain must appear, in that causal order relative to each other.
	var kinds []journal.Kind
	for _, e := range entries {
		kinds = append(kinds, e.Kind)
	}
	assertBefore(t, kinds, journal.KindIntent, journal.KindProposal)
	assertBefore(t, kinds, journal.KindProposal, journal.KindDecision)
	assertBefore(t, kinds, journal.KindDecision, journal.KindRequest)
	assertBefore(t, kinds, journal.KindRequest, journal.KindOrder)

	assert.Contains(t, kinds, journal.KindFill)
	assert.Contains(t, kinds, journal.KindTrade)
	// Note: sim (adapters/broker/sim) never emits an EventKindAccount
	// broker event today — account state is observed only via
	// Account.Snapshot, not the event stream — so no KindAccount entry
	// is expected here. The journal correctly reflects that; it is not
	// a journaling gap.

	// Both open trades (per TestRunner_SuccessfulRun's own reconciliation)
	// are represented as KindTrade entries.
	tradeCount := 0
	for _, e := range entries {
		if e.Kind == journal.KindTrade {
			tradeCount++
		}
	}
	assert.Equal(t, len(result.Trades)+len(result.OpenTrades), tradeCount)
}

// assertBefore asserts the first occurrence of first appears strictly
// before the first occurrence of second in kinds.
func assertBefore(t *testing.T, kinds []journal.Kind, first, second journal.Kind) {
	t.Helper()
	firstIdx, secondIdx := -1, -1
	for i, k := range kinds {
		if firstIdx == -1 && k == first {
			firstIdx = i
		}
		if secondIdx == -1 && k == second {
			secondIdx = i
		}
	}
	require.NotEqual(t, -1, firstIdx, "%s never appeared", first)
	require.NotEqual(t, -1, secondIdx, "%s never appeared", second)
	assert.Less(t, firstIdx, secondIdx, "%s must appear before %s", first, second)
}

// TestRunner_JournalFailureAbortsRun proves a Journal.Record failure
// fails the run immediately rather than returning a successful Result
// alongside a silently incomplete journal (issue #218 review point 4).
func TestRunner_JournalFailureAbortsRun(t *testing.T) {
	strat := mustEnterOnFirstBarStrategy(t)
	params := mustRunnerParams(t, strat)
	params.Journal = failingRecorder{}

	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)
	_, err = runner.Run(context.Background())
	require.ErrorIs(t, err, errIntentional)
}

type failingRecorder struct{}

func (failingRecorder) Record(ctx context.Context, rec journal.Record) error { return errIntentional }
func (failingRecorder) Close() error                                         { return nil }

var _ journal.Recorder = failingRecorder{}
