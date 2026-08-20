package marketdata_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

func TestRegisterFXInstrument_Valid(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	id, err := svc.RegisterFXInstrument(resolver, "oanda", "eurusd")
	require.NoError(t, err)
	require.False(t, id.IsZero())

	listing, err := resolver.ResolveInstrument(id, "oanda", "")
	require.NoError(t, err)
	require.Equal(t, "EURUSD", listing.Symbol())
	require.Equal(t, "oanda", listing.Provider())
}

func TestRegisterFXInstrument_JPYPairsUseLargerTickSize(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	id, err := svc.RegisterFXInstrument(resolver, "oanda", "USDJPY")
	require.NoError(t, err)

	listing, err := resolver.ResolveInstrument(id, "oanda", "")
	require.NoError(t, err)
	require.Equal(t, "0.001", listing.Spec().TickSize().String())
}

func TestRegisterFXInstrument_NonJPYPairsUseStandardTickSize(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	id, err := svc.RegisterFXInstrument(resolver, "oanda", "EURUSD")
	require.NoError(t, err)

	listing, err := resolver.ResolveInstrument(id, "oanda", "")
	require.NoError(t, err)
	require.Equal(t, "0.00001", listing.Spec().TickSize().String())
}

func TestRegisterFXInstrument_RejectsWrongLength(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	_, err := svc.RegisterFXInstrument(resolver, "oanda", "EURO")
	require.Error(t, err)
}

func TestRegisterFXInstrument_RejectsInvalidCurrencyCode(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	_, err := svc.RegisterFXInstrument(resolver, "oanda", "1URUSD")
	require.Error(t, err)
}

func TestRegisterFXInstrument_DuplicateRegistrationFails(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	_, err := svc.RegisterFXInstrument(resolver, "oanda", "EURUSD")
	require.NoError(t, err)

	_, err = svc.RegisterFXInstrument(resolver, "oanda", "EURUSD")
	require.Error(t, err, "registering the exact same provider/venue/symbol twice must fail, per instrument.Resolver's own contract (ADR-016)")
}
