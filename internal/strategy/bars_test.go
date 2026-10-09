package strategy_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

func TestNewBarsEvent_PartitionsInRequirementOrder(t *testing.T) {
	id := func(b string) instrument.ID {
		return instrument.CurrencyPairID(num.MustParseCurrency(b), num.MustParseCurrency("USD"))
	}
	eur, gbp, aud := id("EUR"), id("GBP"), id("AUD")
	ts := time.Date(2024, 1, 2, 15, 0, 0, 0, time.UTC)
	reqs := []strategy.DataRequirement{
		{Instrument: gbp, Interval: marketdata.D1},
		{Instrument: aud, Interval: marketdata.D1},
		{Instrument: eur, Interval: marketdata.D1},
	}
	// Present in a different order than declared: output must follow
	// the declaration order, not arrival order.
	present := []strategy.BarEvent{{Instrument: eur}, {Instrument: gbp}}

	ev := strategy.NewBarsEvent(ts, reqs, present)
	require.True(t, ev.Boundary.Equal(ts))
	require.Equal(t, marketdata.D1, ev.Interval)
	require.Len(t, ev.Bars, 2)
	require.True(t, ev.Bars[0].Instrument.Equal(gbp))
	require.True(t, ev.Bars[1].Instrument.Equal(eur))
	require.Len(t, ev.Missing, 1)
	require.True(t, ev.Missing[0].Equal(aud))
}
