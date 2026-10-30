package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptStrategy returns a fixed intent per bar index and records the bars
// it was shown.
type scriptStrategy struct {
	intents []intent
	seen    []bar
}

func (s *scriptStrategy) init(map[string]any) error { return nil }

func (s *scriptStrategy) onBar(_ context.Context, b bar) intent {
	i := len(s.seen)
	s.seen = append(s.seen, b)
	if i < len(s.intents) {
		return s.intents[i]
	}
	return intent{}
}

// recordHandler records intents and fails on a chosen call.
type recordHandler struct {
	got    []intent
	failAt int // 1-based call number to fail on; 0 means never
	err    error
}

func (h *recordHandler) handleIntent(_ context.Context, i intent) error {
	h.got = append(h.got, i)
	if h.failAt != 0 && len(h.got) == h.failAt {
		return h.err
	}
	return nil
}

func seq(bars []bar, errAt int, err error) iter.Seq2[bar, error] {
	return func(yield func(bar, error) bool) {
		for i, b := range bars {
			if i == errAt {
				yield(bar{}, err)
				return
			}
			if !yield(b, nil) {
				return
			}
		}
	}
}

func TestBacktestRun_AcceptedRequests(t *testing.T) {
	bars := closes(1, 2, 3)
	strat := &scriptStrategy{intents: []intent{{action: open}, {}, {action: close}}}
	h := &recordHandler{}

	err := (&backtest{}).run(context.Background(), seq(bars, -1, nil), strat, h)

	require.NoError(t, err)
	assert.Equal(t, bars, strat.seen)
	assert.Equal(t, []intent{{action: open}, {}, {action: close}}, h.got)
}

func TestBacktestRun_NoBarsNoCalls(t *testing.T) {
	strat := &scriptStrategy{}
	h := &recordHandler{}

	require.NoError(t, (&backtest{}).run(context.Background(), seq(nil, -1, nil), strat, h))
	assert.Empty(t, strat.seen)
	assert.Empty(t, h.got)
}

func TestBacktestRun_FatalHandlerErrorStopsRun(t *testing.T) {
	boom := errors.New("rejected")
	strat := &scriptStrategy{}
	h := &recordHandler{failAt: 2, err: boom}

	err := (&backtest{}).run(context.Background(), seq(closes(1, 2, 3, 4), -1, nil), strat, h)

	require.ErrorIs(t, err, boom)
	assert.Len(t, strat.seen, 2, "no bars processed after the failure")
	assert.Len(t, h.got, 2)
}

func TestBacktestRun_DataErrorStopsRun(t *testing.T) {
	boom := errors.New("bad data")
	strat := &scriptStrategy{}
	h := &recordHandler{}

	err := (&backtest{}).run(context.Background(), seq(closes(1, 2, 3), 2, boom), strat, h)

	require.ErrorIs(t, err, boom)
	assert.Len(t, strat.seen, 2, "bars before the error are processed, none after")
}

func TestBacktestRun_ContextCancelled(t *testing.T) {
	md := &marketData{data: closes(1, 2, 3, 4)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	strat := &scriptStrategy{}
	h := &cancelAfterFirst{cancel: cancel}

	err := (&backtest{}).run(ctx, md.bars(ctx), strat, h)

	require.ErrorIs(t, err, context.Canceled)
	assert.Len(t, strat.seen, 1)
}

type cancelAfterFirst struct{ cancel context.CancelFunc }

func (c *cancelAfterFirst) handleIntent(context.Context, intent) error {
	c.cancel()
	return nil
}

// captureLogs routes the default slog logger to a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestBacktestRun_ExpectedRejectionIsLoggedAndRunContinues(t *testing.T) {
	logs := captureLogs(t)
	strat := &scriptStrategy{intents: []intent{
		{instrument: "EUR/USD", direction: long, action: open},
	}}
	h := &recordHandler{failAt: 1, err: fmt.Errorf("open: %w", errInsufficientMargin)}

	err := (&backtest{}).run(context.Background(), seq(closes(1, 2, 3), -1, nil), strat, h)

	require.NoError(t, err, "a margin rejection must not stop the run")
	assert.Len(t, strat.seen, 3, "bars after the rejection are still processed")
	assert.Len(t, h.got, 3)

	out := logs.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "intent rejected")
	assert.Contains(t, out, "insufficient margin available")
	assert.Contains(t, out, "time=2010-01-04")
	assert.Contains(t, out, "instrument=EUR/USD")
	assert.Contains(t, out, "action=open")
	assert.Contains(t, out, "direction=long")
}

func TestBacktestRun_FatalHandlerErrorIsNotLoggedAsRejection(t *testing.T) {
	logs := captureLogs(t)
	h := &recordHandler{failAt: 1, err: errors.New("broker down")}

	err := (&backtest{}).run(context.Background(), seq(closes(1, 2), -1, nil), &scriptStrategy{}, h)

	require.Error(t, err)
	assert.NotContains(t, logs.String(), "intent rejected")
}

func TestBacktestRun_AcceptedRequestCallsHandlerOnceWithIntent(t *testing.T) {
	want := intent{instrument: "EUR/USD", direction: short, action: open}
	strat := &scriptStrategy{intents: []intent{want}}
	h := &recordHandler{}

	err := (&backtest{}).run(context.Background(), seq(closes(1), -1, nil), strat, h)

	require.NoError(t, err)
	require.Len(t, h.got, 1)
	assert.Equal(t, want, h.got[0])
}

// rejectFirstOpen rejects only the first open it sees for margin, then
// accepts everything, modelling margin that becomes available later.
type rejectFirstOpen struct {
	rejected bool
	got      []intent
	accepted []int // bar index of each accepted open
	calls    int
}

func (h *rejectFirstOpen) handleIntent(_ context.Context, i intent) error {
	idx := h.calls
	h.calls++
	h.got = append(h.got, i)
	if i.action != open {
		return nil
	}
	if !h.rejected {
		h.rejected = true
		return errInsufficientMargin
	}
	h.accepted = append(h.accepted, idx)
	return nil
}

func countActions(intents []intent, a action) (n int) {
	for _, i := range intents {
		if i.action == a {
			n++
		}
	}
	return n
}

// §2 critical regression: a rejected entry must not become a late fill.
func TestMissedEntry_RejectedOpenIsNotRetried(t *testing.T) {
	captureLogs(t)
	e := newTestEmax(t, 2, 4)
	h := &rejectFirstOpen{}

	// Bearish, one bullish cross at index 5, then bullish for many bars while
	// margin "becomes available" (the handler accepts any later open).
	bars := closes(10, 9, 8, 7, 6, 12, 14, 16, 18, 20, 22, 24, 26)
	err := (&backtest{}).run(context.Background(), seq(bars, -1, nil), e, h)

	require.NoError(t, err)
	require.Len(t, h.got, len(bars), "handler sees one intent per bar")
	assert.Equal(t, open, h.got[5].action, "the one entry opportunity")
	assert.True(t, h.rejected)
	assert.Equal(t, 1, countActions(h.got, open), "no second open while still bullish")
	assert.Empty(t, h.accepted, "no late entry once margin is available")
	for i := 6; i < len(h.got); i++ {
		assert.Equal(t, hold, h.got[i].action, "bar %d", i)
	}
}

// §2: a rejected entry must not suppress re-entry forever.
func TestMissedEntry_FreshSignalAfterRejectionIsAllowed(t *testing.T) {
	captureLogs(t)
	e := newTestEmax(t, 2, 4)
	h := &rejectFirstOpen{}

	// bullish cross (rejected), bearish cross, then a fresh bullish cross.
	bars := closes(10, 9, 8, 7, 6, 12, 14, 16, 8, 4, 2, 1, 14, 20, 30)
	err := (&backtest{}).run(context.Background(), seq(bars, -1, nil), e, h)

	require.NoError(t, err)
	require.Equal(t, 2, countActions(h.got, open), "intents: %v", h.got)
	assert.Equal(t, open, h.got[5].action, "first, rejected, opportunity")
	require.Len(t, h.accepted, 1, "the fresh opportunity is accepted")
	assert.Greater(t, h.accepted[0], 5)
}
