package external

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func TestNegotiateCapabilities(t *testing.T) {
	fill, bars := v1.Capability_CAPABILITY_FILL_HANDLER, v1.Capability_CAPABILITY_BARS_DELIVERY
	tests := []struct {
		name      string
		requested []v1.Capability
		want      []v1.Capability
	}{
		{"none", nil, nil},
		{"fill", []v1.Capability{fill}, []v1.Capability{fill}},
		{"bars", []v1.Capability{bars}, []v1.Capability{bars}},
		{"both, any order, deduplicated", []v1.Capability{bars, fill, bars, fill}, []v1.Capability{fill, bars}},
		{"unknown dropped", []v1.Capability{v1.Capability_CAPABILITY_UNSPECIFIED, v1.Capability(99)}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, negotiateCapabilities(tt.requested))
		})
	}
}

func TestValidateBarsDelivery(t *testing.T) {
	eur := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	gbp := instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))

	require.ErrorContains(t, validateBarsDelivery(strategy.Descriptor{}), "at least one")
	require.NoError(t, validateBarsDelivery(strategy.Descriptor{Requirements: []strategy.DataRequirement{
		{Instrument: eur, Interval: marketdata.D1}, {Instrument: gbp, Interval: marketdata.D1},
	}}))
	require.ErrorContains(t, validateBarsDelivery(strategy.Descriptor{Requirements: []strategy.DataRequirement{
		{Instrument: eur, Interval: marketdata.H1}, {Instrument: gbp, Interval: marketdata.D1},
	}}), "single interval")
}

func TestNewExternalStrategyAdapter_SelectsWrapperByCapabilities(t *testing.T) {
	fill, bars := v1.Capability_CAPABILITY_FILL_HANDLER, v1.Capability_CAPABILITY_BARS_DELIVERY
	tests := []struct {
		caps          []v1.Capability
		bars, hasFill bool
	}{
		{nil, false, false},
		{[]v1.Capability{fill}, false, true},
		{[]v1.Capability{bars}, true, false},
		{[]v1.Capability{fill, bars}, true, true},
	}
	for _, tt := range tests {
		s := newExternalStrategyAdapter(newRunSession("s", strategy.Descriptor{}, tt.caps, 0), nil)
		_, isBars := s.(strategy.BarsHandler)
		_, isFill := s.(strategy.FillHandler)
		require.Equal(t, tt.bars, isBars, "caps %v", tt.caps)
		require.Equal(t, tt.hasFill, isFill, "caps %v", tt.caps)
	}
}

func TestExternalBars_OnBarIsAContractViolation(t *testing.T) {
	s := newExternalStrategyAdapter(newRunSession("s", strategy.Descriptor{}, []v1.Capability{v1.Capability_CAPABILITY_BARS_DELIVERY}, 0), nil)
	_, err := s.OnBar(context.Background(), strategy.BarEvent{}, nil)
	require.ErrorContains(t, err, "OnBars")
}
