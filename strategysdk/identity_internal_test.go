package strategysdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
			got, err := parseInstrumentID(want)
			require.NoError(t, err)
			require.Equal(t, want, got.String())
		})
	}
}

func TestParseInstrumentID_EmptyRejected(t *testing.T) {
	_, err := parseInstrumentID("")
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestParseInstrumentID_NonCanonicalRejected(t *testing.T) {
	tests := []string{
		"fx:",
		"fx:eur/usd",
		"fx:EURUSD",
		"not-a-real-kind:FOO",
		"eq:NASDAQAAPL",
		"fut:ES:2026-13",
	}
	for _, id := range tests {
		t.Run(id, func(t *testing.T) {
			_, err := parseInstrumentID(id)
			require.ErrorIs(t, err, ErrInvalidWireValue)
		})
	}
}
