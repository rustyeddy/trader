package backtest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svcbacktest "github.com/rustyeddy/trader/service/backtest"
)

func TestService_New_RequiresManager(t *testing.T) {
	_, err := svcbacktest.New(nil, newFixtureResolver(t), &fakeEnvironmentFactory{}, nil)
	require.ErrorIs(t, err, svcbacktest.ErrNilManager)
}

func TestService_New_RequiresResolver(t *testing.T) {
	_, err := svcbacktest.New(newFixtureManager(t), nil, &fakeEnvironmentFactory{}, nil)
	require.ErrorIs(t, err, svcbacktest.ErrNilResolver)
}

func TestService_New_RequiresEnvironmentFactory(t *testing.T) {
	_, err := svcbacktest.New(newFixtureManager(t), newFixtureResolver(t), nil, nil)
	require.ErrorIs(t, err, svcbacktest.ErrNilEnvironments)
}

func TestService_Run_RejectsInvalidRequestBeforeTouchingFactory(t *testing.T) {
	factory := &fakeEnvironmentFactory{}
	svc, err := svcbacktest.New(newFixtureManager(t), newFixtureResolver(t), factory, nil)
	require.NoError(t, err)

	req := validRunRequest(t)
	req.Strategy = nil

	_, err = svc.Run(context.Background(), req)
	require.ErrorIs(t, err, svcbacktest.ErrInvalidRequest)
	assert.False(t, factory.called, "an invalid request must never reach the environment factory")
}

func TestService_Run_NeverCallsFactoryOnPreCanceledContext(t *testing.T) {
	factory := &fakeEnvironmentFactory{env: validEnvironment(t)}
	svc, err := svcbacktest.New(newFixtureManager(t), newFixtureResolver(t), factory, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.Run(ctx, validRunRequest(t))
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, factory.called, "a pre-canceled request must never construct/open a broker via the environment factory")
}

func TestService_Run_PropagatesEnvironmentFactoryError(t *testing.T) {
	factoryErr := errors.New("boom: no capacity for a fresh broker")
	factory := &fakeEnvironmentFactory{err: factoryErr}
	svc, err := svcbacktest.New(newFixtureManager(t), newFixtureResolver(t), factory, nil)
	require.NoError(t, err)

	_, err = svc.Run(context.Background(), validRunRequest(t))
	require.ErrorIs(t, err, factoryErr)
	assert.True(t, factory.called)
}

func TestService_Run_SuccessfulRun(t *testing.T) {
	svc, err := svcbacktest.New(newFixtureManager(t), newFixtureResolver(t), simEnvironmentFactory{}, nil)
	require.NoError(t, err)

	resp, err := svc.Run(context.Background(), validRunRequest(t))
	require.NoError(t, err)

	assert.False(t, resp.Manifest.RunID().IsZero())
	assert.Equal(t, "noop", resp.Manifest.StrategyName())
	assert.True(t, resp.Manifest.StartingCapital().Equal(validRunRequest(t).StartingCapital))
	assert.Empty(t, resp.Trades, "noopStrategy never trades")
	assert.Empty(t, resp.OpenTrades)
	require.NotEmpty(t, resp.EquityCurve, "at least the run's own starting observation")
	assert.Equal(t, 0, resp.Metrics.TradeCount())
}
