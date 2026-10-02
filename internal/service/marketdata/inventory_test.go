package marketdata_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/logging"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
)

func TestInventory_RawThenCanonicalAfterBuild(t *testing.T) {
	ctx := context.Background()
	mgr, s := newTestManagerAndService(t)
	req := svc.InventoryRequest{Instrument: eurusdID(t), Interval: marketdata.H1}

	resp, err := s.Inventory(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp.Inventory.Raw)
	assert.Equal(t, 2, resp.Inventory.Raw.Partitions)
	assert.True(t, resp.Inventory.Raw.First.Equal(time.Date(2024, time.January, 7, 22, 0, 0, 0, time.UTC)))
	assert.True(t, resp.Inventory.Raw.Last.Equal(time.Date(2024, time.February, 5, 0, 0, 0, 0, time.UTC)))
	assert.Nil(t, resp.Inventory.Canonical, "raw ahead of canonical: nothing built yet")

	plan, err := s.Plan(ctx, svc.PlanRequest{DatasetRequest: datasetRequest(t, marketdata.H1, fixtureSpan(t))})
	require.NoError(t, err)
	_, err = mgr.Build(ctx, plan.Plan)
	require.NoError(t, err)

	resp, err = s.Inventory(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp.Inventory.Canonical)
	assert.Equal(t, 1, resp.Inventory.Canonical.Partitions)
	assert.True(t, resp.Inventory.Canonical.First.Equal(fixtureSpan(t).Start()))
	assert.True(t, resp.Inventory.Canonical.Last.Before(fixtureSpan(t).End()))
	assert.True(t, resp.Inventory.Raw.Last.After(resp.Inventory.Canonical.Last), "raw still extends past canonical")
}

func TestInventory_InvalidRequest(t *testing.T) {
	s := newTestService(t)
	for name, req := range map[string]svc.InventoryRequest{
		"zero instrument":  {Interval: marketdata.H1},
		"invalid interval": {Instrument: eurusdID(t)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.Inventory(context.Background(), req)
			assert.ErrorIs(t, err, svc.ErrInvalidRequest)
		})
	}
}

func TestInventory_LogsFailureWithError(t *testing.T) {
	logger, rec := logging.Capture()
	_, s := newTestManagerAndServiceWithLogger(t, logger)

	_, err := s.Inventory(context.Background(), svc.InventoryRequest{Instrument: instrument.ETFID("ARCA", "SPY"), Interval: marketdata.H1})
	require.ErrorIs(t, err, instrument.ErrUnknownSymbol)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "inventory query failed", records[0].Message)
	assert.Equal(t, slog.LevelError, records[0].Level)
	assert.Contains(t, records[0].Attrs, "error")
}

func TestInventory_LogsSuccess(t *testing.T) {
	logger, rec := logging.Capture()
	_, s := newTestManagerAndServiceWithLogger(t, logger)

	_, err := s.Inventory(context.Background(), svc.InventoryRequest{Instrument: eurusdID(t), Interval: marketdata.H1})
	require.NoError(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "inventory queried", records[0].Message)
	assert.Equal(t, slog.LevelDebug, records[0].Level)
	assert.Equal(t, true, records[0].Attrs["has_raw"])
	assert.Equal(t, false, records[0].Attrs["has_canonical"])
	assert.NotContains(t, records[0].Attrs, "error")
}
