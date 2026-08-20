package marketdata_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/marketdata"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

// TestNew_NilLoggerDefaultsToDiscard proves New(manager, nil) does not
// panic on the first log call a Service operation makes -- the "inject
// a logger, or nothing at all" convention (logging/doc.go) only holds
// if a nil logger is actually usable, not merely accepted at
// construction time.
func TestNew_NilLoggerDefaultsToDiscard(t *testing.T) {
	_, s := newTestManagerAndServiceWithLogger(t, nil)

	require.NotPanics(t, func() {
		_, _ = s.Plan(context.Background(), svc.PlanRequest{DatasetRequest: datasetRequest(t, marketdata.D1, fixtureSpan(t))})
	})
}

// TestBars_LogsCompletionWithCanonicalAttributes is issue #128's own
// demonstration that a MarketData service operation's log record uses
// canonical component/domain attributes -- instrument_id, the
// logging.ComponentMarketData scope every Service record carries --
// rather than embedding the same information only inside the message
// string.
func TestBars_LogsCompletionWithCanonicalAttributes(t *testing.T) {
	logger, rec := logging.Capture()
	mgr, s := newTestManagerAndServiceWithLogger(t, logger)
	id := eurusdID(t)
	span := fixtureSpan(t)
	ctx := context.Background()

	planResp, err := s.Plan(ctx, svc.PlanRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)
	_, err = mgr.Build(ctx, planResp.Plan)
	require.NoError(t, err)
	rec.Reset()

	_, err = s.Bars(ctx, svc.BarsRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	rec0 := records[0]
	assert.Equal(t, "bars queried", rec0.Message)
	assert.Equal(t, slog.LevelDebug, rec0.Level)
	assert.Equal(t, "marketdata", rec0.Attrs[logging.Component])
	assert.Equal(t, id.String(), rec0.Attrs[logging.InstrumentID])
	assert.Equal(t, int64(216), rec0.Attrs["bar_count"])
	assert.NotContains(t, rec0.Attrs, "error", "a successful record must not carry a stray error attribute")
}

// TestBars_LogsFailureAtErrorLevel proves a failing read is logged
// once, at ERROR, with the actual error attached under the canonical
// "error" key -- not merely returned silently to the caller.
// Unbuilt canonical data (fixtureSpan with nothing ever published) is
// the same deterministic failure TestBars_MissingDataReturnsErrDataUnavailable
// already relies on.
func TestBars_LogsFailureAtErrorLevel(t *testing.T) {
	logger, rec := logging.Capture()
	_, s := newTestManagerAndServiceWithLogger(t, logger)

	_, err := s.Bars(context.Background(), svc.BarsRequest{DatasetRequest: datasetRequest(t, marketdata.D1, fixtureSpan(t))})
	require.ErrorIs(t, err, marketdata.ErrDataUnavailable)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "bars query failed", records[0].Message)
	assert.Equal(t, slog.LevelError, records[0].Level)
	assert.Equal(t, err, records[0].Attrs["error"],
		"the logged error attribute must be the exact error returned to the caller")
}

// TestPlan_LogsCorrelationIDFromContext proves context-propagated
// correlation metadata reaches Service's own records automatically
// (issue #128's own "correlation/causation propagates" criterion), the
// same mechanism logging.WithCorrelationID/ExampleWithCorrelationID
// already documents -- Service needs no correlation-specific code of
// its own, only to log through a *Context method on a
// NewContextHandler-wrapped logger, which logging.Capture already
// builds.
func TestPlan_LogsCorrelationIDFromContext(t *testing.T) {
	logger, rec := logging.Capture()
	_, s := newTestManagerAndServiceWithLogger(t, logger)

	ctx := logging.WithCorrelationID(context.Background(), "corr-plan-1")
	_, err := s.Plan(ctx, svc.PlanRequest{DatasetRequest: datasetRequest(t, marketdata.D1, fixtureSpan(t))})
	require.NoError(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "corr-plan-1", records[0].Attrs[logging.CorrelationID])
}

// TestUpdate_LogsSingleCompletionRecordWithPartitionCounts is the
// direct regression for ADR-023's own literal INFO example ("a market
// data update completed" with downloaded_partitions/
// published_partitions attributes): Update's own single rollup record
// carries those exact attribute names, distinct from (not a duplicate
// of) the inner Sync/Build calls' own "sync completed"/"build
// completed" records.
func TestUpdate_LogsSingleCompletionRecordWithPartitionCounts(t *testing.T) {
	body := oandaCandlesJSON([]time.Time{
		time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 8, 1, 0, 0, 0, time.UTC),
	})
	server := newFakeOANDAServer(t, body)
	logger, rec := logging.Capture()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 8, 2, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	_, s := newTestManagerAndServiceWithOANDAAndLogger(t, server.URL, t.TempDir(), logger)

	_, err = s.Update(context.Background(), svc.UpdateRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)

	var completions, syncCompletions, buildCompletions int
	for _, r := range rec.Records() {
		switch r.Message {
		case "update completed":
			completions++
			assert.Equal(t, true, r.Attrs["sync_performed"])
			assert.Contains(t, r.Attrs, "downloaded_partitions")
			assert.Contains(t, r.Attrs, "published_partitions")
		case "sync completed":
			syncCompletions++
		case "build completed":
			buildCompletions++
		}
	}
	assert.Equal(t, 1, completions, "Update must log its own outcome exactly once per call")
	assert.Equal(t, 1, syncCompletions, "Sync's own inner call still logs its own record")
	assert.Equal(t, 1, buildCompletions, "Build's own inner call still logs its own record")
}

// TestUpdate_LogsFailureOnceAtUpdateBoundary proves Update's own
// failure record fires exactly once even though the underlying Sync
// failure already logged its own, separate "sync failed" record --
// this is deliberate layered logging (Update is itself a directly
// invokable use case with its own callers), not the duplicate-logging
// pattern ADR-023 warns against (see Service.Update's own doc comment).
func TestUpdate_LogsFailureOnceAtUpdateBoundary(t *testing.T) {
	server := newFakeOANDAServer(t, "not valid json")
	logger, rec := logging.Capture()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 8, 2, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	_, s := newTestManagerAndServiceWithOANDAAndLogger(t, server.URL, t.TempDir(), logger)

	_, err = s.Update(context.Background(), svc.UpdateRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.Error(t, err)

	var updateFailures, syncFailures int
	for _, r := range rec.Records() {
		switch r.Message {
		case "update failed":
			updateFailures++
		case "sync failed":
			syncFailures++
		}
	}
	assert.Equal(t, 1, updateFailures)
	assert.Equal(t, 1, syncFailures)
}
