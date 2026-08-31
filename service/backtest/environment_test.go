package backtest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
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
