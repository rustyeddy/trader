package external_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/strategy/external"
	"github.com/rustyeddy/trader/id"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func TestSignalFromWire_Basic(t *testing.T) {
	w := &v1.DescribedSignal{
		Strategy: "smatrend",
		Values:   map[string]string{"fast_sma": "1.1000", "slow_sma": "1.0990"},
	}

	sig, corr, err := external.SignalFromWire(w, nil)
	require.NoError(t, err)
	require.Equal(t, "smatrend", sig.Strategy)
	require.Equal(t, "1.1000", sig.Values["fast_sma"])
	require.Equal(t, "1.0990", sig.Values["slow_sma"])
	require.True(t, corr.IsZero())
}

func TestSignalFromWire_NilRejected(t *testing.T) {
	_, _, err := external.SignalFromWire(nil, nil)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

// TestSignalFromWire_EmptyStrategyRejected is the review finding:
// journal.NewRecord rejects an empty Signal.Strategy, so this must
// reject it here rather than returning an already-invalid
// journal.Signal.
func TestSignalFromWire_EmptyStrategyRejected(t *testing.T) {
	_, _, err := external.SignalFromWire(&v1.DescribedSignal{Values: map[string]string{"k": "v"}}, nil)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestSignalFromWire_EmptyValuesIsNilMap(t *testing.T) {
	sig, _, err := external.SignalFromWire(&v1.DescribedSignal{Strategy: "smatrend"}, nil)
	require.NoError(t, err)
	require.Nil(t, sig.Values)
}

// TestSignalFromWire_CorrelatesWithIntentGroup is issue #384's own
// deterministic-equivalence requirement: a signal whose
// correlation_token matches a described intent's own token resolves
// to that same real CorrelationID IntentsFromWire minted.
func TestSignalFromWire_CorrelatesWithIntentGroup(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	inst := eurUSD(t)

	intents, tokens, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_ENTER, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY, CorrelationToken: "cross"},
	}, factory)
	require.NoError(t, err)
	require.Len(t, intents, 1)

	sig, corr, err := external.SignalFromWire(&v1.DescribedSignal{
		Strategy:         "smatrend",
		Values:           map[string]string{"cross": "golden"},
		CorrelationToken: "cross",
	}, tokens)
	require.NoError(t, err)
	require.Equal(t, "smatrend", sig.Strategy)
	require.True(t, corr.Equal(intents[0].Metadata.CorrelationID))
}

// TestSignalFromWire_NoMatchingIntentIsZeroCorrelationID mirrors
// strategy/smatrend's own "no intents this bar" case: an empty (or
// unmatched) correlation_token never fabricates a CorrelationID.
func TestSignalFromWire_NoMatchingIntentIsZeroCorrelationID(t *testing.T) {
	tokens := map[string]id.CorrelationID{}
	_, corr, err := external.SignalFromWire(&v1.DescribedSignal{Strategy: "smatrend"}, tokens)
	require.NoError(t, err)
	require.True(t, corr.IsZero())

	_, corr, err = external.SignalFromWire(&v1.DescribedSignal{
		Strategy:         "smatrend",
		CorrelationToken: "no-such-token",
	}, tokens)
	require.NoError(t, err)
	require.True(t, corr.IsZero())
}
