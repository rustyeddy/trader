package sdk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/sdk"
)

func TestSignal(t *testing.T) {
	s := sdk.Signal("sdk_guest", map[string]string{"fast_sma": "1.1000"})
	require.Equal(t, "sdk_guest", s.Strategy)
	require.Equal(t, "1.1000", s.Values["fast_sma"])
	require.Empty(t, s.CorrelationToken)
}

func TestDescribedSignal_WithCorrelation(t *testing.T) {
	s := sdk.Signal("sdk_guest", nil).WithCorrelation("grp-1")
	require.Equal(t, "grp-1", s.CorrelationToken)

	original := sdk.Signal("sdk_guest", nil)
	require.Empty(t, original.CorrelationToken)
}
