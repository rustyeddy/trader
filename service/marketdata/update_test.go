package marketdata_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/marketdata"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

func TestUpdate_FullPipelineFromMissingRaw(t *testing.T) {
	t.Parallel()
	body := oandaCandlesJSON([]time.Time{
		time.Date(2024, time.January, 2, 22, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 2, 23, 0, 0, 0, time.UTC),
	})
	server := newFakeOANDAServer(t, body)
	_, s := newTestManagerAndServiceWithOANDA(t, server.URL, t.TempDir())
	ctx := context.Background()

	resp, err := s.Update(ctx, svc.UpdateRequest{DatasetRequest: datasetRequest(t, marketdata.H1, januarySpan(t))})
	require.NoError(t, err)

	require.NotEmpty(t, resp.InitialPlan.Actions, "raw was entirely missing")
	require.Equal(t, marketdata.ActionDownloadRaw, resp.InitialPlan.Actions[0].Kind)

	require.True(t, resp.SyncPerformed)
	require.Len(t, resp.Sync.Result.Downloaded, 1)
	require.Equal(t, 2, resp.Sync.Result.Downloaded[0].RecordsWritten)

	require.Len(t, resp.Build.Result.Published, 1,
		"Build must recompute its own Plan after Sync -- InitialPlan alone never contained a normalize action")
	require.Equal(t, 2, resp.Build.Result.Published[0].BarCount)

	// Actually published: a direct Bars read confirms it.
	barsResp, err := s.Bars(ctx, svc.BarsRequest{DatasetRequest: datasetRequest(t, marketdata.H1, januarySpan(t))})
	require.NoError(t, err)
	require.Len(t, barsResp.Bars, 2)
}

func TestUpdate_SkipsSyncWhenRawAlreadyPresent(t *testing.T) {
	t.Parallel()
	// No OANDA credential configured at all: proves Update does not
	// require one when Sync is not actually needed.
	_, s := newTestManagerAndService(t)
	ctx := context.Background()
	span := fixtureSpan(t)

	resp, err := s.Update(ctx, svc.UpdateRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)

	require.NotEmpty(t, resp.InitialPlan.Actions, "raw is present in the fixture but canonical is not built yet")
	require.Equal(t, marketdata.ActionNormalizeCanonical, resp.InitialPlan.Actions[0].Kind)
	require.False(t, resp.SyncPerformed, "no raw-related Action; Sync must never be invoked")
	require.Equal(t, svc.SyncResponse{}, resp.Sync)

	require.Len(t, resp.Build.Result.Published, 1)
	require.Equal(t, 216, resp.Build.Result.Published[0].BarCount)
}

func TestUpdate_NothingToDoWhenAlreadyCurrent(t *testing.T) {
	t.Parallel()
	_, s := newTestManagerAndService(t)
	ctx := context.Background()
	span := fixtureSpan(t)

	first, err := s.Update(ctx, svc.UpdateRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)
	require.Len(t, first.Build.Result.Published, 1)

	second, err := s.Update(ctx, svc.UpdateRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)
	require.Empty(t, second.InitialPlan.Actions, "the freshly recomputed Plan finds nothing left to do")
	require.False(t, second.SyncPerformed)
	require.Empty(t, second.Build.Result.Published, "re-running an already-current Update performs no writes")
	require.Empty(t, second.Build.Result.Skipped)
}

func TestUpdate_InvalidRequestNeverReachesManager(t *testing.T) {
	t.Parallel()
	_, s := newTestManagerAndService(t)
	ctx := context.Background()

	_, err := s.Update(ctx, svc.UpdateRequest{})
	require.ErrorIs(t, err, svc.ErrInvalidRequest)
}

func TestUpdate_PropagatesContextCancellation(t *testing.T) {
	t.Parallel()
	_, s := newTestManagerAndService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Update(ctx, svc.UpdateRequest{DatasetRequest: datasetRequest(t, marketdata.H1, fixtureSpan(t))})
	require.ErrorIs(t, err, context.Canceled)
}

func TestUpdate_StopsBeforeBuildWhenSyncFails(t *testing.T) {
	t.Parallel()
	server := newJanuarySucceedsFebruaryFailsServer(t)
	_, s := newTestManagerAndServiceWithOANDA(t, server.URL, t.TempDir())
	ctx := context.Background()

	resp, err := s.Update(ctx, svc.UpdateRequest{DatasetRequest: datasetRequest(t, marketdata.H1, twoMonthSpan(t))})
	require.Error(t, err)

	require.True(t, resp.SyncPerformed)
	require.Len(t, resp.Sync.Result.Downloaded, 1, "January's partial progress survives Sync's own failure")
	require.Equal(t, svc.BuildResponse{}, resp.Build, "Build must never be attempted after a failed Sync")
}
