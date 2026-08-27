package execution

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

func TestParseIntentSide(t *testing.T) {
	cases := []struct {
		in   string
		want order.Side
	}{
		{"buy", order.Buy},
		{"BUY", order.Buy},
		{" Sell ", order.Sell},
	}
	for _, c := range cases {
		got, err := parseIntentSide(c.in)
		require.NoError(t, err, c.in)
		require.Equal(t, c.want, got, c.in)
	}

	_, err := parseIntentSide("sideways")
	require.ErrorContains(t, err, "invalid --side")
}

func TestBuildEnterIntent(t *testing.T) {
	gen := id.NewGenerator(clock.NewSimulated(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)

	intent, err := buildEnterIntent(gen, inst.ID(), order.Buy)
	require.NoError(t, err)
	require.Equal(t, order.IntentEnter, intent.Kind)
	require.Equal(t, order.Buy, intent.Side)
	require.False(t, intent.IntentID.IsZero())
	require.False(t, intent.Metadata.EventID.IsZero())
	require.False(t, intent.Metadata.CorrelationID.IsZero())
}

func TestBuildSizingParams(t *testing.T) {
	t.Run("valid risk fraction and adverse distance, no reference price", func(t *testing.T) {
		riskFraction, adverse, ref, err := buildSizingParams(intentFlags{
			riskFraction:    "0.01",
			adverseDistance: "0.01000",
		})
		require.NoError(t, err)
		require.Equal(t, num.MustParseRate("0.01"), riskFraction)
		require.NotNil(t, adverse)
		require.Equal(t, num.MustParsePrice("0.01000"), *adverse)
		require.Nil(t, ref)
	})

	t.Run("reference price is threaded through when supplied", func(t *testing.T) {
		_, _, ref, err := buildSizingParams(intentFlags{
			riskFraction:    "0.01",
			adverseDistance: "0.01000",
			referencePrice:  "1.10000",
		})
		require.NoError(t, err)
		require.NotNil(t, ref)
		require.Equal(t, num.MustParsePrice("1.10000"), *ref)
	})

	t.Run("invalid risk fraction is rejected", func(t *testing.T) {
		_, _, _, err := buildSizingParams(intentFlags{riskFraction: "not-a-number", adverseDistance: "0.01000"})
		require.ErrorContains(t, err, "--risk-fraction")
	})

	t.Run("invalid adverse distance is rejected", func(t *testing.T) {
		_, _, _, err := buildSizingParams(intentFlags{riskFraction: "0.01", adverseDistance: "not-a-number"})
		require.ErrorContains(t, err, "--adverse-distance")
	})

	t.Run("invalid reference price is rejected", func(t *testing.T) {
		_, _, _, err := buildSizingParams(intentFlags{riskFraction: "0.01", adverseDistance: "0.01000", referencePrice: "not-a-number"})
		require.ErrorContains(t, err, "--reference-price")
	})
}
