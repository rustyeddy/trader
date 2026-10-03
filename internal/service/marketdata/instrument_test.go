package marketdata_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
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

// TestRegisterFXInstrument_RepeatRegistrationIsIdempotent replaces the
// former "duplicate registration fails" test: ADR-069 Decision 2 (issue
// #448) makes service registration idempotent. instrument.MemoryResolver
// itself still rejects duplicates (ADR-016); the service layer treats an
// identical existing listing as success.
func TestRegisterFXInstrument_RepeatRegistrationIsIdempotent(t *testing.T) {
	resolver := instrument.NewMemoryResolver()

	first, err := svc.RegisterFXInstrument(resolver, "oanda", "EURUSD")
	require.NoError(t, err)

	second, err := svc.RegisterFXInstrument(resolver, "oanda", "eurusd")
	require.NoError(t, err)
	require.True(t, first.Equal(second))
}
