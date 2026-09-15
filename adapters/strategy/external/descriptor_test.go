package external_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/strategy/external"
	"github.com/rustyeddy/trader/marketdata"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func TestFromWireDescriptor(t *testing.T) {
	inst := eurUSD(t)
	w := &v1.StrategyDescriptor{
		Name:    "smatrend",
		Version: "1.2.3",
		Requirements: []*v1.DataRequirement{
			{
				InstrumentId: inst.String(),
				Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1},
				WarmupBars:   50,
			},
		},
	}

	d, err := external.FromWireDescriptor(w)
	require.NoError(t, err)
	require.Equal(t, "smatrend", d.Name)
	require.Equal(t, "1.2.3", d.Version)
	require.Len(t, d.Requirements, 1)

	req := d.Requirements[0]
	require.True(t, req.Instrument.Equal(inst))
	wantInterval, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	require.Equal(t, wantInterval, req.Interval)
	require.Equal(t, 50, req.WarmupBars)
}

func TestFromWireDescriptor_NilRejected(t *testing.T) {
	_, err := external.FromWireDescriptor(nil)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestFromWireDescriptor_InvalidInstrumentIDRejected(t *testing.T) {
	w := &v1.StrategyDescriptor{
		Name: "smatrend",
		Requirements: []*v1.DataRequirement{
			{
				InstrumentId: "not-a-valid-instrument-id",
				Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1},
			},
		},
	}
	_, err := external.FromWireDescriptor(w)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestFromWireDescriptor_NegativeWarmupBarsRejected(t *testing.T) {
	w := &v1.StrategyDescriptor{
		Name: "smatrend",
		Requirements: []*v1.DataRequirement{
			{
				InstrumentId: eurUSD(t).String(),
				Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1},
				WarmupBars:   -1,
			},
		},
	}
	_, err := external.FromWireDescriptor(w)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestFromWireDescriptor_EmptyRequirementsIsValid(t *testing.T) {
	w := &v1.StrategyDescriptor{Name: "no_requirements"}
	d, err := external.FromWireDescriptor(w)
	require.NoError(t, err)
	require.Empty(t, d.Requirements)
}
