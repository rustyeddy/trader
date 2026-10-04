package marketdata

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderRegistry(t *testing.T) {
	assert.Equal(t, []string{"alpaca", "oanda", "stooq"}, ProviderNames(), "sorted")

	oanda, err := LookupProvider("oanda")
	require.NoError(t, err)
	assert.Equal(t, ProviderInfo{Name: "oanda", AssetClass: AssetClassFX, Calendar: CalendarFX, LiveAcquisition: true, Credentials: CredentialsToken}, oanda)

	alpaca, err := LookupProvider("alpaca")
	require.NoError(t, err)
	assert.Equal(t, ProviderInfo{Name: "alpaca", AssetClass: AssetClassUSEquity, Calendar: CalendarUSEquity, LiveAcquisition: true, Credentials: CredentialsKeyPair}, alpaca)

	stooq, err := LookupProvider("stooq")
	require.NoError(t, err)
	assert.Equal(t, ProviderInfo{Name: "stooq", AssetClass: AssetClassUSEquity, Calendar: CalendarUSEquity, NativeArchive: true}, stooq)

	for _, name := range []string{"", "OANDA", "bloomberg"} {
		_, err := LookupProvider(name)
		assert.ErrorIs(t, err, ErrUnknownProvider, name)
	}
	_, err = LookupProvider("bloomberg")
	assert.ErrorContains(t, err, `"bloomberg" (supported: alpaca, oanda, stooq)`)

	all := Providers()
	require.Len(t, all, 3)
	all[0].Name = "mutated"
	assert.Equal(t, "alpaca", Providers()[0].Name, "Providers returns a copy")
}

func TestProviderEnumStrings(t *testing.T) {
	assert.Equal(t, "fx", AssetClassFX.String())
	assert.Equal(t, "us-equity", AssetClassUSEquity.String())
	assert.Equal(t, "AssetClass(0)", AssetClass(0).String())
	assert.Equal(t, "fx", CalendarFX.String())
	assert.Equal(t, "us-equity", CalendarUSEquity.String())
	assert.Equal(t, "CalendarKind(9)", CalendarKind(9).String())
}

func TestNew_RejectsUnknownProvider(t *testing.T) {
	_, err := New(Config{Clock: testClock(), StoreRoot: t.TempDir(), Resolver: testResolver(t), ProviderName: "bloomberg"})
	require.ErrorIs(t, err, ErrInvalidConfig)
	assert.ErrorIs(t, err, ErrUnknownProvider, "an unknown provider is never treated as oanda")
}

func TestManager_CapabilitiesFollowRegistry(t *testing.T) {
	oanda := newTestManagerWithRaw(t, t.TempDir())
	assert.Equal(t, "oanda", oanda.Provider().Name)
	assert.True(t, oanda.allowsLiveExtend())

	stooq := newStooqTestManager(t, t.TempDir())
	assert.False(t, stooq.allowsLiveExtend(), "no live acquisition")
	_, err := stooq.Sync(context.Background(), Plan{Actions: []Action{downloadAction(2020, 3)}})
	require.ErrorIs(t, err, ErrInvalidConfig)
	assert.ErrorContains(t, err, `provider "stooq" has no live acquisition client`)
}
