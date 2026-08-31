package backtest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/broker"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
)

func TestService_Run_RejectsEnvironmentMissingClock(t *testing.T) {
	env := validEnvironment(t)
	env.Clock = nil
	assertRunRejectsEnvironment(t, env)
}

func TestService_Run_RejectsEnvironmentMissingIDs(t *testing.T) {
	env := validEnvironment(t)
	env.IDs = nil
	assertRunRejectsEnvironment(t, env)
}

func TestService_Run_RejectsEnvironmentMissingAccount(t *testing.T) {
	env := validEnvironment(t)
	env.Account = nil
	assertRunRejectsEnvironment(t, env)
}

func TestService_Run_RejectsEnvironmentMissingPipeline(t *testing.T) {
	env := validEnvironment(t)
	env.Pipeline = nil
	assertRunRejectsEnvironment(t, env)
}

func TestService_Run_RejectsEnvironmentMissingFillModel(t *testing.T) {
	env := validEnvironment(t)
	env.FillModel = backtest.ComponentInfo{}
	assertRunRejectsEnvironment(t, env)
}

// TestService_Run_RejectsAccountNotImplementingMarketObserver is the
// issue #239 review regression: backtest.RunnerParams.validate()
// itself requires Account to implement backtest.MarketObserver for a
// mark-to-market equity curve, so Environment.validate() must catch
// the same composition bug at the service boundary
// (ErrInvalidEnvironment), not let it escape as the less-local
// backtest.ErrInvalidRunnerParams.
func TestService_Run_RejectsAccountNotImplementingMarketObserver(t *testing.T) {
	env := validEnvironment(t)
	env.Account = nonObservingAccount{Account: env.Account}
	assertRunRejectsEnvironment(t, env)
}

// nonObservingAccount wraps a real broker.Account but, by embedding
// the interface rather than the concrete type, exposes only broker.
// Account's own method set — backtest.MarketObserver's ObserveMark is
// not promoted, even though the wrapped sim account implements it.
type nonObservingAccount struct {
	broker.Account
}

// validEnvironment returns a well-formed Environment by delegating to
// simEnvironmentFactory — the same construction the successful-run
// test uses — so each rejection test only has to zero out the one
// field it's testing.
func validEnvironment(t *testing.T) svcbacktest.Environment {
	t.Helper()
	env, err := simEnvironmentFactory{}.NewEnvironment(context.Background(), svcbacktest.EnvironmentRequest{
		StartingCapital: validRunRequest(t).StartingCapital,
		Span:            fixtureSpan(t),
	})
	require.NoError(t, err)
	return env
}

// assertRunRejectsEnvironment asserts that Run rejects env with
// svcbacktest.ErrInvalidEnvironment before ever constructing a Runner.
func assertRunRejectsEnvironment(t *testing.T, env svcbacktest.Environment) {
	t.Helper()
	svc, err := svcbacktest.New(newFixtureManager(t), newFixtureResolver(t), &fakeEnvironmentFactory{env: env}, nil)
	require.NoError(t, err)

	_, err = svc.Run(context.Background(), validRunRequest(t))
	require.ErrorIs(t, err, svcbacktest.ErrInvalidEnvironment)
}
