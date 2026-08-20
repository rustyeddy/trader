package marketdata_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

func TestNew_RejectsNilManager(t *testing.T) {
	s, err := svc.New(nil)
	require.Nil(t, s)
	require.ErrorIs(t, err, svc.ErrNilManager)
}

func TestNew_AcceptsConfiguredManager(t *testing.T) {
	t.Parallel()

	manager, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    t.TempDir(),
		Resolver:     instrument.NewMemoryResolver(),
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	s, err := svc.New(manager)
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestErrNilManager_IsStableSentinel(t *testing.T) {
	_, err := svc.New(nil)
	require.True(t, errors.Is(err, svc.ErrNilManager))
}
