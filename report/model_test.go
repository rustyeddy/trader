package report_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/report"
)

func TestNewBacktestReport_RepresentativeResult(t *testing.T) {
	result := newRepresentativeResult(t)
	rep := report.NewBacktestReport(report.BacktestInputFromResult(result))

	assert.Equal(t, result.Manifest.RunID().String(), rep.Run.RunID)
	assert.Equal(t, result.Manifest.StrategyName(), rep.Run.StrategyName)
	assert.Equal(t, result.Manifest.ConfigDigest(), rep.Run.ConfigDigest)
	assert.True(t, rep.Run.SpanStart.Equal(result.Manifest.Span().Start()))
	assert.Equal(t, "UTC", rep.Run.SpanStart.Location().String())

	assert.Equal(t, result.Metrics.StartingCapital(), rep.Performance.StartingCapital)
	assert.Equal(t, result.Metrics.FinalEquity(), rep.Performance.FinalEquity)
	assert.Equal(t, result.Metrics.NetReturn(), rep.Performance.NetReturn)
	assert.Equal(t, result.Metrics.MaxDrawdown(), rep.Performance.MaxDrawdown)

	require.Equal(t, result.Metrics.TradeCount(), rep.TradeStats.TradeCount)
	assert.Equal(t, result.Metrics.Wins(), rep.TradeStats.Wins)
	assert.Equal(t, result.Metrics.Losses(), rep.TradeStats.Losses)
	require.NotNil(t, rep.TradeStats.WinRate)
	assert.Equal(t, *result.Metrics.WinRate(), *rep.TradeStats.WinRate)
	assert.Equal(t, result.Metrics.AccountFees(), rep.TradeStats.AccountFees)
	assert.Equal(t, result.Metrics.ClosedTradeCosts(), rep.TradeStats.ClosedTradeCosts)
	assert.NotEqual(t, rep.TradeStats.AccountFees, rep.TradeStats.ClosedTradeCosts,
		"fixture must exercise AccountFees != ClosedTradeCosts (issue #220 review, point 9)")

	require.Len(t, rep.PerInstrument, len(result.Metrics.PerInstrument()))
	require.Len(t, rep.ClosedTrades, len(result.Trades))
	require.Len(t, rep.OpenTrades, len(result.OpenTrades))
	assert.True(t, rep.OpenTrades[0].ClosedAt.IsZero(), "an open trade's ClosedAt must remain zero, not a fabricated timestamp")

	require.Len(t, rep.EquityCurve, len(result.EquityCurve))
	assert.Equal(t, result.EquityCurve[0].Equity, rep.EquityCurve[0].Equity)

	assert.Equal(t, result.Account.Equity(), rep.Account.Equity)
	require.Len(t, rep.Account.OpenPositions, 1)
	assert.Equal(t, "long", rep.Account.OpenPositions[0].Side)
}

func TestNewBacktestReport_ZeroTradeResult(t *testing.T) {
	result := newZeroTradeResult(t)
	rep := report.NewBacktestReport(report.BacktestInputFromResult(result))

	assert.Equal(t, 0, rep.TradeStats.TradeCount)
	assert.Nil(t, rep.TradeStats.WinRate)
	assert.Nil(t, rep.TradeStats.AverageWin)
	assert.Nil(t, rep.TradeStats.AverageLoss)
	assert.Nil(t, rep.TradeStats.Expectancy)
	assert.Nil(t, rep.TradeStats.ProfitFactor)
	assert.Empty(t, rep.PerInstrument)
	assert.Empty(t, rep.ClosedTrades)
	assert.Empty(t, rep.OpenTrades)
	assert.Empty(t, rep.Account.OpenPositions)
}

// TestNewBacktestReport_AcceptsInputWithoutABacktestResult proves the
// primary NewBacktestReport(BacktestInput) API works from data that
// never passed through a backtest.Result — the shape a persisted
// report snapshot deserializes into (issue #222), or a service/
// backtest.RunResponse a caller copies field-for-field, both of which
// have no backtest.Result value to call BacktestInputFromResult on.
func TestNewBacktestReport_AcceptsInputWithoutABacktestResult(t *testing.T) {
	result := newZeroTradeResult(t)
	in := report.BacktestInput{
		Manifest:    result.Manifest,
		Account:     result.Account,
		Trades:      result.Trades,
		OpenTrades:  result.OpenTrades,
		EquityCurve: result.EquityCurve,
		Metrics:     result.Metrics,
	}

	rep := report.NewBacktestReport(in)
	assert.Equal(t, result.Manifest.RunID().String(), rep.Run.RunID)
	assert.Equal(t, 0, rep.TradeStats.TradeCount)
}
