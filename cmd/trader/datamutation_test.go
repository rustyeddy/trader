package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// oandaCandlesJSON builds a minimal OANDA-shaped candles response body
// for times, all marked complete, matching the wire shape
// *oanda.Client actually decodes -- the same fixture shape
// service/marketdata's own Sync tests use.
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

// newFakeOANDAServer returns an httptest.Server answering every
// candles request with body, closed automatically at test cleanup.
// This exercises Sync's real network path end to end (a real
// *oanda.Client making a real HTTP request over loopback), since
// Manager builds its OANDA client internally from --oanda-base-url/
// --oanda-token and accepts no injectable transport.
func newFakeOANDAServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// runDataWithOANDA is runData's counterpart for Sync/Update tests that
// need a configured OANDA client.
func runDataWithOANDA(t *testing.T, storeRoot, rawRoot, baseURL string, args ...string) (string, error) {
	t.Helper()
	root, cleanup := newRootCmd()
	defer func() { _ = cleanup() }()

	full := append([]string{"data"}, args...)
	full = append(full,
		"--store-root", storeRoot,
		"--raw-root", rawRoot,
		"--oanda-token", "test-token",
		"--oanda-base-url", baseURL,
	)
	root.SetArgs(full)

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestDataBuild_PublishesFromExistingRaw(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	out, err := runData(t, storeRoot, rawRoot,
		"build", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08")
	require.NoError(t, err)
	require.Contains(t, out, "published")

	barsOut, err := runData(t, storeRoot, rawRoot,
		"bars", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08")
	require.NoError(t, err)
	require.NotEmpty(t, barsOut, "the published data must actually be readable back")
}

func TestDataBuild_NoOpWhenAlreadyCurrent(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"build", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08")
	require.NoError(t, err)

	out, err := runData(t, storeRoot, rawRoot,
		"build", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08")
	require.NoError(t, err)
	require.NotContains(t, out, "published", "re-running build against an already-current dataset must publish nothing")
}

func TestDataBuild_InvalidIntervalIsRejected(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"build", "EURUSD", "H99", "--from", "2024-01-07", "--to", "2024-01-08")
	require.Error(t, err)
}

func TestDataBuild_PropagatesCancelledContext(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	root, cleanup := newRootCmd()
	defer func() { _ = cleanup() }()
	root.SetArgs([]string{"data", "build", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08",
		"--store-root", storeRoot, "--raw-root", rawRoot})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := root.ExecuteContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestDataSync_RequiresOANDACredentials(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"sync", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08")
	require.Error(t, err, "no --oanda-token/--oanda-base-url configured at all")
}

func TestDataSync_RejectsTokenWithoutBaseURL(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	root, cleanup := newRootCmd()
	defer func() { _ = cleanup() }()
	root.SetArgs([]string{"data", "sync", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08",
		"--store-root", storeRoot, "--raw-root", rawRoot, "--oanda-token", "abc"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	require.Error(t, err, "a token without a base URL must not silently proceed as if unconfigured")
}

func TestDataSync_DownloadsMissingRawViaRealNetworkRoundTrip(t *testing.T) {
	body := oandaCandlesJSON([]time.Time{
		time.Date(2024, time.January, 2, 22, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 2, 23, 0, 0, 0, time.UTC),
	})
	server := newFakeOANDAServer(t, body)

	storeRoot := t.TempDir()
	rawRoot := t.TempDir() + "/oanda"
	require.NoError(t, os.MkdirAll(rawRoot, 0o755))

	out, err := runDataWithOANDA(t, storeRoot, rawRoot, server.URL,
		"sync", "EURUSD", "H1", "--from", "2024-01-01", "--to", "2024-02-01")
	require.NoError(t, err)
	require.Contains(t, out, "downloaded")
}

func TestDataUpdate_SkipsSyncWhenRawAlreadyPresent(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	// No OANDA credentials configured at all: proves update does not
	// require them when raw is already present and only a canonical
	// build is needed.
	out, err := runData(t, storeRoot, rawRoot,
		"update", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08")
	require.NoError(t, err)
	require.Contains(t, out, "published")
}

func TestDataUpdate_FullPipelineFromMissingRaw(t *testing.T) {
	body := oandaCandlesJSON([]time.Time{
		time.Date(2024, time.January, 2, 22, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 2, 23, 0, 0, 0, time.UTC),
	})
	server := newFakeOANDAServer(t, body)

	storeRoot := t.TempDir()
	rawRoot := t.TempDir() + "/oanda"
	require.NoError(t, os.MkdirAll(rawRoot, 0o755))
	span := []string{"--from", "2024-01-02T22:00:00Z", "--to", "2024-01-03T00:00:00Z"}

	args := append([]string{"update", "EURUSD", "H1"}, span...)
	out, err := runDataWithOANDA(t, storeRoot, rawRoot, server.URL, args...)
	require.NoError(t, err)
	require.Contains(t, out, "downloaded")
	require.Contains(t, out, "published")

	barsOut, err := runData(t, storeRoot, rawRoot, append([]string{"bars", "EURUSD", "H1"}, span...)...)
	require.NoError(t, err)
	require.NotEmpty(t, barsOut)
}

func TestDataUpdate_InvalidRequestNeverReachesManager(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"update", "EURUSD", "H99", "--from", "2024-01-07", "--to", "2024-01-08")
	require.Error(t, err)
}

func TestDataUpdate_PropagatesCancelledContext(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	root, cleanup := newRootCmd()
	defer func() { _ = cleanup() }()
	root.SetArgs([]string{"data", "update", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08",
		"--store-root", storeRoot, "--raw-root", rawRoot})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := root.ExecuteContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
