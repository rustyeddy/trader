package external_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/adapters/strategy/external"
	"github.com/rustyeddy/trader/marketdata"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func TestFromWireHistoryBarsRequest(t *testing.T) {
	inst := eurUSD(t)
	w := &v1.GetHistoryBarsRequest{
		SessionId:        "sess-1",
		CallbackSequence: 9,
		InstrumentId:     inst.String(),
		Interval:         &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1},
		Count:            20,
	}

	q, err := external.FromWireHistoryBarsRequest(w)
	require.NoError(t, err)
	require.Equal(t, "sess-1", q.SessionID)
	require.Equal(t, uint64(9), q.CallbackSequence)
	require.True(t, q.Instrument.Equal(inst))
	require.Equal(t, 20, q.Count)

	wantInterval, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	require.Equal(t, wantInterval, q.Interval)
}

func TestFromWireHistoryBarsRequest_EmptySessionIDRejected(t *testing.T) {
	w := &v1.GetHistoryBarsRequest{
		InstrumentId: eurUSD(t).String(),
		Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1},
	}
	_, err := external.FromWireHistoryBarsRequest(w)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestFromWireHistoryBarsRequest_NegativeCountRejected(t *testing.T) {
	w := &v1.GetHistoryBarsRequest{
		SessionId:    "sess-1",
		InstrumentId: eurUSD(t).String(),
		Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1},
		Count:        -1,
	}
	_, err := external.FromWireHistoryBarsRequest(w)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestToWireHistoryBarsResponse_PreservesOldestFirstOrder(t *testing.T) {
	b1 := testBar(t)
	b2 := b1
	b2.Time = b1.Time.Add(-time.Hour) // older, but caller still supplies oldest-first

	resp := external.ToWireHistoryBarsResponse([]marketdata.Bar{b2, b1})
	require.Len(t, resp.GetBars(), 2)
	require.Equal(t, b2.Time.UTC().UnixNano(), resp.GetBars()[0].GetTimeUnixNanos())
	require.Equal(t, b1.Time.UTC().UnixNano(), resp.GetBars()[1].GetTimeUnixNanos())
}
