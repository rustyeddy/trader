package order

import (
	"testing"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseIntent(t *testing.T, kind IntentKind) Intent {
	t.Helper()
	return Intent{
		IntentID:   mustIntentID(t),
		Kind:       kind,
		Instrument: mustEurUsdInstrument(t),
		Metadata:   id.Metadata{EventID: mustEventID(t)},
	}
}

func TestNewIntentValidEnter(t *testing.T) {
	in := baseIntent(t, IntentEnter)
	in.Side = Buy
	got, err := NewIntent(in)
	require.NoError(t, err)
	assert.Equal(t, IntentEnter, got.Kind)
	assert.Equal(t, Buy, got.Side)
}

func TestNewIntentValidExit(t *testing.T) {
	in := baseIntent(t, IntentExit)
	got, err := NewIntent(in)
	require.NoError(t, err)
	assert.Equal(t, IntentExit, got.Kind)
}

func TestNewIntentValidAdjustStop(t *testing.T) {
	in := baseIntent(t, IntentAdjustStop)
	in.StopPrice = price(t, "1.05000")
	got, err := NewIntent(in)
	require.NoError(t, err)
	require.NotNil(t, got.StopPrice)
	assert.True(t, got.StopPrice.Equal(*price(t, "1.05000")))
}

func TestNewIntentValidTargetExposure(t *testing.T) {
	in := baseIntent(t, IntentTargetExposure)
	in.Side = Sell
	in.Quantity = qty(t, "500")
	got, err := NewIntent(in)
	require.NoError(t, err)
	assert.Equal(t, Sell, got.Side)
	require.NotNil(t, got.Quantity)
	assert.True(t, got.Quantity.Equal(*qty(t, "500")))
}

func TestNewIntentRejectsZeroIntentID(t *testing.T) {
	in := baseIntent(t, IntentExit)
	in.IntentID = id.IntentID{}
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentRejectsZeroInstrument(t *testing.T) {
	in := baseIntent(t, IntentExit)
	in.Instrument = instrument.ID{}
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentRejectsInvalidKind(t *testing.T) {
	in := baseIntent(t, IntentKind(99))
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentRejectsZeroKind(t *testing.T) {
	in := baseIntent(t, intentUnset)
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentRejectsZeroMetadataEventID(t *testing.T) {
	in := baseIntent(t, IntentExit)
	in.Metadata = id.Metadata{}
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentEnterRequiresSide(t *testing.T) {
	in := baseIntent(t, IntentEnter)
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentEnterRejectsQuantity(t *testing.T) {
	in := baseIntent(t, IntentEnter)
	in.Side = Buy
	in.Quantity = qty(t, "100")
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentEnterRejectsStopPrice(t *testing.T) {
	in := baseIntent(t, IntentEnter)
	in.Side = Buy
	in.StopPrice = price(t, "1.10000")
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentExitRejectsSide(t *testing.T) {
	in := baseIntent(t, IntentExit)
	in.Side = Buy
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentExitRejectsQuantity(t *testing.T) {
	in := baseIntent(t, IntentExit)
	in.Quantity = qty(t, "100")
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentExitRejectsStopPrice(t *testing.T) {
	in := baseIntent(t, IntentExit)
	in.StopPrice = price(t, "1.10000")
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentAdjustStopRequiresStopPrice(t *testing.T) {
	in := baseIntent(t, IntentAdjustStop)
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentAdjustStopRejectsSide(t *testing.T) {
	in := baseIntent(t, IntentAdjustStop)
	in.Side = Buy
	in.StopPrice = price(t, "1.10000")
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentAdjustStopRejectsQuantity(t *testing.T) {
	in := baseIntent(t, IntentAdjustStop)
	in.StopPrice = price(t, "1.10000")
	in.Quantity = qty(t, "100")
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentTargetExposureRequiresSide(t *testing.T) {
	in := baseIntent(t, IntentTargetExposure)
	in.Quantity = qty(t, "100")
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentTargetExposureRequiresQuantity(t *testing.T) {
	in := baseIntent(t, IntentTargetExposure)
	in.Side = Buy
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentTargetExposureRejectsZeroQuantity(t *testing.T) {
	in := baseIntent(t, IntentTargetExposure)
	in.Side = Buy
	in.Quantity = qty(t, "0")
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestNewIntentTargetExposureRejectsStopPrice(t *testing.T) {
	in := baseIntent(t, IntentTargetExposure)
	in.Side = Buy
	in.Quantity = qty(t, "100")
	in.StopPrice = price(t, "1.10000")
	_, err := NewIntent(in)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestIntentKindStringUnknownValue(t *testing.T) {
	assert.Equal(t, "IntentKind(99)", IntentKind(99).String())
}

func TestIntentKindStringKnownValues(t *testing.T) {
	assert.Equal(t, "enter", IntentEnter.String())
	assert.Equal(t, "exit", IntentExit.String())
	assert.Equal(t, "adjust_stop", IntentAdjustStop.String())
	assert.Equal(t, "target_exposure", IntentTargetExposure.String())
}
