package report_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/backtest"
	"github.com/rustyeddy/trader/internal/report"
	"github.com/rustyeddy/trader/num"
)

func TestNewBacktestReport_MarginSection(t *testing.T) {
	t.Run("representative run", func(t *testing.T) {
		mg := report.NewBacktestReport(report.BacktestInputFromResult(newRepresentativeResult(t))).Margin
		require.NotNil(t, mg.InitialMarginRatio)
		assert.True(t, mg.InitialMarginRatio.Equal(num.MustParseRate("0.5")))
		assert.Equal(t, 3, mg.Rejections)
		assert.Equal(t, 2, mg.AdmissionRejections)
		assert.Equal(t, 1, mg.FillRejections)
		require.NotNil(t, mg.PeakGrossNotional)
		assert.True(t, mg.PeakGrossNotional.Equal(fixtureUSD("12650")))
		require.NotNil(t, mg.PeakGrossLeverage)
		assert.Equal(t, "1.23210285", mg.PeakGrossLeverage.String(), "12650 / 10267")
	})
	t.Run("no margin model", func(t *testing.T) {
		mg := report.NewBacktestReport(report.BacktestInputFromResult(newZeroTradeResult(t))).Margin
		assert.Nil(t, mg.InitialMarginRatio)
		assert.Zero(t, mg.Rejections)
		assert.Nil(t, mg.PeakGrossNotional)
		assert.Nil(t, mg.PeakGrossLeverage)
	})
	t.Run("margin model without a parsable ratio", func(t *testing.T) {
		result := newZeroTradeResult(t)
		for _, params := range []any{map[string]string{"other": "1"}, map[string]string{backtest.InitialMarginRatioParameter: "abc"}, []int{1}} {
			model, err := backtest.NewComponentInfo("initial-margin-ratio", "v1", params)
			require.NoError(t, err)
			result.Manifest = withMarginModel(t, result.Manifest, model)
			assert.Nil(t, report.NewBacktestReport(report.BacktestInputFromResult(result)).Margin.InitialMarginRatio, "%v", params)
		}
	})
}

// withMarginModel rebuilds m with model as its margin model.
func withMarginModel(t *testing.T, m backtest.Manifest, model backtest.ComponentInfo) backtest.Manifest {
	t.Helper()
	out, err := backtest.NewManifest(backtest.ManifestParams{
		RunID:           m.RunID(),
		StrategyName:    m.StrategyName(),
		StrategyVersion: m.StrategyVersion(),
		Universe:        m.Universe(),
		Span:            m.Span(),
		StartingCapital: m.StartingCapital(),
		RiskFraction:    m.RiskFraction(),
		AdverseDistance: m.AdverseDistance(),
		FillModel:       m.FillModel(),
		SlippageModel:   m.SlippageModel(),
		CommissionModel: m.CommissionModel(),
		MarginModel:     model,
		Dataset:         m.Dataset(),
		TraderVersion:   m.TraderVersion(),
	})
	require.NoError(t, err)
	return out
}
