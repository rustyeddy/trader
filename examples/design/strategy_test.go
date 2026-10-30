package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testStart is the fixed timestamp of the first test bar (a Monday).
var testStart = time.Date(2010, 1, 4, 0, 0, 0, 0, time.UTC)

// closes builds consecutive daily test bars for "TEST" with the given closes.
func closes(vals ...float64) []bar {
	bars := make([]bar, len(vals))
	for i, v := range vals {
		bars[i] = bar{
			instrument: "TEST",
			Time:       testStart.AddDate(0, 0, i),
			ohlc:       ohlc{c: price(v * priceScale)},
		}
	}
	return bars
}

func newTestEmax(t *testing.T, short, long int) *emax {
	t.Helper()
	e := &emax{short: short, long: long}
	require.NoError(t, e.init(nil))
	return e
}

func TestEmax_HoldsUntilBothWarm(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	// Falling prices: once warm, short < long. No signal, and especially no
	// close, while the long EMA is still warming up (first 3 bars).
	for i, b := range closes(10, 9, 8) {
		assert.Equal(t, hold, e.onBar(ctx, b).action, "bar %d", i)
	}
}

func TestEmax_LongWarmsOnSameBars(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	for _, b := range closes(1, 2, 3, 4) {
		e.onBar(ctx, b)
	}
	// Regression: the long EMA must be seeded from bars 1-4 (value 2.5). If it
	// were only fed after the short EMA warmed, the next value would differ.
	v, ok := e.emaLong.onBar(ctx, bar{ohlc: ohlc{c: price(4 * priceScale)}})
	require.True(t, ok)
	assert.InDelta(t, 2.5+0.4*(4-2.5), v, 1e-9)
}

func TestEmax_Crossovers(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	// Down, then sharply up (short crosses above long), then sharply down.
	series := closes(10, 9, 8, 7, 6, 12, 14, 16, 8, 4, 2)
	var got []action
	for _, b := range series {
		got = append(got, e.onBar(ctx, b).action)
	}

	// Warm on bar index 3. The short EMA crosses above the long one on the
	// jump to 12 (index 5) and back below on the drop to 8 (index 8).
	want := []action{hold, hold, hold, hold, hold, open, hold, hold, close, hold, hold}
	assert.Equal(t, want, got)
}

func TestEmax_NoRepeatedOpen(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	var opens int
	for _, b := range closes(10, 9, 8, 7, 6, 12, 14, 16, 18, 20, 22, 24) {
		if e.onBar(ctx, b).action == open {
			opens++
		}
	}
	assert.Equal(t, 1, opens, "a sustained bullish state must not re-emit open")
}

func TestEmax_FirstValidBarAlreadyBullish(t *testing.T) {
	// Documents current behavior: with short already above long on the first
	// valid bar there is no crossover, but emax still emits open.
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	var got []action
	for _, b := range closes(1, 2, 3, 4) {
		got = append(got, e.onBar(ctx, b).action)
	}
	assert.Equal(t, []action{hold, hold, hold, open}, got)
}

func TestEmax_Init(t *testing.T) {
	tests := []struct {
		name        string
		short, long int
		wantErr     bool
	}{
		{"valid", 2, 5, false},
		{"zero short", 0, 5, true},
		{"zero long", 2, 0, true},
		{"negative short", -1, 5, true},
		{"equal windows", 5, 5, true},
		{"short above long", 6, 5, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &emax{short: tt.short, long: tt.long}
			err := e.init(nil)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, e.emaShort)
				assert.Nil(t, e.emaLong)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, e.emaShort)
			assert.NotNil(t, e.emaLong)
		})
	}
}

func TestEmax_IntentsCarryInstrumentAndDirection(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	var opens, closes_ []intent
	for _, b := range closes(10, 9, 8, 7, 6, 12, 14, 16, 8, 4, 2) {
		switch i := e.onBar(ctx, b); i.action {
		case open:
			opens = append(opens, i)
		case close:
			closes_ = append(closes_, i)
		default:
			assert.Equal(t, intent{instrument: "TEST"}, i, "hold names its instrument")
		}
	}
	assert.Equal(t, []intent{{instrument: "TEST", direction: long, action: open}}, opens)
	assert.Equal(t, []intent{{instrument: "TEST", action: close}}, closes_, "close takes no direction")
}

// There is no atomic reverse: a position change is a close followed, on a
// later bar, by a separate open.
func TestEmax_ReversalIsCloseThenSeparateOpen(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	series := closes(10, 9, 8, 7, 6, 12, 14, 16, 8, 4, 2, 1, 14, 20, 30, 10, 2)
	var seq []action
	var openBar, closeBar []int
	for i, b := range series {
		in := e.onBar(ctx, b)
		switch in.action {
		case open:
			openBar = append(openBar, i)
			seq = append(seq, in.action)
		case close:
			closeBar = append(closeBar, i)
			seq = append(seq, in.action)
		}
	}

	assert.Equal(t, []action{open, close, open, close}, seq, "opens and closes strictly alternate")
	for i := range closeBar {
		assert.Less(t, openBar[i], closeBar[i], "close follows its open")
	}
	assert.Less(t, closeBar[0], openBar[1], "re-open happens on a later bar than the close")
}

func TestEmax_HoldsCarryInstrumentAndValidate(t *testing.T) {
	e := newTestEmax(t, 2, 4)
	ctx := context.Background()

	for i, b := range closes(10, 9, 8, 7, 6, 12, 14, 16, 8, 4, 2) {
		in := e.onBar(ctx, b)
		assert.Equal(t, "TEST", in.instrument, "bar %d (%s)", i, in.action)
		assert.NoError(t, in.validate(), "bar %d (%s)", i, in.action)
	}
}

func TestEmax_Name(t *testing.T) {
	var s strategy = &emax{id: "emax-20-50"}
	assert.Equal(t, "emax-20-50", s.name())
}
