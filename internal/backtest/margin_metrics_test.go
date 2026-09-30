package backtest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/backtest"
	"github.com/rustyeddy/trader/num"
)

func grossPoint(at time.Time, equity, gross string) backtest.EquityPoint {
	p := backtest.EquityPoint{Timestamp: at, Equity: num.MustParseMoney(equity, num.MustParseCurrency("USD"))}
	if gross != "" {
		g := num.MustParseMoney(gross, num.MustParseCurrency("USD"))
		p.GrossNotional = &g
	}
	return p
}

func metricsFor(t *testing.T, curve ...backtest.EquityPoint) backtest.Metrics {
	t.Helper()
	usd := num.MustParseCurrency("USD")
	m, err := backtest.NewMetrics(backtest.MetricsParams{
		StartingCapital: num.MustParseMoney("10000", usd),
		FinalEquity:     curve[len(curve)-1].Equity,
		EquityCurve:     curve,
		AccountFees:     num.MustParseMoney("0", usd),
	})
	require.NoError(t, err)
	return m
}

func TestMetrics_PeakGrossExposure(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }

	t.Run("peaks are taken independently", func(t *testing.T) {
		m := metricsFor(t,
			grossPoint(at(0), "10000", "0"),
			grossPoint(at(1), "10000", "15000"), // gross peak, 1.5x
			grossPoint(at(2), "5000", "10000"),  // leverage peak, 2x
			grossPoint(at(3), "10000", "2000"),
		)
		require.NotNil(t, m.PeakGrossNotional())
		assert.True(t, m.PeakGrossNotional().Equal(num.MustParseMoney("15000", num.MustParseCurrency("USD"))))
		require.NotNil(t, m.PeakGrossLeverage())
		assert.True(t, m.PeakGrossLeverage().Equal(num.MustParseRate("2")))
	})
	t.Run("flat run peaks at zero", func(t *testing.T) {
		m := metricsFor(t, grossPoint(at(0), "10000", "0"), grossPoint(at(1), "10000", "0"))
		assert.True(t, m.PeakGrossNotional().IsZero())
		assert.True(t, m.PeakGrossLeverage().IsZero())
	})
	t.Run("points without gross are skipped; none at all is nil", func(t *testing.T) {
		m := metricsFor(t, grossPoint(at(0), "10000", ""), grossPoint(at(1), "10000", ""))
		assert.Nil(t, m.PeakGrossNotional())
		assert.Nil(t, m.PeakGrossLeverage())

		m = metricsFor(t, grossPoint(at(0), "10000", ""), grossPoint(at(1), "10000", "3000"))
		assert.True(t, m.PeakGrossNotional().Equal(num.MustParseMoney("3000", num.MustParseCurrency("USD"))))
	})
	t.Run("non-positive equity is skipped for leverage only", func(t *testing.T) {
		m := metricsFor(t, grossPoint(at(0), "10000", "1000"), grossPoint(at(1), "-50", "9000"))
		assert.True(t, m.PeakGrossNotional().Equal(num.MustParseMoney("9000", num.MustParseCurrency("USD"))))
		assert.True(t, m.PeakGrossLeverage().Equal(num.MustParseRate("0.1")))
	})
	t.Run("gross in another currency is rejected", func(t *testing.T) {
		eur := num.MustParseMoney("1", num.MustParseCurrency("EUR"))
		p := grossPoint(at(0), "10000", "")
		p.GrossNotional = &eur
		_, err := backtest.NewMetrics(backtest.MetricsParams{
			StartingCapital: num.MustParseMoney("10000", num.MustParseCurrency("USD")),
			FinalEquity:     p.Equity,
			EquityCurve:     []backtest.EquityPoint{p},
			AccountFees:     num.MustParseMoney("0", num.MustParseCurrency("USD")),
		})
		assert.ErrorIs(t, err, backtest.ErrInvalidMetrics)
	})
}

func TestMarginRejectionsTotal(t *testing.T) {
	assert.Equal(t, 5, backtest.MarginRejections{Admission: 2, Fill: 3}.Total())
}
