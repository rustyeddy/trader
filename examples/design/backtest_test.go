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
	seen    []ohlc
}

func (s *scriptStrategy) init(map[string]any) error { return nil }

func (s *scriptStrategy) onBar(_ context.Context, bar ohlc) intent {
	i := len(s.seen)
	s.seen = append(s.seen, bar)
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

func seq(bars []ohlc, errAt int, err error) iter.Seq2[ohlc, error] {
	return func(yield func(ohlc, error) bool) {
		for i, b := range bars {
			if i == errAt {
				yield(ohlc{}, err)
				return
			}
			if !yield(b, nil) {
				return
			}
		}
	}
}

func TestBacktestRun_ProcessesEveryBarInOrder(t *testing.T) {
	bars := closes(1, 2, 3)
	strat := &scriptStrategy{intents: []intent{{action: open}, {}, {action: close}}}
	h := &recordHandler{}

	err := (&backtest{}).run(context.Background(), seq(bars, -1, nil), strat, h)

	require.NoError(t, err)
	assert.Equal(t, bars, strat.seen)
	assert.Equal(t, []intent{{action: open}, {}, {action: close}}, h.got)
}

func TestBacktestRun_EmptyInput(t *testing.T) {
	strat := &scriptStrategy{}
	h := &recordHandler{}

	require.NoError(t, (&backtest{}).run(context.Background(), seq(nil, -1, nil), strat, h))
	assert.Empty(t, strat.seen)
	assert.Empty(t, h.got)
}

func TestBacktestRun_HandlerErrorStopsRun(t *testing.T) {
	boom := errors.New("rejected")
	strat := &scriptStrategy{}
	h := &recordHandler{failAt: 2, err: boom}

	err := (&backtest{}).run(context.Background(), seq(closes(1, 2, 3, 4), -1, nil), strat, h)

	require.ErrorIs(t, err, boom)
	assert.Len(t, strat.seen, 2, "no bars processed after the failure")
	assert.Len(t, h.got, 2)
}

func TestBacktestRun_IteratorErrorPropagates(t *testing.T) {
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

func TestBacktestRun_MarginRejectionIsLoggedAndRunContinues(t *testing.T) {
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
