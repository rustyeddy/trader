package backtest

// This file is intentionally package backtest (internal), not
// backtest_test: it needs direct access to this package's own
// unexported composition-root helpers (environmentFactory,
// nextBarOpenPriceSource) to drive strategy/smatrend through
// service/backtest exactly the way run.go itself does for the
// --config/EMA-crossover path, so the in-tree side of this comparison
// is built the identical way a real "trader backtest run" invocation
// would build it — not a simplified stand-in.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/strategy/external"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
	"github.com/rustyeddy/trader/strategy/smatrend"
)

// smaLongHoldBuildOnce/smaLongHoldPath build examples/sma-long-hold
// once for every test in this file — this file has its own build step
// rather than reusing external_strategy_test.go's TestMain (package
// backtest_test, a different Go package sharing this directory) since
// a test binary may define only one TestMain across every _test.go
// file in a directory, internal and external test packages alike.
var (
	smaLongHoldBuildOnce sync.Once
	smaLongHoldPath      string
	smaLongHoldBuildErr  error
)

func buildSMALongHoldBinary(t *testing.T) string {
	t.Helper()
	smaLongHoldBuildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "sma-long-hold-bin-*")
		if err != nil {
			smaLongHoldBuildErr = err
			return
		}
		out := filepath.Join(dir, "sma-long-hold")
		build := exec.Command("go", "build", "-o", out, "github.com/rustyeddy/trader/examples/sma-long-hold")
		build.Stdout = os.Stdout
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			smaLongHoldBuildErr = err
			return
		}
		smaLongHoldPath = out
	})
	require.NoError(t, smaLongHoldBuildErr)
	return smaLongHoldPath
}

// smaLongHoldEquivalenceListing mirrors run_test.go's own
// newGappedFixtureManager pattern: a real registered EUR/USD listing
// on both the "oanda" (historical) and "sim" (broker/fill) providers,
// exactly the two-resolver split run.go's own resolveInstrumentSet
// maintains for every real CLI invocation.
func smaLongHoldEquivalenceListing(t *testing.T, provider string) instrument.Listing {
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
		Provider:   provider,
		Symbol:     "EURUSD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// smaLongHoldEquivalenceParams is the one configuration both sides of
// this equivalence comparison must agree on byte-for-byte: SMA
// period, trailing stop percent, instrument, and interval.
type smaLongHoldEquivalenceParams struct {
	smaPeriod           int
	trailingStopPercent string
	span                marketdata.TimeRange
	startingCapital     num.Money
	riskFraction        num.Rate
	adverseDistance     num.Price
}

// defaultSMALongHoldEquivalenceParams uses a small, deliberately
// engineered fixture (testdata/raw/oanda/EURUSD/2024/06) rather than
// the repo's other, organically-sourced EURUSD fixtures: real OANDA
// data from those months happens to stay continuously above its own
// short-period SMA once warmed up (a strong, smooth uptrend), so
// "fresh-cross" — the exact default this comparison must exercise —
// never actually fires and this test would trivially compare two
// empty runs. This fixture instead engineers a real below-SMA warm-up,
// a sharp cross-above (bar 9) triggering entry, three bars of rising
// highs ratcheting the trailing stop upward (bars 10-12), then a sharp
// reversal (bar 13) whose Low breaches the ratcheted stop, closing the
// position via the real broker-side resting-order mechanism (ADR-026)
// — a genuine, non-trivial entry/ratchet/stop-out episode.
func defaultSMALongHoldEquivalenceParams(t *testing.T) smaLongHoldEquivalenceParams {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.June, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.June, 3, 16, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return smaLongHoldEquivalenceParams{
		smaPeriod:           5,
		trailingStopPercent: "0.02",
		span:                span,
		startingCapital:     num.MustParseMoney("10000", num.MustParseCurrency("USD")),
		riskFraction:        num.MustParseRate("0.10"),
		adverseDistance:     num.MustParsePrice("0.01000"),
	}
}

// runInTreeSMATrend drives strategy/smatrend through service/backtest
// exactly like run.go's own --config path does, configured to
// smatrend's own EQS-01 defaults (trailing-stop/fresh-cross/
// fresh-cross) — the same defaults examples/sma-long-hold reproduces.
func runInTreeSMATrend(t *testing.T, manager *marketdata.Manager, simResolver instrument.Resolver, simListing instrument.Listing, p smaLongHoldEquivalenceParams) svcbacktest.RunResponse {
	t.Helper()
	ctx := t.Context()

	interval := marketdata.H1
	trailingStopPercent, err := num.ParseRate(p.trailingStopPercent)
	require.NoError(t, err)

	strat, err := smatrend.New(simListing.InstrumentID(), interval, smatrend.Config{
		SMAPeriod:           p.smaPeriod,
		TrailingStopPercent: trailingStopPercent,
	})
	require.NoError(t, err)

	src := newNextBarOpenPriceSource()
	require.NoError(t, src.load(ctx, manager, simListing.Symbol(), marketdata.BarQuery{
		Instrument: simListing.InstrumentID(), Interval: interval, Range: p.span,
	}))

	factory := environmentFactory{prices: src}
	svc, err := svcbacktest.New(manager, simResolver, factory, nil)
	require.NoError(t, err)

	resp, err := svc.Run(ctx, svcbacktest.RunRequest{
		Strategy:           strat,
		StrategyParameters: strat.Config(),
		Span:               p.span,
		StartingCapital:    p.startingCapital,
		RiskFraction:       p.riskFraction,
		AdverseDistance:    p.adverseDistance,
	})
	require.NoError(t, err)
	return resp
}

// runExternalSMALongHold drives examples/sma-long-hold through the
// identical service/backtest composition, but launched as a real
// out-of-process guest via adapters/strategy/external.Launch — the
// same construction run.go's own --strategy-exec branch performs,
// built directly here (rather than through the CLI's own text/JSON
// rendering) so its RunResponse can be compared field-for-field
// against runInTreeSMATrend's, with no serialization round trip in
// between.
func runExternalSMALongHold(t *testing.T, manager *marketdata.Manager, simResolver instrument.Resolver, simListing instrument.Listing, p smaLongHoldEquivalenceParams) svcbacktest.RunResponse {
	t.Helper()
	ctx := t.Context()

	interval := marketdata.H1

	configPath := filepath.Join(t.TempDir(), "sma-long-hold.json")
	configJSON, err := json.Marshal(map[string]any{
		"base": "EUR", "quote": "USD",
		"interval_unit": "hour", "interval_count": 1,
		"sma_period":            p.smaPeriod,
		"trailing_stop_percent": p.trailingStopPercent,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, configJSON, 0o600))

	process, err := external.Launch(ctx, external.LaunchConfig{
		Command: buildSMALongHoldBinary(t),
		Env:     append(os.Environ(), "TRADER_STRATEGY_CONFIG="+configPath),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = process.Stop(t.Context()) })

	src := newNextBarOpenPriceSource()
	require.NoError(t, src.load(ctx, manager, simListing.Symbol(), marketdata.BarQuery{
		Instrument: simListing.InstrumentID(), Interval: interval, Range: p.span,
	}))

	factory := environmentFactory{prices: src}
	svc, err := svcbacktest.New(manager, simResolver, factory, nil)
	require.NoError(t, err)

	resp, err := svc.Run(ctx, svcbacktest.RunRequest{
		Strategy:        process.Strategy(),
		Span:            p.span,
		StartingCapital: p.startingCapital,
		RiskFraction:    p.riskFraction,
		AdverseDistance: p.adverseDistance,
	})
	require.NoError(t, err)
	return resp
}

// tradeKey is a trade's own economically meaningful content, with
// every identifier (AccountID, FillIDs) deliberately excluded — the
// in-tree and external runs use independently generated IDs and are
// never expected to agree on those, only on what actually happened.
// Instrument is included (review finding): omitting it would let two
// runs that happened to agree on every other field but traded
// different instruments still compare equal, which is not what
// "identical results" claims.
type tradeKey struct {
	Instrument  string
	Side        order.PositionSide
	OpenedAt    time.Time
	ClosedAt    time.Time
	RealizedPnL string
	Costs       string
}

func tradeKeys(trades []order.Trade) []tradeKey {
	keys := make([]tradeKey, len(trades))
	for i, tr := range trades {
		keys[i] = tradeKey{
			Instrument:  tr.Listing.InstrumentID().String(),
			Side:        tr.Side,
			OpenedAt:    tr.OpenedAt.UTC(),
			ClosedAt:    tr.ClosedAt.UTC(),
			RealizedPnL: tr.RealizedPnL.String(),
			Costs:       tr.Costs.String(),
		}
	}
	return keys
}

// positionKey is one open account.Position's own economically
// meaningful content — instrument, side, quantity, and average
// price, the same "no IDs, but everything that describes what is
// actually held" discipline tradeKey follows (review finding: the
// original version of this test compared only the *number* of open
// positions, which two runs holding different instruments/sides/
// sizes at the same count could still pass).
type positionKey struct {
	Instrument string
	Side       order.PositionSide
	Quantity   string
	AvgPrice   string
}

func positionKeys(positions []order.Position) []positionKey {
	keys := make([]positionKey, len(positions))
	for i, p := range positions {
		var avgPrice string
		if p.AvgPrice != nil {
			avgPrice = p.AvgPrice.String()
		}
		keys[i] = positionKey{
			Instrument: p.Listing.InstrumentID().String(),
			Side:       p.Side,
			Quantity:   p.Quantity.String(),
			AvgPrice:   avgPrice,
		}
	}
	return keys
}

// TestSMALongHold_EquivalentToInTreeSMATrendDefaultConfig is issue
// #383's own core acceptance criterion: examples/sma-long-hold,
// driven as a real out-of-process guest, produces the identical
// trades and final account state as strategy/smatrend's own default
// configuration (trailing-stop/fresh-cross/fresh-cross) over
// identical canonical market data — proving Strategy Protocol v1 with
// a real, non-trivial strategy, not merely that a trivial guest can
// complete a Handshake.
func TestSMALongHold_EquivalentToInTreeSMATrendDefaultConfig(t *testing.T) {
	params := defaultSMALongHoldEquivalenceParams(t)

	oandaResolver := instrument.NewMemoryResolver()
	require.NoError(t, oandaResolver.Register(smaLongHoldEquivalenceListing(t, "oanda")))
	simResolver := instrument.NewMemoryResolver()
	simListing := smaLongHoldEquivalenceListing(t, "sim")
	require.NoError(t, simResolver.Register(simListing))

	manager, err := marketdata.New(marketdata.Config{
		Clock:        clock.Real{},
		StoreRoot:    t.TempDir(),
		RawRoot:      "testdata/raw/oanda",
		Resolver:     oandaResolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	ctx := t.Context()
	plan, err := manager.Plan(ctx, marketdata.BarQuery{Instrument: simListing.InstrumentID(), Interval: marketdata.H1, Range: params.span})
	require.NoError(t, err)
	if len(plan.Actions) > 0 {
		_, err = manager.Build(ctx, plan)
		require.NoError(t, err)
	}

	inTree := runInTreeSMATrend(t, manager, simResolver, simListing, params)
	external := runExternalSMALongHold(t, manager, simResolver, simListing, params)

	// A meaningful equivalence proof requires the scenario to have
	// actually traded — an equivalence assertion over two runs that
	// both did nothing at all would pass trivially without proving
	// anything about OnBar/intent-translation correctness.
	require.NotEmpty(t, inTree.Trades, "the in-tree fixture/config must produce at least one closed trade for this comparison to be meaningful")

	assert.Equal(t, tradeKeys(inTree.Trades), tradeKeys(external.Trades),
		"closed trades must be identical between the in-tree and external runs")
	assert.Equal(t, tradeKeys(inTree.OpenTrades), tradeKeys(external.OpenTrades),
		"any still-open trade must be identical between the in-tree and external runs")

	assert.True(t, inTree.Account.Equity().Equal(external.Account.Equity()),
		"final account equity must match: in-tree %s, external %s", inTree.Account.Equity(), external.Account.Equity())
	assert.True(t, inTree.Account.RealizedPnL().Equal(external.Account.RealizedPnL()),
		"final realized PnL must match: in-tree %s, external %s", inTree.Account.RealizedPnL(), external.Account.RealizedPnL())
	assert.Equal(t, positionKeys(inTree.Account.Positions()), positionKeys(external.Account.Positions()),
		"final open positions must be identical, not merely the same count")
}
