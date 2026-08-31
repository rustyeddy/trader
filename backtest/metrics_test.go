package backtest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

var metricsTestIDs = id.NewGenerator(clock.NewSimulated(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))

func metricsUSD(s string) num.Money {
	return num.MustParseMoney(s, num.MustParseCurrency("USD"))
}

func metricsTrade(t *testing.T, listing instrument.Listing, realizedPnL, costs string, closedAt time.Time) order.Trade {
	t.Helper()
	tr, err := order.NewTrade(order.Trade{
		AccountID:    mustMetricsAccountID(t),
		Listing:      listing,
		Side:         order.Long,
		EntryFillIDs: []id.FillID{mustMetricsFillID(t)},
		OpenedAt:     closedAt.Add(-time.Hour),
		ClosedAt:     closedAt,
		RealizedPnL:  metricsUSD(realizedPnL),
		Costs:        metricsUSD(costs),
	})
	require.NoError(t, err)
	return tr
}

func mustMetricsAccountID(t *testing.T) id.AccountID {
	t.Helper()
	v, err := id.GenerateAccountID(metricsTestIDs)
	require.NoError(t, err)
	return v
}

func mustMetricsFillID(t *testing.T) id.FillID {
	t.Helper()
	v, err := id.GenerateFillID(metricsTestIDs)
	require.NoError(t, err)
	return v
}

func TestNewMetrics_ZeroTrades(t *testing.T) {
	m, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: metricsUSD("10000"),
		FinalEquity:     metricsUSD("10000"),
	})
	require.NoError(t, err)

	assert.Equal(t, 0, m.TradeCount())
	assert.Equal(t, 0, m.Wins())
	assert.Equal(t, 0, m.Losses())
	assert.Nil(t, m.WinRate())
	assert.Nil(t, m.AverageWin())
	assert.Nil(t, m.AverageLoss())
	assert.Nil(t, m.Expectancy())
	assert.Nil(t, m.ProfitFactor())
	assert.True(t, m.GrossPnL().IsZero())
	assert.True(t, m.TotalCosts().IsZero())
	assert.True(t, m.NetPnL().IsZero())
	assert.True(t, m.NetReturn().IsZero())
	assert.True(t, m.MaxDrawdown().IsZero())
	assert.Empty(t, m.PerInstrument())
}

func TestNewMetrics_AllWins(t *testing.T) {
	listing := simListing(t, "EUR", "USD", "EUR_USD")
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	trades := []order.Trade{
		metricsTrade(t, listing, "100", "1", t0),
		metricsTrade(t, listing, "50", "1", t0.Add(time.Hour)),
	}
	m, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: metricsUSD("10000"),
		FinalEquity:     metricsUSD("10148"),
		Trades:          trades,
	})
	require.NoError(t, err)

	assert.Equal(t, 2, m.TradeCount())
	assert.Equal(t, 2, m.Wins())
	assert.Equal(t, 0, m.Losses())
	require.NotNil(t, m.WinRate())
	assert.True(t, m.WinRate().Equal(num.MustParseRate("1")))
	require.NotNil(t, m.AverageWin())
	assert.True(t, m.AverageWin().Equal(metricsUSD("74")), "((100-1)+(50-1))/2 = 74")
	assert.Nil(t, m.AverageLoss())
	assert.Nil(t, m.ProfitFactor(), "no losses means an undefined profit factor, not infinite")
	require.NotNil(t, m.Expectancy())
	assert.True(t, m.Expectancy().Equal(metricsUSD("74")))
	assert.True(t, m.GrossPnL().Equal(metricsUSD("150")))
	assert.True(t, m.TotalCosts().Equal(metricsUSD("2")))
	assert.True(t, m.NetPnL().Equal(metricsUSD("148")))
}

func TestNewMetrics_AllLosses(t *testing.T) {
	listing := simListing(t, "EUR", "USD", "EUR_USD")
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	trades := []order.Trade{
		metricsTrade(t, listing, "-100", "1", t0),
	}
	m, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: metricsUSD("10000"),
		FinalEquity:     metricsUSD("9899"),
		Trades:          trades,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, m.Losses())
	assert.Equal(t, 0, m.Wins())
	require.NotNil(t, m.WinRate())
	assert.True(t, m.WinRate().IsZero())
	assert.Nil(t, m.AverageWin())
	require.NotNil(t, m.AverageLoss())
	assert.True(t, m.AverageLoss().Equal(metricsUSD("-101")), "average loss is negative-signed")
	require.NotNil(t, m.ProfitFactor(), "gross loss is nonzero here, so the ratio (0 gross profit / gross loss) is defined, just zero")
	assert.True(t, m.ProfitFactor().IsZero())
}

func TestNewMetrics_MixedWithScratch(t *testing.T) {
	listing := simListing(t, "EUR", "USD", "EUR_USD")
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	trades := []order.Trade{
		metricsTrade(t, listing, "100", "0", t0),                  // win: net 100
		metricsTrade(t, listing, "60", "0", t0.Add(time.Hour)),    // win: net 60
		metricsTrade(t, listing, "-40", "0", t0.Add(2*time.Hour)), // loss: net -40
		metricsTrade(t, listing, "10", "10", t0.Add(3*time.Hour)), // scratch: net 0
	}
	m, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: metricsUSD("10000"),
		FinalEquity:     metricsUSD("10120"),
		Trades:          trades,
	})
	require.NoError(t, err)

	assert.Equal(t, 4, m.TradeCount())
	assert.Equal(t, 2, m.Wins())
	assert.Equal(t, 1, m.Losses())
	require.NotNil(t, m.WinRate())
	assert.True(t, m.WinRate().Equal(num.MustParseRate("0.5")), "2 wins out of 4 trades")
	require.NotNil(t, m.ProfitFactor())
	assert.True(t, m.ProfitFactor().Equal(num.MustParseRate("4")), "(100+60)/40")
	assert.True(t, m.NetPnL().Equal(metricsUSD("120")))
}

func TestNewMetrics_RejectsCurrencyMismatch(t *testing.T) {
	listing := simListing(t, "EUR", "USD", "EUR_USD")
	eur := num.MustParseCurrency("EUR")
	badTrade, err := order.NewTrade(order.Trade{
		AccountID:    mustMetricsAccountID(t),
		Listing:      listing,
		Side:         order.Long,
		EntryFillIDs: []id.FillID{mustMetricsFillID(t)},
		OpenedAt:     time.Now(),
		RealizedPnL:  num.MustParseMoney("1", eur),
		Costs:        num.MustParseMoney("0", eur),
	})
	require.NoError(t, err)

	_, err = backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: metricsUSD("10000"),
		FinalEquity:     metricsUSD("10000"),
		Trades:          []order.Trade{badTrade},
	})
	require.ErrorIs(t, err, backtest.ErrInvalidMetrics)
}

func TestNewMetrics_RejectsZeroStartingCapital(t *testing.T) {
	_, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: metricsUSD("0"),
		FinalEquity:     metricsUSD("0"),
	})
	require.ErrorIs(t, err, backtest.ErrInvalidMetrics)
}

func TestNewMetrics_RejectsOutOfOrderEquityCurve(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: metricsUSD("10000"),
		FinalEquity:     metricsUSD("10000"),
		EquityCurve: []backtest.EquityPoint{
			{Timestamp: t0.Add(time.Hour), Equity: metricsUSD("10000")},
			{Timestamp: t0, Equity: metricsUSD("10000")},
		},
	})
	require.ErrorIs(t, err, backtest.ErrInvalidMetrics)
}

func TestNewMetrics_NetReturnAndDrawdown(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	curve := []backtest.EquityPoint{
		{Timestamp: t0, Equity: metricsUSD("10000")},
		{Timestamp: t0.Add(time.Hour), Equity: metricsUSD("11000")},    // peak
		{Timestamp: t0.Add(2 * time.Hour), Equity: metricsUSD("9900")}, // trough: (11000-9900)/11000 = 0.1
		{Timestamp: t0.Add(3 * time.Hour), Equity: metricsUSD("10500")},
	}
	m, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: metricsUSD("10000"),
		FinalEquity:     metricsUSD("10500"),
		EquityCurve:     curve,
	})
	require.NoError(t, err)

	assert.True(t, m.NetReturn().Equal(num.MustParseRate("0.05")), "(10500-10000)/10000")
	assert.True(t, m.MaxDrawdown().Equal(num.MustParseRate("0.1")), "(11000-9900)/11000")
	assert.Equal(t, curve, m.EquityCurve())
}

func TestNewMetrics_DrawdownEmptyOrSinglePointCurveIsZero(t *testing.T) {
	m, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: metricsUSD("10000"),
		FinalEquity:     metricsUSD("10000"),
	})
	require.NoError(t, err)
	assert.True(t, m.MaxDrawdown().IsZero())

	m2, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: metricsUSD("10000"),
		FinalEquity:     metricsUSD("10000"),
		EquityCurve:     []backtest.EquityPoint{{Timestamp: time.Now(), Equity: metricsUSD("10000")}},
	})
	require.NoError(t, err)
	assert.True(t, m2.MaxDrawdown().IsZero())
}

func TestNewMetrics_DrawdownNonPositivePeakIsFullDrawdown(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	curve := []backtest.EquityPoint{
		{Timestamp: t0, Equity: metricsUSD("-100")},
		{Timestamp: t0.Add(time.Hour), Equity: metricsUSD("-50")},
	}
	m, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: metricsUSD("10000"),
		FinalEquity:     metricsUSD("-50"),
		EquityCurve:     curve,
	})
	require.NoError(t, err)
	assert.True(t, m.MaxDrawdown().Equal(num.MustParseRate("1")))
}

func TestNewMetrics_PerInstrumentBreakdown(t *testing.T) {
	eurusd := simListing(t, "EUR", "USD", "EUR_USD")
	gbpusd := simListing(t, "GBP", "USD", "GBP_USD")
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	trades := []order.Trade{
		metricsTrade(t, eurusd, "100", "1", t0),
		metricsTrade(t, eurusd, "-40", "1", t0.Add(time.Hour)),
		metricsTrade(t, gbpusd, "20", "0", t0.Add(2*time.Hour)),
	}
	m, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: metricsUSD("10000"),
		FinalEquity:     metricsUSD("10079"),
		Trades:          trades,
	})
	require.NoError(t, err)

	byInstrument := m.PerInstrument()
	require.Len(t, byInstrument, 2)

	var eurGroup, gbpGroup *backtest.InstrumentMetrics
	for i := range byInstrument {
		switch byInstrument[i].InstrumentID.String() {
		case eurusd.InstrumentID().String():
			eurGroup = &byInstrument[i]
		case gbpusd.InstrumentID().String():
			gbpGroup = &byInstrument[i]
		}
	}
	require.NotNil(t, eurGroup)
	require.NotNil(t, gbpGroup)

	assert.Equal(t, 2, eurGroup.Count)
	assert.Equal(t, 1, eurGroup.Wins)
	assert.Equal(t, 1, eurGroup.Losses)
	assert.True(t, eurGroup.GrossPnL.Equal(metricsUSD("60")))
	assert.True(t, eurGroup.Costs.Equal(metricsUSD("2")))
	assert.True(t, eurGroup.NetPnL.Equal(metricsUSD("58")))

	assert.Equal(t, 1, gbpGroup.Count)
	assert.True(t, gbpGroup.NetPnL.Equal(metricsUSD("20")))

	// Reconciles with the aggregate.
	sumNet, err := eurGroup.NetPnL.Add(gbpGroup.NetPnL)
	require.NoError(t, err)
	assert.True(t, sumNet.Equal(m.NetPnL()))
}
