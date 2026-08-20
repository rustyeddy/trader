package marketdata_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

// staticCredential is a minimal oanda.CredentialProvider implementation
// this package can construct without importing marketdata/internal —
// Config.OANDACredential is typed as an interface (Token(ctx)
// (string, error)), so any type with a matching method satisfies it by
// structural typing; no internal import is required or possible.
type staticCredential string

func (s staticCredential) Token(context.Context) (string, error) {
	return string(s), nil
}

// oandaCandlesJSON builds a minimal OANDA-shaped candles response body
// for times, all marked complete, matching the wire shape
// *oanda.Client actually decodes.
func oandaCandlesJSON(times []time.Time) string {
	var b strings.Builder
	b.WriteString(`{"candles":[`)
	for i, t := range times {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"complete":true,"time":%q,"volume":100,`+
			`"bid":{"o":"1.10000","h":"1.10100","l":"1.09900","c":"1.10050"},`+
			`"ask":{"o":"1.10010","h":"1.10110","l":"1.09910","c":"1.10060"}}`,
			t.Format(time.RFC3339Nano))
	}
	b.WriteString(`]}`)
	return b.String()
}

// newFakeOANDAServer returns an httptest.Server that answers every
// candles request with body, closed automatically at test cleanup. This
// exercises Service.Sync's real network path end to end (a real
// *oanda.Client making a real HTTP request over loopback), the only way
// available to a package outside marketdata's own subtree: Manager
// builds its OANDA client from OANDABaseURL/OANDACredential internally
// and does not accept an injectable HTTP transport through its public
// Config, unlike marketdata's own in-package Sync tests.
func newFakeOANDAServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// newTestManagerAndServiceWithOANDA is newTestManagerAndService's
// counterpart for Sync tests: it additionally configures Manager's
// OANDA client against baseURL, so Service.Sync can actually download.
// rawRoot lets a caller start from either an empty directory (raw
// entirely missing) or a copy of this package's committed fixture (raw
// already present) — Manager.Sync requires an OANDA client configured
// regardless of whether the Plan it is given actually contains any
// ActionDownloadRaw entries, so even a raw-already-present,
// nothing-to-download scenario needs one.
func newTestManagerAndServiceWithOANDA(t *testing.T, baseURL, rawRoot string) (*marketdata.Manager, *svc.Service) {
	t.Helper()
	return newTestManagerAndServiceWithOANDAAndLogger(t, baseURL, rawRoot, nil)
}

// newTestManagerAndServiceWithOANDAAndLogger is
// newTestManagerAndServiceWithOANDA's counterpart for logging tests
// (issue #128): identical setup, but passed straight through to
// svc.New so a test can inspect what Sync/Build/Update actually log
// via logging.Capture.
func newTestManagerAndServiceWithOANDAAndLogger(t *testing.T, baseURL, rawRoot string, logger *slog.Logger) (*marketdata.Manager, *svc.Service) {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdListing(t)))

	mgr, err := marketdata.New(marketdata.Config{
		Clock:           clock.NewSimulated(time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:       t.TempDir(),
		RawRoot:         rawRoot,
		Resolver:        resolver,
		ProviderName:    "oanda",
		OANDACredential: staticCredential("test-token"),
		OANDABaseURL:    baseURL,
	})
	require.NoError(t, err)

	s, err := svc.New(mgr, logger)
	require.NoError(t, err)
	return mgr, s
}

// januarySpan is one full month, matching the fake OANDA server's
// fixture candles below.
func januarySpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

func TestSync_DownloadsMissingRawPartition(t *testing.T) {
	t.Parallel()
	body := oandaCandlesJSON([]time.Time{
		time.Date(2024, time.January, 2, 22, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 2, 23, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC),
	})
	server := newFakeOANDAServer(t, body)
	_, s := newTestManagerAndServiceWithOANDA(t, server.URL, t.TempDir())
	ctx := context.Background()

	resp, err := s.Sync(ctx, svc.SyncRequest{DatasetRequest: datasetRequest(t, marketdata.H1, januarySpan(t))})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Plan.Actions, "raw H1 is entirely missing")
	require.Equal(t, marketdata.ActionDownloadRaw, resp.Plan.Actions[0].Kind)
	require.Len(t, resp.Result.Downloaded, 1)
	require.Equal(t, 3, resp.Result.Downloaded[0].RecordsWritten)
	require.Empty(t, resp.Result.Skipped, "raw was missing, so normalize-canonical is gated and never scheduled")
}

func TestSync_InvalidRequestNeverReachesManager(t *testing.T) {
	t.Parallel()
	server := newFakeOANDAServer(t, oandaCandlesJSON(nil))
	_, s := newTestManagerAndServiceWithOANDA(t, server.URL, t.TempDir())
	ctx := context.Background()

	_, err := s.Sync(ctx, svc.SyncRequest{})
	require.ErrorIs(t, err, svc.ErrInvalidRequest)
}

func TestSync_PropagatesContextCancellation(t *testing.T) {
	t.Parallel()
	server := newFakeOANDAServer(t, oandaCandlesJSON(nil))
	_, s := newTestManagerAndServiceWithOANDA(t, server.URL, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Sync(ctx, svc.SyncRequest{DatasetRequest: datasetRequest(t, marketdata.H1, januarySpan(t))})
	require.ErrorIs(t, err, context.Canceled)
}

func TestSync_MissingOANDACredentialReturnsClearError(t *testing.T) {
	t.Parallel()
	// newTestService (read_test.go) configures no OANDA credential at all.
	s := newTestService(t)
	ctx := context.Background()

	resp, err := s.Sync(ctx, svc.SyncRequest{DatasetRequest: datasetRequest(t, marketdata.H1, januarySpan(t))})
	require.Error(t, err)
	require.ErrorIs(t, err, marketdata.ErrInvalidConfig)
	require.Empty(t, resp.Result.Downloaded, "no partial progress possible before Manager.Sync's own configuration check")
}

// twoMonthSpan covers January and February 2024, so a Plan against
// entirely-missing raw schedules two separate ActionDownloadRaw entries
// (one per month) rather than one — what
// TestSync_ReturnsPartialResultOnFetchFailure needs to prove a later
// failure doesn't discard an earlier success.
func twoMonthSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

// newJanuarySucceedsFebruaryFailsServer answers a January candles
// request with valid data and a February one with a malformed body,
// distinguishing them by the "from" query parameter *oanda.Client sets
// to each month's start — the two ActionDownloadRaw entries
// twoMonthSpan produces fetch from those exact starts, in chronological
// (January-then-February) order, matching Sync's own per-Action loop.
func newJanuarySucceedsFebruaryFailsServer(t *testing.T) *httptest.Server {
	t.Helper()
	validBody := oandaCandlesJSON([]time.Time{time.Date(2024, time.January, 2, 22, 0, 0, 0, time.UTC)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Query().Get("from"), "2024-02") {
			_, _ = w.Write([]byte("not valid json"))
			return
		}
		_, _ = w.Write([]byte(validBody))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestSync_ReturnsPartialResultOnFetchFailure(t *testing.T) {
	t.Parallel()
	// Proves the actual claim SyncResponse's doc comment makes: a
	// successful earlier Action's result survives in Result.Downloaded
	// alongside the error from a later Action's failure, rather than
	// being discarded. A one-Action Plan (this test's previous version)
	// could not distinguish that from Result simply being empty in every
	// error case.
	server := newJanuarySucceedsFebruaryFailsServer(t)
	_, s := newTestManagerAndServiceWithOANDA(t, server.URL, t.TempDir())
	ctx := context.Background()

	resp, err := s.Sync(ctx, svc.SyncRequest{DatasetRequest: datasetRequest(t, marketdata.H1, twoMonthSpan(t))})
	require.Error(t, err)
	require.Len(t, resp.Plan.Actions, 2, "one ActionDownloadRaw per missing month")
	require.Len(t, resp.Result.Downloaded, 1, "January succeeded before February's failure stopped the loop")
	require.Equal(t, time.January, resp.Result.Downloaded[0].Action.Month)
	require.Equal(t, 1, resp.Result.Downloaded[0].RecordsWritten)
}

func TestSync_SkipsNonDownloadActions(t *testing.T) {
	t.Parallel()
	// Raw already present (this package's committed fixture) but
	// nothing canonical yet: Plan schedules ActionNormalizeCanonical,
	// not ActionDownloadRaw, since gated scheduling only produces a raw
	// action when raw is actually missing (marketdata's own
	// deriveActionsRawBuilt doc comment). Sync must report that single
	// non-download Action in Result.Skipped rather than silently
	// dropping it or, worse, attempting to execute it — proving
	// SyncResponse's "non-download Actions appear in Skipped" claim
	// against a case that actually produces one, not just against an
	// empty Skipped list.
	server := newFakeOANDAServer(t, oandaCandlesJSON(nil)) // never actually called
	_, s := newTestManagerAndServiceWithOANDA(t, server.URL, copyFixtureRaw(t))
	ctx := context.Background()

	resp, err := s.Sync(ctx, svc.SyncRequest{DatasetRequest: datasetRequest(t, marketdata.H1, fixtureSpan(t))})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Plan.Actions)
	require.Equal(t, marketdata.ActionNormalizeCanonical, resp.Plan.Actions[0].Kind)
	require.Empty(t, resp.Result.Downloaded)
	require.Len(t, resp.Result.Skipped, 1)
	require.Equal(t, marketdata.ActionNormalizeCanonical, resp.Result.Skipped[0].Action.Kind)
}

func TestBuild_PublishesFromExistingRawData(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	ctx := context.Background()
	span := fixtureSpan(t)

	resp, err := s.Build(ctx, svc.BuildRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Plan.Actions, "raw H1 is present in this package's fixture but nothing is canonical yet")
	require.Equal(t, marketdata.ActionNormalizeCanonical, resp.Plan.Actions[0].Kind)
	require.Len(t, resp.Result.Published, 1)
	require.Equal(t, 216, resp.Result.Published[0].BarCount, "9 open days x 24 H1 bars, per this package's own fixture")

	// Build must have actually published: a direct Bars query against
	// the same dataset now finds the data.
	barsResp, err := s.Bars(ctx, svc.BarsRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)
	require.Len(t, barsResp.Bars, 216)
}

func TestBuild_InvalidRequestNeverReachesManager(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	ctx := context.Background()

	_, err := s.Build(ctx, svc.BuildRequest{})
	require.ErrorIs(t, err, svc.ErrInvalidRequest)
}

func TestBuild_PropagatesContextCancellation(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Build(ctx, svc.BuildRequest{DatasetRequest: datasetRequest(t, marketdata.D1, fixtureSpan(t))})
	require.ErrorIs(t, err, context.Canceled)
}

func TestBuild_NothingToDoWhenAlreadyCurrent(t *testing.T) {
	t.Parallel()
	_, s := newTestManagerAndService(t)
	ctx := context.Background()
	span := fixtureSpan(t)

	first, err := s.Build(ctx, svc.BuildRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)
	require.Len(t, first.Result.Published, 1)

	second, err := s.Build(ctx, svc.BuildRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)
	require.Empty(t, second.Plan.Actions, "the freshly recomputed Plan finds nothing left to do")
	require.Empty(t, second.Result.Published)
}
