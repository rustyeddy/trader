package marketdata_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

// TestSync_LogsCorrelationAndCausationFromContext is the M2.6
// completion review's own (issue #129) representative check that
// correlation/causation propagation is not something only Plan (a
// read-only operation) happens to demonstrate: a mutating operation's
// own INFO-level completion record carries both context-propagated
// IDs identically, through the same generic *Context logger mechanism
// -- no Sync-specific code exists, or is needed, for this to hold.
func TestSync_LogsCorrelationAndCausationFromContext(t *testing.T) {
	server := newFakeOANDAServer(t, oandaCandlesJSON(nil)) // never actually called; raw already present
	logger, rec := logging.Capture()
	_, s := newTestManagerAndServiceWithOANDAAndLogger(t, server.URL, copyFixtureRaw(t), logger)

	ctx := logging.WithCorrelationID(context.Background(), "corr-sync-1")
	ctx = logging.WithCausationID(ctx, "cause-sync-1")
	_, err := s.Sync(ctx, svc.SyncRequest{DatasetRequest: datasetRequest(t, marketdata.H1, fixtureSpan(t))})
	require.NoError(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "sync completed", records[0].Message)
	assert.Equal(t, "corr-sync-1", records[0].Attrs[logging.CorrelationID])
	assert.Equal(t, "cause-sync-1", records[0].Attrs[logging.CausationID])
}

// TestSync_NeverLogsOANDACredential is the M2.6 completion review's own
// (issue #129) direct redaction-adjacent verification for MarketData
// specifically: OANDACredential is configured only on
// *marketdata.Manager (dataservice.go's own oandaTokenCredential,
// production-side) and never reaches Service or its logger at all, so
// a real Sync round trip against a server that requires this exact
// token must produce no record -- message or any attribute value --
// containing it. This is not testing logging.Secret/redactSensitiveKeys
// themselves (logging's own redact_test.go already does); it is
// confirming service/marketdata's own call sites never had a reason to
// need them in the first place.
func TestSync_NeverLogsOANDACredential(t *testing.T) {
	const token = "test-token" // matches staticCredential("test-token"), write_test.go
	server := newTokenGatedOANDAServer(t, token, oandaCandlesJSON([]time.Time{
		time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC),
	}))
	logger, rec := logging.Capture()
	_, s := newTestManagerAndServiceWithOANDAAndLogger(t, server.URL, t.TempDir(), logger)

	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 8, 1, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	_, err = s.Sync(context.Background(), svc.SyncRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)

	records := rec.Records()
	require.NotEmpty(t, records)
	for _, r := range records {
		assert.NotContains(t, r.Message, token)
		for k, v := range r.Attrs {
			assert.NotContains(t, fmt.Sprintf("%v", v), token, "attribute %q must not carry the OANDA credential", k)
		}
	}
}

// newTokenGatedOANDAServer is newFakeOANDAServer's counterpart for
// credential-verification tests: unlike newFakeOANDAServer (which
// returns body unconditionally, never inspecting the request at all),
// this rejects any request whose Authorization header is not exactly
// "Bearer "+token with 401 and no body. Review on #138 correctly found
// that a credential-non-leakage test proves nothing about the
// credential actually being required/used if the fake server it talks
// to never checks for it in the first place; this is what makes
// TestSync_NeverLogsOANDACredential's "real Sync round trip against a
// server that requires this exact token" claim true rather than
// aspirational -- a Sync call presenting the wrong (or no) credential
// against this server fails, which TestSync_NeverLogsOANDACredential's
// own require.NoError would catch.
func newTokenGatedOANDAServer(t *testing.T, token, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
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
