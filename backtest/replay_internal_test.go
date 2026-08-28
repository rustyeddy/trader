package backtest

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
)

func eurusdIDForTest(t *testing.T) instrument.ID {
	t.Helper()
	id := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	assert.False(t, id.IsZero())
	return id
}

func gbpusdIDForTest(t *testing.T) instrument.ID {
	t.Helper()
	id := instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	assert.False(t, id.IsZero())
	return id
}

// TestLessStreamTieBreaksByInstrumentThenInterval is a white-box test
// for the canonical merge tie-break, covering the instrument-ID branch
// that a single-instrument fixture in replay_test.go cannot reach.
func TestLessStreamTieBreaksByInstrumentThenInterval(t *testing.T) {
	when := time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC)

	eur := &replayStream{
		req:    strategy.DataRequirement{Instrument: eurusdIDForTest(t), Interval: marketdata.H1},
		peeked: &marketdata.Bar{Time: when},
	}
	gbp := &replayStream{
		req:    strategy.DataRequirement{Instrument: gbpusdIDForTest(t), Interval: marketdata.H1},
		peeked: &marketdata.Bar{Time: when},
	}

	assert.True(t, lessStream(eur, gbp), "fx:EUR/USD sorts before fx:GBP/USD lexically")
	assert.False(t, lessStream(gbp, eur))

	sameInstD1 := &replayStream{
		req:    strategy.DataRequirement{Instrument: eurusdIDForTest(t), Interval: marketdata.D1},
		peeked: &marketdata.Bar{Time: when},
	}
	assert.True(t, lessStream(sameInstD1, eur), "D1 sorts before H1 lexically at an instrument tie")

	earlier := &replayStream{
		req:    strategy.DataRequirement{Instrument: gbpusdIDForTest(t), Interval: marketdata.H1},
		peeked: &marketdata.Bar{Time: when.Add(-time.Hour)},
	}
	assert.True(t, lessStream(earlier, eur), "an earlier timestamp always sorts first, regardless of instrument/interval")
}

// TestCoverageCompleteRejectsNonCurrentPartitionEvenWithoutGaps covers
// coverageComplete's partition-status branch directly: a partition
// that is Missing, Invalid, or Stale contributes no Gaps of its own
// (Coverage's own doc comment), so a real fixture that only exercises
// the Gaps branch cannot reach this one.
func TestCoverageCompleteRejectsNonCurrentPartitionEvenWithoutGaps(t *testing.T) {
	cov := marketdata.Coverage{
		Partitions: []marketdata.PartitionCoverage{
			{Year: 2024, Month: time.January, Status: marketdata.PartitionCoverageMissing},
		},
	}
	assert.False(t, coverageComplete(cov))
}

func TestCoverageCompleteAcceptsCurrentPartitionsWithNoGaps(t *testing.T) {
	cov := marketdata.Coverage{
		Partitions: []marketdata.PartitionCoverage{
			{Year: 2024, Month: time.January, Status: marketdata.PartitionCoverageCurrent},
		},
	}
	assert.True(t, coverageComplete(cov))
}

// TestCoverageErrorSingleFailureMessage covers the singular branch of
// CoverageError.Error, which a multi-failure fixture test cannot reach.
func TestCoverageErrorSingleFailureMessage(t *testing.T) {
	err := &CoverageError{
		Failures: []RequirementCoverage{
			{
				Requirement: strategy.DataRequirement{Instrument: eurusdIDForTest(t), Interval: marketdata.H1},
				Coverage: marketdata.Coverage{
					Gaps: []marketdata.Gap{{}},
				},
			},
		},
	}
	assert.Contains(t, err.Error(), "1 gap(s)")
	assert.ErrorIs(t, err, marketdata.ErrDataUnavailable)
}
