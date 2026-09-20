package external_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/adapters/strategy/external"
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

// TestParseInstrumentID_RecognizedPrefixButNonCanonicalRejected is the
// review finding: instrument.ID.UnmarshalJSON alone would have
// accepted these (a recognized "<kind>:" prefix is all it checks),
// even though no instrument constructor could ever produce them.
func TestParseInstrumentID_RecognizedPrefixButNonCanonicalRejected(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"fx with nothing after prefix", "fx:"},
		{"fx lowercase currencies", "fx:eur/usd"},
		{"fx missing slash", "fx:EURUSD"},
		{"fx empty base", "fx:/USD"},
		{"eq missing colon", "eq:NASDAQAAPL"},
		{"eq empty ticker", "eq:NASDAQ:"},
		{"fut malformed month", "fut:ES:2026-13"},
		{"fut missing month", "fut:ES"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := external.FromWireHistoryBarsRequest(&v1.GetHistoryBarsRequest{
				SessionId:    "s",
				InstrumentId: tt.id,
				Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_DAY, Count: 1},
			})
			require.ErrorIsf(t, err, external.ErrInvalidWireValue, "id %q should have been rejected", tt.id)
		})
	}
}

// TestParseInstrumentID_EveryKind proves reconstructInstrumentID's own
// per-Kind parser actually accepts each Kind's real canonical form —
// not merely rejects malformed ones.
func TestParseInstrumentID_EveryKind(t *testing.T) {
	tests := []string{
		"fx:EUR/USD",
		"eq:NASDAQ:AAPL",
		"etf:NYSE:SPY",
		"fut:ES:2026-12",
		"cont:ES",
		"idx:SPX",
	}
	for _, want := range tests {
		t.Run(want, func(t *testing.T) {
			q, err := external.FromWireHistoryBarsRequest(&v1.GetHistoryBarsRequest{
				SessionId:    "s",
				InstrumentId: want,
				Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_DAY, Count: 1},
			})
			require.NoError(t, err)
			require.Equal(t, want, q.Instrument.String())
		})
	}
}
