package signalreplay

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
)

// ── test fixtures / helpers ──────────────────────────────────────────────

// fakeCtx is a minimal strategy.StrategyContext backed by a real LotBook so
// tests can simulate lots opening and closing across bars.
type fakeCtx struct {
	instrument string
	lots       *execution.LotBook
}

func newFakeCtx(instrument string) *fakeCtx {
	return &fakeCtx{instrument: instrument, lots: &execution.LotBook{}}
}

func (f *fakeCtx) Instrument() string         { return f.instrument }
func (f *fakeCtx) OpenLots() strategy.LotView { return f.lots }

func (f *fakeCtx) openLot(id string, side types.Side) {
	_ = f.lots.Add(&execution.Lot{
		TradeCommon: &execution.TradeCommon{ID: id, Instrument: f.instrument, Side: side},
		State:       execution.LotOpen,
	})
}

func (f *fakeCtx) closeLot(id string) {
	f.lots.Delete(id)
}

func candleAt(ts int64) *market.Candle {
	return &market.Candle{Open: 100, High: 101, Low: 99, Close: 100, Timestamp: types.Timestamp(ts)}
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func mustRow(dateStr, pair, bias string) signalRow {
	t, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		panic(err)
	}
	var side types.Side
	switch bias {
	case "long":
		side = types.Long
	case "short":
		side = types.Short
	}
	return signalRow{Date: t, Instrument: market.NormalizeInstrument(pair), Bias: side}
}

// ── compileEpisodes ──────────────────────────────────────────────────────

func TestCompileEpisodes_MergesWithinGap(t *testing.T) {
	t.Parallel()
	rows := []signalRow{
		mustRow("2024-01-02T00:00:00Z", "EURUSD", "long"),
		mustRow("2024-01-03T00:00:00Z", "EURUSD", "long"),
	}
	eps := compileEpisodes(rows, 5)
	require.Len(t, eps, 1)
	assert.Equal(t, types.Long, eps[0].Bias)
	assert.True(t, eps[0].FirstDate.Equal(day(2024, 1, 2)))
	assert.True(t, eps[0].LastDate.Equal(day(2024, 1, 3)))
}

func TestCompileEpisodes_BoundaryGapExactlyMerges(t *testing.T) {
	t.Parallel()
	rows := []signalRow{
		mustRow("2024-01-03T00:00:00Z", "EURUSD", "long"),
		mustRow("2024-01-08T00:00:00Z", "EURUSD", "long"), // exactly 5 days later
	}
	eps := compileEpisodes(rows, 5)
	require.Len(t, eps, 1, "gap exactly equal to episode-gap must merge")
	assert.True(t, eps[0].LastDate.Equal(day(2024, 1, 8)))
}

func TestCompileEpisodes_GapOverThresholdSplits(t *testing.T) {
	t.Parallel()
	rows := []signalRow{
		mustRow("2024-01-03T00:00:00Z", "EURUSD", "long"),
		mustRow("2024-01-09T00:00:00Z", "EURUSD", "long"), // 6 days later
	}
	eps := compileEpisodes(rows, 5)
	require.Len(t, eps, 2, "gap exceeding episode-gap must split into a new episode")
}

func TestCompileEpisodes_BiasFlipAlwaysSplits(t *testing.T) {
	t.Parallel()
	rows := []signalRow{
		mustRow("2024-01-02T00:00:00Z", "EURUSD", "long"),
		mustRow("2024-01-03T00:00:00Z", "EURUSD", "short"), // 1 day later, opposite bias
	}
	eps := compileEpisodes(rows, 5)
	require.Len(t, eps, 2)
	assert.Equal(t, types.Long, eps[0].Bias)
	assert.Equal(t, types.Short, eps[1].Bias)
}

func TestCompileEpisodes_UnsortedInputSortedFirst(t *testing.T) {
	t.Parallel()
	rows := []signalRow{
		mustRow("2024-01-04T00:00:00Z", "EURUSD", "long"),
		mustRow("2024-01-02T00:00:00Z", "EURUSD", "long"),
	}
	eps := compileEpisodes(rows, 5)
	require.Len(t, eps, 1)
	assert.True(t, eps[0].FirstDate.Equal(day(2024, 1, 2)))
	assert.True(t, eps[0].LastDate.Equal(day(2024, 1, 4)))
}

// ── CSV parsing / instrument normalization ───────────────────────────────

func TestLoadSignalRows_RealSweepHeaderFixture(t *testing.T) {
	t.Parallel()
	rows, err := loadSignalRows("testdata/sweep_fixture.csv")
	require.NoError(t, err)
	// Non-tradeable buckets (hot/watch) and the WEEK% > 100% / non-ASCII
	// glyph columns must not break parsing; only BUCKET/DATE/PAIR/BIAS are
	// consumed.
	assert.Len(t, rows, 16)
	for _, r := range rows {
		assert.True(t, r.Bias == types.Long || r.Bias == types.Short)
		assert.NotContains(t, r.Instrument, "_")
	}
}

func TestLoadSignalRows_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := loadSignalRows("testdata/does-not-exist.csv")
	assert.Error(t, err)
}

func TestLoadSignalRows_MissingRequiredColumn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/bad.csv"
	require.NoError(t, writeFile(path, "DATE,PAIR,BIAS\n2024-01-02T00:00:00Z,EURUSD,long\n"))
	_, err := loadSignalRows(path)
	assert.ErrorContains(t, err, "BUCKET")
}

func TestLoadSignalRows_InstrumentNormalization(t *testing.T) {
	t.Parallel()
	rows, err := loadSignalRows("testdata/sweep_fixture.csv")
	require.NoError(t, err)
	found := false
	for _, r := range rows {
		if r.Instrument == "EURUSD" {
			found = true
		}
	}
	assert.True(t, found, "EUR_USD in CSV must normalize to EURUSD")
}

// ── build() / New() validation ────────────────────────────────────────────

func TestBuild_RequiresSignalsParam(t *testing.T) {
	t.Parallel()
	_, err := build(map[string]any{})
	assert.ErrorContains(t, err, "signals")
}

func TestBuild_FailsFastOnMissingSignalsFile(t *testing.T) {
	t.Parallel()
	_, err := build(map[string]any{"signals": "testdata/does-not-exist.csv"})
	assert.Error(t, err)
}

func TestBuild_DefaultsApplied(t *testing.T) {
	t.Parallel()
	s, err := build(map[string]any{"signals": "testdata/sweep_fixture.csv"})
	require.NoError(t, err)
	impl := s.(*Strategy)
	assert.Equal(t, "next-open", impl.cfg.Entry)
	assert.Equal(t, 5, impl.cfg.EpisodeGapDays)
	assert.Equal(t, 0, impl.cfg.MaxHoldDays)
	assert.True(t, impl.cfg.CloseOnFlip)
	assert.True(t, impl.cfg.OnePerEpisode)
}

func TestNew_RejectsUnsupportedEntryMode(t *testing.T) {
	t.Parallel()
	_, err := New(Config{SignalsPath: "x.csv", Entry: "rejection-candle"})
	assert.ErrorContains(t, err, "entry mode")
}

func TestNew_RejectsNegativeEpisodeGap(t *testing.T) {
	t.Parallel()
	_, err := New(Config{SignalsPath: "x.csv", EpisodeGapDays: -1})
	assert.ErrorContains(t, err, "episode-gap")
}

// ── Update() behavior ─────────────────────────────────────────────────────

func TestUpdate_NilCandle_ReturnsSafely(t *testing.T) {
	t.Parallel()
	s, err := New(Config{SignalsPath: "testdata/sweep_fixture.csv"})
	require.NoError(t, err)
	sig := s.Update(context.Background(), nil, newFakeCtx("EURUSD"))
	assert.Equal(t, types.Flat, sig.Side)
}

func TestUpdate_EntersOnFirstBarAfterSignalDate(t *testing.T) {
	t.Parallel()
	s, err := New(Config{SignalsPath: "testdata/sweep_fixture.csv"})
	require.NoError(t, err)
	ctx := newFakeCtx("EURUSD")

	signalDate := day(2024, 1, 2).Unix()

	// Bar exactly at the signal date: must not enter yet ("strictly after").
	sig := s.Update(context.Background(), candleAt(signalDate), ctx)
	assert.Equal(t, types.Flat, sig.Side)
	assert.True(t, s.Ready())

	// Bar strictly after the signal date: entry fires.
	sig = s.Update(context.Background(), candleAt(signalDate+3600), ctx)
	assert.Equal(t, types.Long, sig.Side)
	assert.Equal(t, "signalreplay:2024-01-02T00:00:00Z", sig.Reason)
	assert.False(t, sig.CloseAll)
}

func TestUpdate_NoReentryWhileLotOpen(t *testing.T) {
	t.Parallel()
	s, err := New(Config{SignalsPath: "testdata/sweep_fixture.csv"})
	require.NoError(t, err)
	ctx := newFakeCtx("EURUSD")

	signalDate := day(2024, 1, 2).Unix()
	s.Update(context.Background(), candleAt(signalDate), ctx) // warm/load
	sig := s.Update(context.Background(), candleAt(signalDate+3600), ctx)
	require.Equal(t, types.Long, sig.Side)

	// Simulate the fill: a lot is now open.
	ctx.openLot("lot-1", types.Long)

	// Same episode is still within its window; because it's the same bias
	// and a lot is open, no new entry/close is emitted.
	sig = s.Update(context.Background(), candleAt(signalDate+7200), ctx)
	assert.Equal(t, types.Flat, sig.Side)
	assert.False(t, sig.CloseAll)
}

func TestUpdate_CloseAllOnBiasFlip(t *testing.T) {
	t.Parallel()
	s, err := New(Config{SignalsPath: "testdata/sweep_fixture.csv", CloseOnFlip: true})
	require.NoError(t, err)
	// Use the short episode starting 2024-01-22 for EURUSD in the fixture.
	ctx := newFakeCtx("EURUSD")

	// Load episodes and open a long lot directly (simulating a fill from an
	// earlier long episode) without going through the strategy's own entry.
	s.ensureLoaded(ctx.Instrument())
	require.NoError(t, s.loadErr)
	ctx.openLot("lot-1", types.Long)

	// Advance idx to the short episode (2024-01-22) by skipping past the
	// earlier long episodes.
	for i, ep := range s.episodes {
		if ep.Bias == types.Short {
			s.idx = i
			break
		}
	}
	shortStart := s.episodes[s.idx].FirstDate.Unix()

	sig := s.Update(context.Background(), candleAt(shortStart+3600), ctx)
	assert.Equal(t, types.Short, sig.Side)
	assert.True(t, sig.CloseAll)
	assert.Equal(t, "signalreplay:2024-01-22T00:00:00Z", sig.Reason)
}

func TestUpdate_NoFlipCloseWhenDisabled(t *testing.T) {
	t.Parallel()
	s, err := New(Config{SignalsPath: "testdata/sweep_fixture.csv", CloseOnFlip: false})
	require.NoError(t, err)
	ctx := newFakeCtx("EURUSD")
	s.ensureLoaded(ctx.Instrument())
	require.NoError(t, s.loadErr)
	ctx.openLot("lot-1", types.Long)

	for i, ep := range s.episodes {
		if ep.Bias == types.Short {
			s.idx = i
			break
		}
	}
	shortStart := s.episodes[s.idx].FirstDate.Unix()

	sig := s.Update(context.Background(), candleAt(shortStart+3600), ctx)
	assert.Equal(t, types.Flat, sig.Side)
	assert.False(t, sig.CloseAll)
}

func TestUpdate_TimeStopAfterMaxHoldDays(t *testing.T) {
	t.Parallel()
	s, err := New(Config{SignalsPath: "testdata/sweep_fixture.csv", MaxHoldDays: 2})
	require.NoError(t, err)
	ctx := newFakeCtx("EURUSD")
	s.ensureLoaded(ctx.Instrument())
	require.NoError(t, s.loadErr)
	ctx.openLot("lot-1", types.Long)

	base := day(2024, 1, 2).Unix()
	// Bar 1 with lot open: barsInPosition -> 1, below threshold.
	sig := s.Update(context.Background(), candleAt(base+86400), ctx)
	assert.Equal(t, types.Flat, sig.Side)
	assert.False(t, sig.CloseAll)

	// Bar 2 with lot open: barsInPosition -> 2, meets threshold, time-stop fires.
	sig = s.Update(context.Background(), candleAt(base+2*86400), ctx)
	assert.True(t, sig.CloseAll)
	assert.Equal(t, types.Flat, sig.Side)
}

func TestUpdate_OnePerEpisode_NoReentryAfterClose(t *testing.T) {
	t.Parallel()
	s, err := New(Config{SignalsPath: "testdata/sweep_fixture.csv", OnePerEpisode: true})
	require.NoError(t, err)
	ctx := newFakeCtx("EURUSD")

	signalDate := day(2024, 1, 2).Unix()
	s.Update(context.Background(), candleAt(signalDate), ctx) // load
	sig := s.Update(context.Background(), candleAt(signalDate+3600), ctx)
	require.Equal(t, types.Long, sig.Side)

	// Simulate a stop-out: lot closes before the next episode activates.
	ctx.openLot("lot-1", types.Long)
	ctx.closeLot("lot-1")

	// Still within the first episode's original window; must not re-enter
	// because OnePerEpisode already consumed it.
	sig = s.Update(context.Background(), candleAt(signalDate+7200), ctx)
	assert.Equal(t, types.Flat, sig.Side)
}

func TestReset_ClearsCursorKeepsEpisodes(t *testing.T) {
	t.Parallel()
	s, err := New(Config{SignalsPath: "testdata/sweep_fixture.csv", OnePerEpisode: true})
	require.NoError(t, err)
	ctx := newFakeCtx("EURUSD")

	signalDate := day(2024, 1, 2).Unix()
	s.Update(context.Background(), candleAt(signalDate), ctx)
	s.Update(context.Background(), candleAt(signalDate+3600), ctx)
	require.Greater(t, s.idx, 0)

	episodesBefore := len(s.episodes)
	s.Reset()
	assert.Equal(t, 0, s.idx)
	assert.Equal(t, 0, s.barsInPosition)
	assert.Len(t, s.episodes, episodesBefore, "episode list must survive Reset")
	assert.True(t, s.Ready(), "loaded episode cache must survive Reset")
}

func TestReady_FalseUntilFirstUpdate(t *testing.T) {
	t.Parallel()
	s, err := New(Config{SignalsPath: "testdata/sweep_fixture.csv"})
	require.NoError(t, err)
	assert.False(t, s.Ready())
	s.Update(context.Background(), candleAt(day(2024, 1, 2).Unix()), newFakeCtx("EURUSD"))
	assert.True(t, s.Ready())
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
