package backtest

import (
	"fmt"
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
// for the canonical merge tie-break, covering the instrument-ID and
// interval branches that a single-instrument fixture in replay_test.go
// cannot reach. The interval comparison uses Interval's intrinsic
// Unit()/Count(), not its display-only String() (issue #212 review):
// UnitHour < UnitDay, so H1 sorts before D1 at an instrument tie.
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
	assert.True(t, lessStream(eur, sameInstD1), "UnitHour < UnitDay: H1 sorts before D1 at an instrument tie")
	assert.False(t, lessStream(sameInstD1, eur))

	earlier := &replayStream{
		req:    strategy.DataRequirement{Instrument: gbpusdIDForTest(t), Interval: marketdata.H1},
		peeked: &marketdata.Bar{Time: when.Add(-time.Hour)},
	}
	assert.True(t, lessStream(earlier, eur), "an earlier timestamp always sorts first, regardless of instrument/interval")
}

// TestCoverageErrorSingleFailureMessage covers the singular branch of
// CoverageError.Error, which a multi-failure fixture test cannot reach.
// The contained error wraps marketdata.ErrDataUnavailable, matching
// NewReplay's own classification rule (issue #212 review): only
// ErrDataUnavailable failures are ever accumulated into a
// CoverageError, so errors.Is(err, marketdata.ErrDataUnavailable) is
// expected to succeed via CoverageError's Unwrap() []error rather than
// an unconditional Is method.
func TestCoverageErrorSingleFailureMessage(t *testing.T) {
	err := &CoverageError{
		Failures: []FailedRequirement{
			{
				Requirement: strategy.DataRequirement{Instrument: eurusdIDForTest(t), Interval: marketdata.H1},
				Err:         fmt.Errorf("%w: no coverage for [2024-01-16T22:00:00Z, 2024-01-17T22:00:00Z)", marketdata.ErrDataUnavailable),
			},
		},
	}
	assert.Contains(t, err.Error(), "no coverage for")
	assert.ErrorIs(t, err, marketdata.ErrDataUnavailable)
}

// TestCoverageErrorUnwrapDoesNotMatchUnrelatedErrors proves
// CoverageError's Unwrap-based Is behavior is not an unconditional
// match: an error it does not contain must not be reported present.
func TestCoverageErrorUnwrapDoesNotMatchUnrelatedErrors(t *testing.T) {
	err := &CoverageError{
		Failures: []FailedRequirement{
			{
				Requirement: strategy.DataRequirement{Instrument: eurusdIDForTest(t), Interval: marketdata.H1},
				Err:         fmt.Errorf("%w: gap", marketdata.ErrDataUnavailable),
			},
		},
	}
	assert.NotErrorIs(t, err, marketdata.ErrInconsistentData)
}
