package emax

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The example is written against the public SDK alone: it must import nothing from
// Trader's private packages, or an outside worker could not be written the way it
// is. (The root package's parity test imports this one, not the other way round.)
func TestPackage_UsesOnlyThePublicSDK(t *testing.T) {
	const mod = "github.com/rustyeddy/trader/"
	public := map[string]bool{mod + "strategy": true, mod + "marketdata": true, mod + "num": true}
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err)
			if strings.HasPrefix(path, mod) {
				assert.True(t, public[path], "%s imports %s", name, path)
			}
		}
	}
}

func price(s string) num.Price { return num.MustParsePrice(s) }

func cfg() Config {
	return Config{Short: 2, Long: 4, StopPct: num.MustParseRate("0.01"), Tick: price("0.00001")}
}

func bar(instrument, close string) marketdata.Bar {
	c := price(close)
	return marketdata.Bar{Instrument: instrument, Mid: marketdata.OHLC{Open: c, High: c, Low: c, Close: c}}
}

func TestConfig_Validate(t *testing.T) {
	require.NoError(t, cfg().Validate())
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"short window", func(c *Config) { c.Short = 0 }, "windows must be > 0"},
		{"long window", func(c *Config) { c.Long = -1 }, "windows must be > 0"},
		{"short not below long", func(c *Config) { c.Short = 4 }, "must be less than"},
		{"no stop", func(c *Config) { c.StopPct = num.Rate{} }, "stop must be between"},
		{"stop of the whole price", func(c *Config) { c.StopPct = num.MustParseRate("1") }, "stop must be between"},
		{"no tick", func(c *Config) { c.Tick = num.Price{} }, "no price tick"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cfg()
			tt.edit(&c)
			assert.ErrorContains(t, c.Validate(), tt.want)
			_, err := New(c)
			assert.Error(t, err)
		})
	}
}

func TestEMA(t *testing.T) {
	e := NewEMA(3)
	_, ok := e.OnBar(bar("X", "3"))
	assert.False(t, ok, "not seeded until a full window")
	_, ok = e.OnBar(bar("X", "6"))
	assert.False(t, ok)
	v, ok := e.OnBar(bar("X", "9"))
	require.True(t, ok)
	assert.Equal(t, 6.0, v, "seeded with the simple average of the first window")
	v, _ = e.OnBar(bar("X", "10"))
	assert.Equal(t, 8.0, v, "then 2/(window+1) of the way to each close: 6 + 0.5*(10-6)")

	assert.Panics(t, func() { NewEMA(0) })
}

func TestOnBars_FlipsByAClosingThenAnOpeningBar(t *testing.T) {
	s, err := New(cfg())
	require.NoError(t, err)
	step := func(close string) (actions []string) {
		orders, err := s.OnBars([]marketdata.Bar{bar("EURUSD", close)}, nil)
		require.NoError(t, err)
		for _, o := range orders {
			actions = append(actions, o.Action+" "+o.Direction)
		}
		return actions
	}

	for _, c := range []string{"1.1", "1.1", "1.1"} {
		assert.Empty(t, step(c), "the long EMA is not seeded yet")
	}
	assert.Equal(t, []string{"open long"}, step("1.2"), "short EMA above long: open long")
	assert.Empty(t, step("1.3"), "still long: nothing to say")
	assert.Equal(t, []string{"close "}, step("1.0"), "the trend turned: a close, with no direction")
	assert.Equal(t, []string{"open short"}, step("0.9"), "and the opposite open on the next bar")
}

func TestOnBars_AnOpenCarriesTheEntryAndAStopAWholeNumberOfTicksAway(t *testing.T) {
	s, err := New(cfg())
	require.NoError(t, err)
	var open []string
	var stops []num.Price
	for _, c := range []string{"1.1", "1.1", "1.1", "1.2"} {
		orders, err := s.OnBars([]marketdata.Bar{bar("EURUSD", c)}, nil)
		require.NoError(t, err)
		for _, o := range orders {
			open = append(open, o.Price.String())
			stops = append(stops, o.StopLoss)
		}
	}
	require.Equal(t, []string{"1.2"}, open, "reference price is the close")
	assert.Equal(t, price("1.188"), stops[0], "1% of 1.2 is 0.012, below a long")
}

func TestOnBars_EachInstrumentHasItsOwnState(t *testing.T) {
	s, err := New(cfg())
	require.NoError(t, err)
	// EURUSD trends up and GBPUSD stays flat, in the same groups.
	var got []string
	for _, c := range []string{"1.1", "1.1", "1.1", "1.2"} {
		orders, err := s.OnBars([]marketdata.Bar{bar("EURUSD", c), bar("GBPUSD", "1.3")}, nil)
		require.NoError(t, err)
		for _, o := range orders {
			got = append(got, o.Instrument+" "+o.Action)
		}
	}
	assert.Equal(t, []string{"EURUSD open"}, got)
}

func TestOnBars_AShortWhoseStopWouldExceedTheLargestPriceIsSkippedAndNotRetried(t *testing.T) {
	s, err := New(cfg())
	require.NoError(t, err)
	var asked []string
	// Falling closes near the ceiling: the short entry at 9950 has a stop 1% above, over 10000.
	for _, c := range []string{"9990", "9990", "9990", "9950", "9940"} {
		orders, err := s.OnBars([]marketdata.Bar{bar("X", c)}, nil)
		require.NoError(t, err)
		for _, o := range orders {
			asked = append(asked, o.Action+" "+o.Direction)
		}
	}
	assert.Empty(t, asked, "the open is skipped, and the signal is consumed, so it is not asked again on the next bar")
}
