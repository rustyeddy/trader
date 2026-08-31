package backtest_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/logging"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
)

func TestService_Run_LogsSuccessWithCanonicalAttributes(t *testing.T) {
	logger, rec := logging.Capture()
	svc, err := svcbacktest.New(newFixtureManager(t), newFixtureResolver(t), simEnvironmentFactory{}, logger)
	require.NoError(t, err)

	_, err = svc.Run(context.Background(), validRunRequest(t))
	require.NoError(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "backtest run completed", records[0].Message)
	assert.Equal(t, slog.LevelInfo, records[0].Level)
	assert.Equal(t, "backtest", records[0].Attrs[logging.Component])
	assert.Equal(t, "noop", records[0].Attrs["strategy_name"])
	assert.NotContains(t, records[0].Attrs, "error")
}

func TestService_Run_LogsFactoryErrorAtErrorLevel(t *testing.T) {
	logger, rec := logging.Capture()
	factoryErr := errors.New("boom: no capacity for a fresh broker")
	svc, err := svcbacktest.New(newFixtureManager(t), newFixtureResolver(t), &fakeEnvironmentFactory{err: factoryErr}, logger)
	require.NoError(t, err)

	_, err = svc.Run(context.Background(), validRunRequest(t))
	require.Error(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "backtest run failed", records[0].Message)
	assert.Equal(t, slog.LevelError, records[0].Level)
	assert.Equal(t, factoryErr, records[0].Attrs["error"])
}
