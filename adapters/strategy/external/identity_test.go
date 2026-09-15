package external_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/strategy/external"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// TestParseInstrumentID_RoundTrips is exercised indirectly through
// every conversion function that reads an instrument_id field; this
// test isolates it via FromWireHistoryBarsRequest, the simplest
// entry point that returns the parsed instrument.ID directly.
func TestParseInstrumentID_RoundTrips(t *testing.T) {
	inst := eurUSD(t)
	q, err := external.FromWireHistoryBarsRequest(&v1.GetHistoryBarsRequest{
		SessionId:    "s",
		InstrumentId: inst.String(),
		Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_DAY, Count: 1},
	})
	require.NoError(t, err)
	require.True(t, q.Instrument.Equal(inst))
	require.Equal(t, inst.String(), q.Instrument.String())
}

func TestParseInstrumentID_EmptyRejected(t *testing.T) {
	_, err := external.FromWireHistoryBarsRequest(&v1.GetHistoryBarsRequest{
		SessionId:    "s",
		InstrumentId: "",
		Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_DAY, Count: 1},
	})
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestParseInstrumentID_UnknownKindPrefixRejected(t *testing.T) {
	_, err := external.FromWireHistoryBarsRequest(&v1.GetHistoryBarsRequest{
		SessionId:    "s",
		InstrumentId: "not-a-real-kind:FOO",
		Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_DAY, Count: 1},
	})
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}
