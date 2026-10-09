package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// sendCapture is a v1.StrategyHostService_RunClient that records Send.
type sendCapture struct {
	v1.StrategyHostService_RunClient
	sent []*v1.RunClientMessage
}

func (c *sendCapture) Send(m *v1.RunClientMessage) error {
	c.sent = append(c.sent, m)
	return nil
}

func wireSnapshot() *v1.AccountSnapshot {
	return &v1.AccountSnapshot{AccountId: "a", Currency: "USD"}
}

// A host that sends the wrong delivery shape is answered with an
// explicit CAPABILITY_MISMATCH error response, never a panic or a
// silently dropped callback.
func TestGuestRun_DeliveryShapeMismatchIsExplicit(t *testing.T) {
	t.Run("bars event to bar consumer", func(t *testing.T) {
		stream := &sendCapture{}
		g := &guestRun{ctx: context.Background(), stream: stream, barHandler: nil, barsHandler: nil}
		iv := &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1}
		bar := testWireBar()
		require.NoError(t, g.handleBarsEvent(&v1.BarsEvent{
			Sequence: 3, Interval: iv, BoundaryUnixNanos: bar.TimeUnixNanos, Account: wireSnapshot(),
		}))
		require.Len(t, stream.sent, 1)
		resp := stream.sent[0].GetOnBarsResponse()
		require.Equal(t, uint64(3), resp.GetSequence())
		require.Equal(t, v1.ErrorCode_ERROR_CODE_CAPABILITY_MISMATCH, resp.GetError().GetCode())
	})

	t.Run("bar event to bars consumer", func(t *testing.T) {
		stream := &sendCapture{}
		g := &guestRun{ctx: context.Background(), stream: stream}
		require.NoError(t, g.handleBarEvent(&v1.BarEvent{
			Sequence: 4, InstrumentId: "fx:EUR/USD",
			Interval: &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1},
			Bar:      testWireBar(), Account: wireSnapshot(),
		}))
		resp := stream.sent[0].GetOnBarResponse()
		require.Equal(t, uint64(4), resp.GetSequence())
		require.Equal(t, v1.ErrorCode_ERROR_CODE_CAPABILITY_MISMATCH, resp.GetError().GetCode())
	})
}
