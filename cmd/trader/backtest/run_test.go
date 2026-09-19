package backtest

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
)

// TestNextBarOpenAfterEntry_UsesFollowingBarOpenNotEntryBarClose is the
// PR #240 review regression: the committed vertical-slice fixture
// happens to have entry-bar-close == fill-bar-open (a continuous
// synthetic series), which let an earlier version of this file's bug
// — pricing every fill at the entry bar's own Close — pass unnoticed.
// This fixture (testdata/raw/oanda/EURUSD/2024/02) is deliberately
// gapped: 2024-02-01T00:00's bid close (1.20010) differs from
// 2024-02-01T01:00's bid open (1.21000), so a wrong implementation
// returning the entry bar's Close is distinguishable from the correct
// next-bar-open value by more than floating-point noise.
func TestNextBarOpenAfterEntry_UsesFollowingBarOpenNotEntryBarClose(t *testing.T) {
	manager, instrumentID := newGappedFixtureManager(t)
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.February, 1, 3, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// Ground truth, read independently of the function under test.
	reader, err := manager.Bars(ctx, marketdata.BarQuery{Instrument: instrumentID, Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	entryBar, err := reader.Next(ctx)
	require.NoError(t, err)
	fillBar, err := reader.Next(ctx)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.False(t, entryBar.Close.Equal(fillBar.Open), "fixture must be gapped for this regression to be meaningful")

	got, err := nextBarOpenAfterEntry(ctx, manager, instrumentID, marketdata.H1, span, 0)
	require.NoError(t, err)

	require.True(t, got.Equal(fillBar.Open), "expected the fill bar's own Open (%s), got %s", fillBar.Open, got)
	require.False(t, got.Equal(entryBar.Close), "must not price the fill at the entry bar's Close (%s)", entryBar.Close)
}

// TestNextBarOpenAfterEntry_AccountsForWarmupBars proves warmupBars
// shifts both the entry bar and the fill bar forward by the same
// amount: with warmupBars=1, the strategy's first delivered OnBar is
// index 1 (2024-02-01T01:00), so its fill bar is index 2
// (2024-02-01T02:00).
func TestNextBarOpenAfterEntry_AccountsForWarmupBars(t *testing.T) {
	manager, instrumentID := newGappedFixtureManager(t)
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.February, 1, 3, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	ctx := context.Background()

	reader, err := manager.Bars(ctx, marketdata.BarQuery{Instrument: instrumentID, Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	_, err = reader.Next(ctx) // index 0, consumed as warm-up
	require.NoError(t, err)
	_, err = reader.Next(ctx) // index 1, the entry bar with warmupBars=1
	require.NoError(t, err)
	fillBar, err := reader.Next(ctx) // index 2, the expected fill bar
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	got, err := nextBarOpenAfterEntry(ctx, manager, instrumentID, marketdata.H1, span, 1)
	require.NoError(t, err)
	require.True(t, got.Equal(fillBar.Open))
}

// newGappedFixtureManager returns a *marketdata.Manager over
// testdata/raw/oanda's deliberately gapped February fixture.
func newGappedFixtureManager(t *testing.T) (*marketdata.Manager, instrument.ID) {
	t.Helper()

	eurusd, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: eurusd,
		Provider:   "oanda",
		Symbol:     "EURUSD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)

	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(listing))

	manager, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    t.TempDir(),
		RawRoot:      "testdata/raw/oanda",
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.February, 1, 3, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	ctx := context.Background()
	plan, err := manager.Plan(ctx, marketdata.BarQuery{Instrument: listing.InstrumentID(), Interval: marketdata.H1, Range: span})
	require.NoError(t, err)
	if len(plan.Actions) > 0 {
		_, err = manager.Build(ctx, plan)
		require.NoError(t, err)
	}

	return manager, listing.InstrumentID()
}

// TestBuildExternalLaunchConfig_InheritsEnvironAndAppendsConfigPath is
// the review regression for --strategy-exec's own env-inheritance
// finding: the child's LaunchConfig.Env must start from the
// operator's own environment, not a bare one containing only
// strategyConfigPathEnv, and a relative --strategy-config path must
// be resolved to an absolute one before being recorded/forwarded.
func TestBuildExternalLaunchConfig_InheritsEnvironAndAppendsConfigPath(t *testing.T) {
	environ := []string{"PATH=/usr/bin:/bin", "HOME=/home/op"}

	cfg, resolvedExec, abs, err := buildExternalLaunchConfig(runFlags{
		strategyExec:   "/bin/true",
		strategyConfig: "strategy.yaml",
	}, environ, nil)
	require.NoError(t, err)

	require.Equal(t, "/bin/true", resolvedExec)
	require.Equal(t, resolvedExec, cfg.Command, "LaunchConfig.Command must be the exact resolved path this function itself returns")

	require.Contains(t, cfg.Env, "PATH=/usr/bin:/bin")
	require.Contains(t, cfg.Env, "HOME=/home/op")

	require.True(t, filepath.IsAbs(abs), "expected an absolute config path, got %q", abs)
	require.Contains(t, cfg.Env, strategyConfigPathEnv+"="+abs)
}

// TestBuildExternalLaunchConfig_ReplacesPreexistingConfigEnvEntry
// proves a strategyConfigPathEnv entry already present in the
// operator's own environment is replaced, never duplicated — a
// duplicate key's effective value at process-launch time is undefined.
func TestBuildExternalLaunchConfig_ReplacesPreexistingConfigEnvEntry(t *testing.T) {
	environ := []string{"PATH=/usr/bin", strategyConfigPathEnv + "=/stale/path.yaml"}

	cfg, _, abs, err := buildExternalLaunchConfig(runFlags{
		strategyExec:   "/bin/true",
		strategyConfig: "strategy.yaml",
	}, environ, nil)
	require.NoError(t, err)

	var matches int
	for _, kv := range cfg.Env {
		if strings.HasPrefix(kv, strategyConfigPathEnv+"=") {
			matches++
			require.Equal(t, strategyConfigPathEnv+"="+abs, kv)
		}
	}
	require.Equal(t, 1, matches, "expected exactly one %s entry, got %d in %v", strategyConfigPathEnv, matches, cfg.Env)
}

// TestBuildExternalLaunchConfig_NoConfigLeavesEnvironUntouched proves
// the no-config case still inherits environ verbatim (minus a filter
// that has nothing to remove) and returns no resolved path.
func TestBuildExternalLaunchConfig_NoConfigLeavesEnvironUntouched(t *testing.T) {
	environ := []string{"PATH=/usr/bin", "HOME=/home/op"}

	cfg, resolvedExec, abs, err := buildExternalLaunchConfig(runFlags{strategyExec: "/bin/true"}, environ, nil)
	require.NoError(t, err)
	require.Equal(t, "/bin/true", resolvedExec)
	require.Empty(t, abs)
	require.ElementsMatch(t, environ, cfg.Env)
}

// TestResolveStrategyExecutable_BareNameUsesPATHLookup proves a bare
// command name (no path separator) is resolved via exec.LookPath —
// os/exec's own PATH-search behavior — not filepath.Abs against the
// current directory, which would silently name a different, likely
// nonexistent file (review finding).
func TestResolveStrategyExecutable_BareNameUsesPATHLookup(t *testing.T) {
	want, err := exec.LookPath("true")
	require.NoError(t, err, "this test environment must have 'true' on PATH")

	got, err := resolveStrategyExecutable("true")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestResolveStrategyExecutable_PathSeparatorNeverSearchesPATH proves
// a name containing a path separator is resolved with plain
// filepath.Abs, matching os/exec's own literal-path handling — never
// looked up on PATH, even if a same-named file also happens to exist
// there.
func TestResolveStrategyExecutable_PathSeparatorNeverSearchesPATH(t *testing.T) {
	got, err := resolveStrategyExecutable("./bin/true")
	require.NoError(t, err)
	wantSuffix := string(filepath.Separator) + filepath.Join("bin", "true")
	assert.True(t, strings.HasSuffix(got, wantSuffix), "expected %q to end with %q", got, wantSuffix)
	assert.True(t, filepath.IsAbs(got))
}

// TestResolveStrategyExecutable_UnresolvableBareNameIsAnError proves a
// bare name not found on PATH fails clearly rather than silently
// falling back to a relative-path interpretation.
func TestResolveStrategyExecutable_UnresolvableBareNameIsAnError(t *testing.T) {
	_, err := resolveStrategyExecutable("this-command-should-not-exist-anywhere-on-path")
	require.Error(t, err)
}

// fakeProcessMonitor is a deterministic, controllable processMonitor
// test double — real *external.Process crash timing cannot reliably
// force the exact "both channels ready simultaneously" race
// awaitRunWithProcessMonitor must close (review finding).
type fakeProcessMonitor struct {
	done chan struct{}
	err  error
}

func (f *fakeProcessMonitor) Done() <-chan struct{} { return f.done }
func (f *fakeProcessMonitor) Err() error            { return f.err }

// TestAwaitRunWithProcessMonitor_SimultaneousReadinessStillDetectsCrash
// is the second-round review regression: resultCh already holds a
// successful result AND process.Done() is already closed before
// select ever runs, forcing Go's "choose pseudo-randomly among ready
// cases" behavior to be exercised on every run. The fix must detect
// the crash regardless of which case select happens to pick — run it
// many times to give an unfixed version every realistic chance to
// pick the resultCh arm and slip through.
func TestAwaitRunWithProcessMonitor_SimultaneousReadinessStillDetectsCrash(t *testing.T) {
	crashErr := errors.New("boom")

	for i := 0; i < 200; i++ {
		resultCh := make(chan runResult, 1)
		resultCh <- runResult{resp: svcbacktest.RunResponse{}, err: nil}

		done := make(chan struct{})
		close(done)
		process := &fakeProcessMonitor{done: done, err: crashErr}

		_, err := awaitRunWithProcessMonitor(func() {}, resultCh, process)
		require.Error(t, err, "iteration %d: a process that already exited must not let a simultaneously-ready successful result slip through", i)
		require.ErrorIs(t, err, crashErr)
	}
}

// TestAwaitRunWithProcessMonitor_StillRunningAcceptsSuccess proves the
// ordinary case is unaffected: a successful result with the process
// still running (Done() open) is accepted with no error.
func TestAwaitRunWithProcessMonitor_StillRunningAcceptsSuccess(t *testing.T) {
	resultCh := make(chan runResult, 1)
	resultCh <- runResult{resp: svcbacktest.RunResponse{}, err: nil}

	process := &fakeProcessMonitor{done: make(chan struct{})} // never closed: still running

	_, err := awaitRunWithProcessMonitor(func() {}, resultCh, process)
	require.NoError(t, err)
}
