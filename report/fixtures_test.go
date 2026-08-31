package report_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/tradertest"
)

// newFixtureIDs returns a fresh, independently-seeded id.Generator.
// Each Result-building function below calls this once and threads the
// result through its own helper calls, rather than sharing one
// package-level generator — a shared generator's counter would advance
// differently depending on test execution order and -count, which
// would make golden output (RunID, AccountID, FillID are all embedded
// in it) nondeterministic across otherwise-identical runs (issue #220
// review, point 6: golden tests must be immune to that).
func newFixtureIDs() *id.Generator {
	return id.NewGenerator(clock.NewSimulated(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(11, 12))
}

func fixtureUSD(s string) num.Money {
	return num.MustParseMoney(s, num.MustParseCurrency("USD"))
}

func mustFixtureRunID(t *testing.T, ids *id.Generator) id.RunID {
	t.Helper()
	v, err := id.GenerateRunID(ids)
	require.NoError(t, err)
	return v
}

func mustFixtureAccountID(t *testing.T, ids *id.Generator) id.AccountID {
	t.Helper()
	v, err := id.GenerateAccountID(ids)
	require.NoError(t, err)
	return v
}

func mustFixtureFillID(t *testing.T, ids *id.Generator) id.FillID {
	t.Helper()
	v, err := id.GenerateFillID(ids)
	require.NoError(t, err)
	return v
}

func fixtureEURUSD(t *testing.T) instrument.Listing {
	t.Helper()
	return tradertest.MustNewListing(tradertest.ListingParams{})
}

func fixtureGBPUSD(t *testing.T) instrument.Listing {
	t.Helper()
	return tradertest.MustNewListing(tradertest.ListingParams{Base: "GBP", Symbol: "GBP_USD"})
}

func fixtureSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

func fixtureDatasetManifest(t *testing.T, listing instrument.Listing) marketdata.Manifest {
	t.Helper()
	m := marketdata.Manifest{
		Provider:         "oanda",
		Instrument:       listing.InstrumentID(),
		Interval:         marketdata.H1,
		Span:             fixtureSpan(t),
		Basis:            marketdata.BasisBid,
		SchemaVersion:    1,
		RawFingerprint:   "sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
		BuilderVersion:   "test-builder-v1",
		ValidatorVersion: "test-validator-v1",
		ResamplerVersion: "none",
		CalendarVersion:  "test-calendar-v1",
		BuiltAt:          time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	require.NoError(t, m.Validate())
	return m
}

func fixtureManifest(t *testing.T, ids *id.Generator, universe []strategy.DataRequirement, dataset []marketdata.Manifest) backtest.Manifest {
	t.Helper()
	fillModel, err := backtest.NewComponentInfo("bar-close", "v1", nil)
	require.NoError(t, err)
	slippageModel, err := backtest.NewComponentInfo("none", "", nil)
	require.NoError(t, err)
	commissionModel, err := backtest.NewComponentInfo("fixed", "v1", nil)
	require.NoError(t, err)

	m, err := backtest.NewManifest(backtest.ManifestParams{
		RunID:           mustFixtureRunID(t, ids),
		StrategyName:    "ema-cross",
		StrategyVersion: "1.0.0",
		Universe:        universe,
		Span:            fixtureSpan(t),
		StartingCapital: fixtureUSD("10000"),
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: num.MustParsePrice("0.01000"),
		FillModel:       fillModel,
		SlippageModel:   slippageModel,
		CommissionModel: commissionModel,
		Dataset:         dataset,
		TraderVersion:   "test-v0",
	})
	require.NoError(t, err)
	return m
}

func fixtureTrade(t *testing.T, ids *id.Generator, accountID id.AccountID, listing instrument.Listing, side order.PositionSide, realizedPnL, costs string, openedAt, closedAt time.Time) order.Trade {
	t.Helper()
	tr, err := order.NewTrade(order.Trade{
		AccountID:    accountID,
		Listing:      listing,
		Side:         side,
		EntryFillIDs: []id.FillID{mustFixtureFillID(t, ids)},
		ExitFillIDs:  []id.FillID{mustFixtureFillID(t, ids)},
		OpenedAt:     openedAt,
		ClosedAt:     closedAt,
		RealizedPnL:  fixtureUSD(realizedPnL),
		Costs:        fixtureUSD(costs),
	})
	require.NoError(t, err)
	return tr
}

func fixtureOpenTrade(t *testing.T, ids *id.Generator, accountID id.AccountID, listing instrument.Listing, side order.PositionSide, costs string, openedAt time.Time) order.Trade {
	t.Helper()
	tr, err := order.NewTrade(order.Trade{
		AccountID:    accountID,
		Listing:      listing,
		Side:         side,
		EntryFillIDs: []id.FillID{mustFixtureFillID(t, ids)},
		OpenedAt:     openedAt,
		RealizedPnL:  fixtureUSD("0"),
		Costs:        fixtureUSD(costs),
	})
	require.NoError(t, err)
	return tr
}

// newRepresentativeResult returns a backtest.Result exercising wins,
// losses, multiple instruments, an open trade whose entry cost makes
// AccountFees differ from ClosedTradeCosts, and a mark-to-market
// equity curve with a genuine drawdown — the scenario issue #220's
// review (point 9) asked golden coverage to include.
func newRepresentativeResult(t *testing.T) backtest.Result {
	t.Helper()

	ids := newFixtureIDs()
	eurusd := fixtureEURUSD(t)
	gbpusd := fixtureGBPUSD(t)
	acctID := mustFixtureAccountID(t, ids)

	day1 := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC)

	universe := []strategy.DataRequirement{
		{Instrument: eurusd.InstrumentID(), Interval: marketdata.H1, WarmupBars: 20},
		{Instrument: gbpusd.InstrumentID(), Interval: marketdata.H1, WarmupBars: 20},
	}
	dataset := []marketdata.Manifest{fixtureDatasetManifest(t, eurusd), fixtureDatasetManifest(t, gbpusd)}
	manifest := fixtureManifest(t, ids, universe, dataset)

	trade1 := fixtureTrade(t, ids, acctID, eurusd, order.Long, "150", "5", day1.Add(9*time.Hour), day1.Add(10*time.Hour))
	trade2 := fixtureTrade(t, ids, acctID, eurusd, order.Short, "-80", "4", day1.Add(13*time.Hour), day1.Add(14*time.Hour))
	trade3 := fixtureTrade(t, ids, acctID, gbpusd, order.Long, "200", "6", day2.Add(8*time.Hour), day2.Add(9*time.Hour))
	openTrade := fixtureOpenTrade(t, ids, acctID, gbpusd, order.Long, "3", day2.Add(12*time.Hour))

	closed := []order.Trade{trade1, trade2, trade3}

	curve := []backtest.EquityPoint{
		{Timestamp: manifest.Span().Start(), Equity: fixtureUSD("10000")},
		{Timestamp: day1.Add(10 * time.Hour), Equity: fixtureUSD("10145")},
		{Timestamp: day1.Add(14 * time.Hour), Equity: fixtureUSD("10061")},
		{Timestamp: day2.Add(9 * time.Hour), Equity: fixtureUSD("10255")},
		{Timestamp: day2.Add(12 * time.Hour), Equity: fixtureUSD("10267")},
	}
	finalEquity := curve[len(curve)-1].Equity

	metrics, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: manifest.StartingCapital(),
		FinalEquity:     finalEquity,
		EquityCurve:     curve,
		Trades:          closed,
		AccountFees:     fixtureUSD("18"), // 5+4+6 closed + 3 still-open entry cost
	})
	require.NoError(t, err)

	position, err := order.NewPosition(order.Position{
		AccountID: acctID,
		Listing:   gbpusd,
		Side:      order.Long,
		Quantity:  num.MustParseQuantity("10000"),
		AvgPrice:  ptrPrice(num.MustParsePrice("1.26500")),
	})
	require.NoError(t, err)

	snapshot, err := tradertest.NewSnapshot(tradertest.SnapshotParams{
		AccountID:     acctID,
		AsOf:          day2.Add(12 * time.Hour),
		Equity:        "10267",
		BuyingPower:   "10067",
		MarginUsed:    "200",
		RealizedPnL:   "270",
		UnrealizedPnL: "12",
		Fees:          "18",
		Positions:     []order.Position{position},
	})
	require.NoError(t, err)

	return backtest.Result{
		Manifest:    manifest,
		Account:     snapshot,
		Trades:      closed,
		OpenTrades:  []order.Trade{openTrade},
		EquityCurve: curve,
		Metrics:     metrics,
	}
}

// newZeroTradeResult returns a backtest.Result for a run that never
// traded — the zero-trade golden case issue #220's acceptance criteria
// calls out explicitly.
func newZeroTradeResult(t *testing.T) backtest.Result {
	t.Helper()

	ids := newFixtureIDs()
	eurusd := fixtureEURUSD(t)
	universe := []strategy.DataRequirement{{Instrument: eurusd.InstrumentID(), Interval: marketdata.H1, WarmupBars: 20}}
	dataset := []marketdata.Manifest{fixtureDatasetManifest(t, eurusd)}
	manifest := fixtureManifest(t, ids, universe, dataset)

	curve := []backtest.EquityPoint{{Timestamp: manifest.Span().Start(), Equity: fixtureUSD("10000")}}

	metrics, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: manifest.StartingCapital(),
		FinalEquity:     fixtureUSD("10000"),
		EquityCurve:     curve,
		AccountFees:     fixtureUSD("0"),
	})
	require.NoError(t, err)

	snapshot, err := tradertest.NewSnapshot(tradertest.SnapshotParams{
		AccountID: mustFixtureAccountID(t, ids),
		AsOf:      manifest.Span().Start(),
		Equity:    "10000",
	})
	require.NoError(t, err)

	return backtest.Result{
		Manifest:    manifest,
		Account:     snapshot,
		Trades:      nil,
		OpenTrades:  nil,
		EquityCurve: curve,
		Metrics:     metrics,
	}
}

func ptrPrice(p num.Price) *num.Price { return &p }
