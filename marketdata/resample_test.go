package marketdata

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dayBar returns a valid Bar at t with the given OHLC/spread/ticks,
// built from validBar(t)'s shape but with explicit values for the
// fields aggregateBars tests care about.
func dayBar(tt *testing.T, at time.Time, open, high, low, close_, avgSpread, maxSpread string, ticks int64) Bar {
	tt.Helper()
	return Bar{
		Time: at, Open: p(tt, open), High: p(tt, high), Low: p(tt, low), Close: p(tt, close_),
		AvgSpread: p(tt, avgSpread), MaxSpread: p(tt, maxSpread), Ticks: ticks,
	}
}

func TestAggregateBars_OHLC(t *testing.T) {
	bars := []Bar{
		dayBar(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "1.10000", "1.10500", "1.09800", "1.10200", "0.00010", "0.00020", 100),
		dayBar(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), "1.10200", "1.10800", "1.10100", "1.10600", "0.00010", "0.00020", 100),
		dayBar(t, time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), "1.10600", "1.10700", "1.09500", "1.09900", "0.00010", "0.00020", 100),
	}
	agg, err := aggregateBars(bars)
	require.NoError(t, err)
	assert.Equal(t, "1.1", agg.Open.String(), "Open must be the first bar's Open")
	assert.Equal(t, "1.108", agg.High.String(), "High must be the max across the window")
	assert.Equal(t, "1.095", agg.Low.String(), "Low must be the min across the window")
	assert.Equal(t, "1.099", agg.Close.String(), "Close must be the last bar's Close")
}

func TestAggregateBars_TicksSummed(t *testing.T) {
	bars := []Bar{
		dayBar(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "1.1", "1.1", "1.1", "1.1", "0.0001", "0.0002", 100),
		dayBar(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), "1.1", "1.1", "1.1", "1.1", "0.0001", "0.0002", 250),
	}
	agg, err := aggregateBars(bars)
	require.NoError(t, err)
	assert.Equal(t, int64(350), agg.Ticks)
}

func TestAggregateBars_MaxSpreadIsMaxAcrossWindow(t *testing.T) {
	bars := []Bar{
		dayBar(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "1.1", "1.1", "1.1", "1.1", "0.0001", "0.0002", 100),
		dayBar(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), "1.1", "1.1", "1.1", "1.1", "0.0001", "0.0009", 100),
		dayBar(t, time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), "1.1", "1.1", "1.1", "1.1", "0.0001", "0.0003", 100),
	}
	agg, err := aggregateBars(bars)
	require.NoError(t, err)
	assert.Equal(t, "0.0009", agg.MaxSpread.String())
}

func TestAggregateBars_AvgSpreadIsTickWeighted(t *testing.T) {
	// bar1: 100 ticks @ spread 0.0001; bar2: 300 ticks @ spread 0.0005.
	// Weighted mean = (100*0.0001 + 300*0.0005) / 400 = 0.0004.
	bars := []Bar{
		dayBar(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "1.1", "1.1", "1.1", "1.1", "0.0001", "0.0010", 100),
		dayBar(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), "1.1", "1.1", "1.1", "1.1", "0.0005", "0.0010", 300),
	}
	agg, err := aggregateBars(bars)
	require.NoError(t, err)
	assert.Equal(t, "0.0004", agg.AvgSpread.String())
}

func TestAggregateBars_UnweightedMeanWouldDiffer(t *testing.T) {
	// Same inputs as above, but confirms the result is NOT the plain
	// (unweighted) arithmetic mean (0.0003) — i.e., the weighting
	// actually matters and isn't accidentally a no-op.
	bars := []Bar{
		dayBar(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "1.1", "1.1", "1.1", "1.1", "0.0001", "0.0010", 100),
		dayBar(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), "1.1", "1.1", "1.1", "1.1", "0.0005", "0.0010", 300),
	}
	agg, err := aggregateBars(bars)
	require.NoError(t, err)
	assert.NotEqual(t, "0.0003", agg.AvgSpread.String())
}

func TestAggregateBars_ZeroTicksFallsBackToZeroSpread(t *testing.T) {
	bars := []Bar{
		dayBar(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "1.1", "1.1", "1.1", "1.1", "0.0001", "0.0002", 0),
		dayBar(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), "1.1", "1.1", "1.1", "1.1", "0.0003", "0.0004", 0),
	}
	agg, err := aggregateBars(bars)
	require.NoError(t, err)
	assert.True(t, agg.AvgSpread.IsZero(), "zero total ticks must fall back to zero AvgSpread, matching legacy's own guard")
	assert.Equal(t, "0.0004", agg.MaxSpread.String(), "MaxSpread aggregation is independent of ticks")
}

func TestAggregateBars_SingleBarPassesThrough(t *testing.T) {
	b := dayBar(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "1.1", "1.105", "1.098", "1.102", "0.0001", "0.0002", 100)
	agg, err := aggregateBars([]Bar{b})
	require.NoError(t, err)
	assert.Equal(t, b.Open.String(), agg.Open.String())
	assert.Equal(t, b.High.String(), agg.High.String())
	assert.Equal(t, b.Low.String(), agg.Low.String())
	assert.Equal(t, b.Close.String(), agg.Close.String())
	assert.Equal(t, b.AvgSpread.String(), agg.AvgSpread.String())
	assert.Equal(t, b.MaxSpread.String(), agg.MaxSpread.String())
	assert.Equal(t, b.Ticks, agg.Ticks)
}

func TestAggregateBars_EmptyInputErrors(t *testing.T) {
	_, err := aggregateBars(nil)
	assert.Error(t, err)
}

func TestAggregateBars_ResultValidates(t *testing.T) {
	bars := []Bar{
		dayBar(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "1.10000", "1.10500", "1.09800", "1.10200", "0.00010", "0.00020", 100),
		dayBar(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), "1.10200", "1.10800", "1.10100", "1.10600", "0.00025", "0.00030", 400),
	}
	agg, err := aggregateBars(bars)
	require.NoError(t, err)
	agg.Time = time.Date(2024, 1, 7, 22, 0, 0, 0, time.UTC) // caller-assigned, per aggregateBars' own contract
	assert.NoError(t, agg.Validate(), "AvgSpread <= MaxSpread must hold by construction")
}

func TestWeekIsD1Ready(t *testing.T) {
	week, err := NewTimeRange(
		time.Date(2024, 1, 7, 22, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 14, 22, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	t.Run("current and no gaps", func(t *testing.T) {
		cov := Coverage{Partitions: []PartitionCoverage{{Year: 2024, Month: time.January, Status: PartitionCoverageCurrent}}}
		assert.True(t, weekIsD1Ready(cov, week))
	})

	t.Run("non-current overlapping month", func(t *testing.T) {
		cov := Coverage{Partitions: []PartitionCoverage{{Year: 2024, Month: time.January, Status: PartitionCoverageStale}}}
		assert.False(t, weekIsD1Ready(cov, week), "a non-Current partition contributes no Gaps, so it must be checked directly")
	})

	t.Run("gap overlapping week", func(t *testing.T) {
		cov := Coverage{
			Partitions: []PartitionCoverage{{Year: 2024, Month: time.January, Status: PartitionCoverageCurrent}},
			Gaps:       []Gap{{State: IntervalStateMissing, Span: week}},
		}
		assert.False(t, weekIsD1Ready(cov, week))
	})

	t.Run("gap outside week does not block it", func(t *testing.T) {
		outside, err := NewTimeRange(
			time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		)
		require.NoError(t, err)
		cov := Coverage{
			Partitions: []PartitionCoverage{{Year: 2024, Month: time.January, Status: PartitionCoverageCurrent}},
			Gaps:       []Gap{{State: IntervalStateMissing, Span: outside}},
		}
		assert.True(t, weekIsD1Ready(cov, week))
	})

	t.Run("non-overlapping month is irrelevant", func(t *testing.T) {
		cov := Coverage{Partitions: []PartitionCoverage{
			{Year: 2024, Month: time.January, Status: PartitionCoverageCurrent},
			{Year: 2024, Month: time.March, Status: PartitionCoverageInvalid},
		}}
		assert.True(t, weekIsD1Ready(cov, week))
	})
}
