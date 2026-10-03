package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svcmarketdata "github.com/rustyeddy/trader/internal/service/marketdata"
)

type fakeMarketData struct {
	asked []string
	err   error
}

func (f *fakeMarketData) DefaultProvider() string { return "oanda" }

func (f *fakeMarketData) ForProvider(provider string) (MarketData, error) {
	f.asked = append(f.asked, provider)
	if f.err != nil {
		return nil, f.err
	}
	return providerOnly(provider), nil
}

// providerOnly is a MarketData that knows only its provider.
type providerOnly string

func (p providerOnly) Provider() string { return string(p) }

func (providerOnly) ResolveInstruments(context.Context, svcmarketdata.ResolveInstrumentsRequest) (svcmarketdata.ResolveInstrumentsResponse, error) {
	return svcmarketdata.ResolveInstrumentsResponse{}, errors.New("not implemented")
}

func (providerOnly) Coverage(context.Context, svcmarketdata.CoverageRequest) (svcmarketdata.CoverageResponse, error) {
	return svcmarketdata.CoverageResponse{}, errors.New("not implemented")
}

func (providerOnly) Inventory(context.Context, svcmarketdata.InventoryRequest) (svcmarketdata.InventoryResponse, error) {
	return svcmarketdata.InventoryResponse{}, errors.New("not implemented")
}

func TestRequireWrites(t *testing.T) {
	var logs bytes.Buffer
	s := &server{deps: Deps{Logger: slog.New(slog.NewTextHandler(&logs, nil))}}
	err := s.requireWrites("trader_marketdata_update")
	require.ErrorIs(t, err, ErrWritesDisabled)
	assert.Contains(t, err.Error(), "trader_marketdata_update")
	assert.Contains(t, err.Error(), "--allow-writes", "the error says how to enable writes")
	assert.Contains(t, logs.String(), "mcp write tool refused")

	s.deps.AllowWrites = true
	assert.NoError(t, s.requireWrites("trader_marketdata_update"))
}

func TestMarketData(t *testing.T) {
	t.Run("not configured", func(t *testing.T) {
		_, err := (&server{}).marketData("stooq")
		assert.ErrorIs(t, err, ErrMarketDataUnavailable)
	})
	t.Run("delegates to the factory per provider", func(t *testing.T) {
		f := &fakeMarketData{}
		s := &server{deps: Deps{MarketData: f}}
		md, err := s.marketData("stooq")
		require.NoError(t, err)
		assert.Equal(t, "stooq", md.Provider())
		_, _ = s.marketData("")
		assert.Equal(t, []string{"stooq", ""}, f.asked)
	})
	t.Run("factory errors pass through", func(t *testing.T) {
		boom := errors.New("unknown provider")
		_, err := (&server{deps: Deps{MarketData: &fakeMarketData{err: boom}}}).marketData("x")
		assert.ErrorIs(t, err, boom)
	})
}

func TestNew_NilLoggerIsSafe(t *testing.T) {
	assert.NotNil(t, New(Deps{}))
}
