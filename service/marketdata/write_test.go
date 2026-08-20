package marketdata_test

import (
	"context"
	"fmt"
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
func newTestManagerAndServiceWithOANDA(t *testing.T, baseURL string) (*marketdata.Manager, *svc.Service) {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdListing(t)))

	mgr, err := marketdata.New(marketdata.Config{
		Clock:           clock.NewSimulated(time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:       t.TempDir(),
		RawRoot:         t.TempDir(),
		Resolver:        resolver,
		ProviderName:    "oanda",
		OANDACredential: staticCredential("test-token"),
		OANDABaseURL:    baseURL,
	})
	require.NoError(t, err)

	s, err := svc.New(mgr)
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
	_, s := newTestManagerAndServiceWithOANDA(t, server.URL)
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
	_, s := newTestManagerAndServiceWithOANDA(t, server.URL)
	ctx := context.Background()

	_, err := s.Sync(ctx, svc.SyncRequest{})
	require.ErrorIs(t, err, svc.ErrInvalidRequest)
}

func TestSync_PropagatesContextCancellation(t *testing.T) {
	t.Parallel()
	server := newFakeOANDAServer(t, oandaCandlesJSON(nil))
	_, s := newTestManagerAndServiceWithOANDA(t, server.URL)
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

func TestSync_ReturnsPartialResultOnFetchFailure(t *testing.T) {
	t.Parallel()
	// A malformed body makes *oanda.Client's decode fail for every
	// request, so Sync's single ActionDownloadRaw entry fails --
	// exercising the "partial result alongside a non-nil error" branch
	// SyncResponse's own doc comment describes. With only one Action in
	// this Plan, "partial" here means Downloaded stays empty rather than
	// containing a later action's success after an earlier failure; see
	// SyncResponse's own doc comment for why Result is still populated
	// (not zeroed) alongside the error regardless.
	server := newFakeOANDAServer(t, `not valid json`)
	_, s := newTestManagerAndServiceWithOANDA(t, server.URL)
	ctx := context.Background()

	resp, err := s.Sync(ctx, svc.SyncRequest{DatasetRequest: datasetRequest(t, marketdata.H1, januarySpan(t))})
	require.Error(t, err)
	require.NotEmpty(t, resp.Plan.Actions, "the Plan itself was computed successfully before Sync's failure")
	require.Empty(t, resp.Result.Downloaded)
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
