package strategy

import (
	"strings"
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/require"
)

// warmUpChandelier feeds n+1 identical candles so the ATR is ready.
// With flat candles (High=110, Low=90, Close=100), TR=20 every period → ATR=20.
func warmUpChandelier(c *ChandelierExit, period int) {
	for i := 0; i <= period; i++ {
		c.Tick(market.Candle{Open: 100, High: 110, Low: 90, Close: 100, Ticks: 1})
	}
}

// ---------------------------------------------------------------------------
// NewChandelierExit
// ---------------------------------------------------------------------------

func TestNewChandelierExit_Valid(t *testing.T) {
	t.Parallel()

	c, err := NewChandelierExit(3, 2.0, market.PriceScale)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewChandelierExit_ZeroPeriod(t *testing.T) {
	t.Parallel()

	_, err := NewChandelierExit(0, 2.0, market.PriceScale)
	require.Error(t, err)
	require.Contains(t, err.Error(), "period")
}

func TestNewChandelierExit_NegativePeriod(t *testing.T) {
	t.Parallel()

	_, err := NewChandelierExit(-1, 2.0, market.PriceScale)
	require.Error(t, err)
}

func TestNewChandelierExit_ZeroScale(t *testing.T) {
	t.Parallel()

	_, err := NewChandelierExit(3, 2.0, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "scale")
}

// ---------------------------------------------------------------------------
// Name
// ---------------------------------------------------------------------------

func TestChandelierExit_Name(t *testing.T) {
	t.Parallel()

	c, err := NewChandelierExit(3, 2.5, market.PriceScale)
	require.NoError(t, err)

	name := c.Name()
	require.True(t, strings.HasPrefix(name, "Chandelier(ATR3"), "got %q", name)
	require.Contains(t, name, "2.5")
}

// ---------------------------------------------------------------------------
// Ready / Tick
// ---------------------------------------------------------------------------

func TestChandelierExit_NotReadyBeforeWarmup(t *testing.T) {
	t.Parallel()

	c, err := NewChandelierExit(2, 2.0, market.PriceScale)
	require.NoError(t, err)
	require.False(t, c.Ready())

	// Feed only period candles (not period+1)
	for i := 0; i < 2; i++ {
		c.Tick(market.Candle{Open: 100, High: 110, Low: 90, Close: 100, Ticks: 1})
	}
	require.False(t, c.Ready())
}

func TestChandelierExit_ReadyAfterWarmup(t *testing.T) {
	t.Parallel()

	c, err := NewChandelierExit(2, 2.0, market.PriceScale)
	require.NoError(t, err)

	warmUpChandelier(c, 2)
	require.True(t, c.Ready())
}

// ---------------------------------------------------------------------------
// InitialStop
// ---------------------------------------------------------------------------

func TestChandelierExit_InitialStop_NotReady(t *testing.T) {
	t.Parallel()

	c, err := NewChandelierExit(2, 2.0, market.PriceScale)
	require.NoError(t, err)

	// Not yet warmed up → should return 0
	stop := c.InitialStop(market.Long, 100, market.Candle{})
	require.Equal(t, market.Price(0), stop)

	stop = c.InitialStop(market.Short, 100, market.Candle{})
	require.Equal(t, market.Price(0), stop)
}

func TestChandelierExit_InitialStop_Long(t *testing.T) {
	t.Parallel()

	// period=2, multiplier=2.0; flat candles → ATR=20, offset=40
	c, err := NewChandelierExit(2, 2.0, market.PriceScale)
	require.NoError(t, err)
	warmUpChandelier(c, 2)

	// entry=100, offset=40 → stop = 60
	stop := c.InitialStop(market.Long, 100, market.Candle{})
	require.Equal(t, market.Price(60), stop)
}

func TestChandelierExit_InitialStop_Short(t *testing.T) {
	t.Parallel()

	// period=2, multiplier=2.0; flat candles → ATR=20, offset=40
	c, err := NewChandelierExit(2, 2.0, market.PriceScale)
	require.NoError(t, err)
	warmUpChandelier(c, 2)

	// entry=100, offset=40 → stop = 140
	stop := c.InitialStop(market.Short, 100, market.Candle{})
	require.Equal(t, market.Price(140), stop)
}

func TestChandelierExit_InitialStop_LongUnderflow(t *testing.T) {
	t.Parallel()

	// multiplier=10 → offset=200, entry=10 → would underflow → clamped to 0
	c, err := NewChandelierExit(2, 10.0, market.PriceScale)
	require.NoError(t, err)
	warmUpChandelier(c, 2)

	stop := c.InitialStop(market.Long, 10, market.Candle{})
	require.Equal(t, market.Price(0), stop)
}

func TestChandelierExit_InitialStop_UnknownSide(t *testing.T) {
	t.Parallel()

	c, err := NewChandelierExit(2, 2.0, market.PriceScale)
	require.NoError(t, err)
	warmUpChandelier(c, 2)

	// side=0 is neither Long nor Short → returns 0
	stop := c.InitialStop(0, 100, market.Candle{})
	require.Equal(t, market.Price(0), stop)
}

// ---------------------------------------------------------------------------
// UpdateStop
// ---------------------------------------------------------------------------

func TestChandelierExit_UpdateStop_NotReady(t *testing.T) {
	t.Parallel()

	c, err := NewChandelierExit(2, 2.0, market.PriceScale)
	require.NoError(t, err)

	// Not ready → returns existing stop unchanged
	stop := c.UpdateStop(market.Long, 55, 0, 110, market.Candle{})
	require.Equal(t, market.Price(55), stop)
}

func TestChandelierExit_UpdateStop_Long_Advances(t *testing.T) {
	t.Parallel()

	// ATR=20, offset=40; extreme=150 → candidate=110 > currentStop=60 → advance
	c, err := NewChandelierExit(2, 2.0, market.PriceScale)
	require.NoError(t, err)
	warmUpChandelier(c, 2)

	stop := c.UpdateStop(market.Long, 60, 0, 150, market.Candle{})
	require.Equal(t, market.Price(110), stop)
}

func TestChandelierExit_UpdateStop_Long_Holds(t *testing.T) {
	t.Parallel()

	// extreme=90 → candidate=50 < currentStop=60 → hold at 60
	c, err := NewChandelierExit(2, 2.0, market.PriceScale)
	require.NoError(t, err)
	warmUpChandelier(c, 2)

	stop := c.UpdateStop(market.Long, 60, 0, 90, market.Candle{})
	require.Equal(t, market.Price(60), stop)
}

func TestChandelierExit_UpdateStop_Short_InitialSet(t *testing.T) {
	t.Parallel()

	// currentStop=0 → always set to extreme+offset
	c, err := NewChandelierExit(2, 2.0, market.PriceScale)
	require.NoError(t, err)
	warmUpChandelier(c, 2)

	// extreme=80, offset=40 → candidate=120; currentStop=0 → set to 120
	stop := c.UpdateStop(market.Short, 0, 0, 80, market.Candle{})
	require.Equal(t, market.Price(120), stop)
}

func TestChandelierExit_UpdateStop_Short_Tightens(t *testing.T) {
	t.Parallel()

	// extreme falls further → candidate falls → tighten stop
	c, err := NewChandelierExit(2, 2.0, market.PriceScale)
	require.NoError(t, err)
	warmUpChandelier(c, 2)

	// extreme=70, offset=40 → candidate=110 < currentStop=120 → tighten
	stop := c.UpdateStop(market.Short, 120, 0, 70, market.Candle{})
	require.Equal(t, market.Price(110), stop)
}

func TestChandelierExit_UpdateStop_Short_Holds(t *testing.T) {
	t.Parallel()

	// candidate >= currentStop → hold
	c, err := NewChandelierExit(2, 2.0, market.PriceScale)
	require.NoError(t, err)
	warmUpChandelier(c, 2)

	// extreme=90, offset=40 → candidate=130 > currentStop=120 → hold at 120
	stop := c.UpdateStop(market.Short, 120, 0, 90, market.Candle{})
	require.Equal(t, market.Price(120), stop)
}
