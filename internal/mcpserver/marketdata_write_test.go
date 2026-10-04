package mcpserver

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svcmarketdata "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
)

const stooqHeader = "<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\n"

const (
	spyJanuary = stooqHeader + "SPY.US,D,20200131,000000,100,101,99,100.5,1000,0\n"
	spyTwo     = spyJanuary + "SPY.US,D,20200203,000000,100.5,102,100,101.5,2000,0\n"
)

func writeArchive(t *testing.T, path, member, content string) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, err := zw.Create(member)
	require.NoError(t, err)
	_, err = w.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
}

func TestWriteTools_RefusedWithoutWriteAccess(t *testing.T) {
	oanda := newFixtureService(t, "oanda")
	session := connect(t, Deps{MarketData: serviceFactory{"oanda": oanda.Service}}) // AllowWrites false
	raw, store := fingerprint(t, oanda.rawRoot), fingerprint(t, oanda.storeRoot)

	for _, tool := range []string{"trader_marketdata_canonicalize", "trader_marketdata_update"} {
		result := call(t, session, tool, map[string]any{"symbols": []string{"EURUSD"}, "interval": "H1"}, nil)
		require.True(t, result.IsError, tool)
		assert.Contains(t, errorText(result), "--allow-writes", tool)
	}
	assert.Equal(t, raw, fingerprint(t, oanda.rawRoot))
	assert.Equal(t, store, fingerprint(t, oanda.storeRoot), "nothing was built")
}

func TestCanonicalizeTool_RawProvider(t *testing.T) {
	oanda := newFixtureService(t, "oanda")
	session := connect(t, Deps{MarketData: serviceFactory{"oanda": oanda.Service}, AllowWrites: true})

	var out DatasetsOutput
	result := call(t, session, "trader_marketdata_canonicalize", map[string]any{"symbols": []string{"EURUSD", "BAD"}, "interval": "H1"}, &out)
	require.False(t, result.IsError, "a failing symbol is not a call error: %s", errorText(result))
	assert.Equal(t, "canonicalize", out.Operation)
	assert.False(t, out.OK)
	assert.Equal(t, DatasetsSummary{Built: 1, Failed: 1}, out.Summary)

	eur := out.Results[0]
	assert.Equal(t, "built", eur.Status)
	assert.Equal(t, "raw", eur.Source)
	require.NotNil(t, eur.Range)
	assert.Equal(t, TimeSpan{Start: "2024-01-07T22:00:00Z", End: "2024-02-05T01:00:00Z"}, *eur.Range, "the whole raw span")
	assert.Equal(t, 2, eur.PublishedPartitions)
	assert.Nil(t, eur.CanonicalBefore)
	require.NotNil(t, eur.CanonicalAfter)

	assert.Equal(t, "failed", out.Results[1].Status)
	assert.Contains(t, out.Results[1].Error, "6-letter FX pair")

	var again DatasetsOutput
	call(t, session, "trader_marketdata_canonicalize", map[string]any{
		"symbols": []string{"EURUSD"}, "interval": "H1", "from": "2024-01-07T22:00:00Z", "to": "2024-01-19T22:00:00Z",
	}, &again)
	assert.True(t, again.OK)
	assert.Equal(t, DatasetsSummary{Current: 1}, again.Summary, "a complete month already built is current")
}

func TestCanonicalizeTool_StooqArchive(t *testing.T) {
	archiveRoot := t.TempDir()
	archive := filepath.Join(archiveRoot, "spy_us_d.zip")
	writeArchive(t, archive, "spy.us.txt", spyTwo)
	before, err := os.ReadFile(archive)
	require.NoError(t, err)
	stooq := newFixtureService(t, "stooq", svcmarketdata.WithArchiveRoot(archiveRoot))
	session := connect(t, Deps{MarketData: serviceFactory{"stooq": stooq.Service}, AllowWrites: true})

	var out DatasetsOutput
	call(t, session, "trader_marketdata_canonicalize", map[string]any{"symbols": []string{"SPY", "QQQ"}, "provider": "stooq", "interval": "D1"}, &out)
	spy := out.Results[0]
	assert.Equal(t, "built", spy.Status)
	assert.Equal(t, "archive", spy.Source, "nothing fetched from the network")
	assert.Equal(t, &ArchiveOutput{FirstDate: "2020-01-31", LastDate: "2020-02-03", Rows: 2}, spy.Archive)
	assert.Equal(t, 2, spy.PublishedBars)

	qqq := out.Results[1]
	assert.Equal(t, "failed", qqq.Status)
	assert.Equal(t, "no archive for this symbol under the server's archive root", qqq.Error)
	assert.NotContains(t, qqq.Error, archiveRoot, "no path reaches the client")

	after, err := os.ReadFile(archive)
	require.NoError(t, err)
	assert.Equal(t, sha256.Sum256(before), sha256.Sum256(after), "the source archive is preserved")
}

func TestUpdateTool_Stooq(t *testing.T) {
	archiveRoot := t.TempDir()
	archive := filepath.Join(archiveRoot, "spy_us_d.zip")
	writeArchive(t, archive, "spy.us.txt", spyJanuary)
	stooq := newFixtureService(t, "stooq", svcmarketdata.WithArchiveRoot(archiveRoot))
	session := connect(t, Deps{MarketData: serviceFactory{"stooq": stooq.Service}, AllowWrites: true})
	args := map[string]any{"symbols": []string{"SPY"}, "provider": "stooq", "interval": "D1"}

	var fresh DatasetsOutput
	call(t, session, "trader_marketdata_update", args, &fresh)
	assert.Equal(t, "failed", fresh.Results[0].Status)
	assert.Contains(t, fresh.Results[0].Error, "canonicalize first")

	call(t, session, "trader_marketdata_canonicalize", args, nil)
	var current DatasetsOutput
	call(t, session, "trader_marketdata_update", args, &current)
	assert.Equal(t, DatasetsSummary{Current: 1}, current.Summary, "the archive holds nothing newer")

	writeArchive(t, archive, "spy.us.txt", spyTwo)
	var updated DatasetsOutput
	call(t, session, "trader_marketdata_update", args, &updated)
	res := updated.Results[0]
	assert.Equal(t, "updated", res.Status)
	assert.Equal(t, "archive", res.Source)
	require.NotNil(t, res.CanonicalBefore)
	assert.Equal(t, "2020-01-31T00:00:00Z", res.CanonicalBefore.Last)
	require.NotNil(t, res.CanonicalAfter)
	assert.Equal(t, "2020-02-03T00:00:00Z", res.CanonicalAfter.Last)
}

func TestUpdateTool_RawProvider(t *testing.T) {
	oanda := newFixtureService(t, "oanda")
	session := connect(t, Deps{MarketData: serviceFactory{"oanda": oanda.Service}, AllowWrites: true})

	var ranged DatasetsOutput
	call(t, session, "trader_marketdata_update", map[string]any{
		"symbols": []string{"EURUSD"}, "interval": "H1", "from": "2024-01-07T22:00:00Z", "to": "2024-01-19T22:00:00Z",
	}, &ranged)
	res := ranged.Results[0]
	assert.Equal(t, "updated", res.Status, res.Error)
	assert.Equal(t, "provider", res.Source)
	assert.Zero(t, res.DownloadedPartitions, "raw already covers the range")

	// Through now needs a download the server has no credentials for:
	// that symbol fails, the call does not, and no detail leaks.
	var through DatasetsOutput
	result := call(t, session, "trader_marketdata_update", map[string]any{"symbols": []string{"EURUSD"}, "interval": "H1"}, &through)
	require.False(t, result.IsError)
	assert.False(t, through.OK)
	assert.Equal(t, "failed", through.Results[0].Status)
	assert.Equal(t, "update failed; see the trader-mcp server log", through.Results[0].Error)
}

func TestWriteTools_InvalidRequest(t *testing.T) {
	oanda := newFixtureService(t, "oanda")
	session := connect(t, Deps{MarketData: serviceFactory{"oanda": oanda.Service}, AllowWrites: true})
	for _, tool := range []string{"trader_marketdata_canonicalize", "trader_marketdata_update"} {
		for name, args := range map[string]map[string]any{
			"bad interval":     {"symbols": []string{"EURUSD"}, "interval": "H99"},
			"one range end":    {"symbols": []string{"EURUSD"}, "interval": "H1", "to": "2024-01-01"},
			"unknown provider": {"symbols": []string{"EURUSD"}, "interval": "H1", "provider": "bloomberg"},
			"no symbols":       {"symbols": []string{}, "interval": "H1"},
		} {
			result := call(t, session, tool, args, nil)
			assert.True(t, result.IsError, "%s %s", tool, name)
		}
	}
}

// TestCanonicalizeTool_SameDataAsServicePath: canonical data built through
// MCP is byte-for-byte what the service operation (and so the CLI) builds
// from the same source, under the same clock — no parallel implementation.
func TestCanonicalizeTool_SameDataAsServicePath(t *testing.T) {
	viaMCP := newFixtureService(t, "oanda")
	direct := newFixtureService(t, "oanda")
	session := connect(t, Deps{MarketData: serviceFactory{"oanda": viaMCP.Service}, AllowWrites: true})

	call(t, session, "trader_marketdata_canonicalize", map[string]any{"symbols": []string{"EURUSD"}, "interval": "H1"}, nil)
	_, err := direct.CanonicalizeDatasets(context.Background(), svcmarketdata.DatasetsRequest{
		Instruments: []svcmarketdata.InstrumentRequest{{Symbol: "EURUSD"}}, Interval: marketdata.H1,
	}, svcmarketdata.CanonicalizeOptions{})
	require.NoError(t, err)

	got := fingerprint(t, viaMCP.storeRoot)
	require.NotEmpty(t, got)
	assert.Equal(t, fingerprint(t, direct.storeRoot), got)
}

func TestWriteTools_ProgressPerSymbol(t *testing.T) {
	ctx := context.Background()
	oanda := newFixtureService(t, "oanda")
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(Deps{MarketData: serviceFactory{"oanda": oanda.Service}, AllowWrites: true}).Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()

	var mu sync.Mutex
	var messages []string
	var totals []float64
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			mu.Lock()
			defer mu.Unlock()
			messages = append(messages, req.Params.Message)
			totals = append(totals, req.Params.Total)
		},
	})
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	params := &mcp.CallToolParams{Name: "trader_marketdata_canonicalize", Arguments: map[string]any{
		"symbols": []string{"EURUSD", "BAD"}, "interval": "H1", "from": "2024-01-07T22:00:00Z", "to": "2024-01-19T22:00:00Z",
	}}
	params.SetProgressToken("tok-1")
	_, err = session.CallTool(ctx, params)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(messages) == 2
	}, 2*time.Second, 10*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"EURUSD: built", "BAD: failed"}, messages)
	assert.Equal(t, []float64{2, 2}, totals)
}
