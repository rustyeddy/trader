package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncBuffer is a writer the server goroutine and the test can share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startServer runs `trader serve` in the background and returns its base URL and a
// stop function that interrupts it and returns what Execute returned.
func startServer(t *testing.T, env map[string]string, args ...string) (base string, stop func() error) {
	t.Helper()
	a := newApp()
	a.getenv = func(k string) string { return env[k] }
	out := &syncBuffer{}
	a.cmd.SetOut(out)
	a.cmd.SetErr(io.Discard)
	a.cmd.SetArgs(append([]string{"--log-output", filepath.Join(t.TempDir(), "serve.log"), "serve"}, args...))
	prev := slogDefault()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.cmd.ExecuteContext(ctx) }()

	re := regexp.MustCompile(`listening on (http://\S+)`)
	require.Eventually(t, func() bool { return re.MatchString(out.String()) }, 5*time.Second, 10*time.Millisecond, "the server announces its address")
	base = re.FindStringSubmatch(out.String())[1]
	stopped := false
	stop = func() error {
		if stopped {
			return nil
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			_ = a.cleanup()
			restoreSlog(prev)
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("the server did not stop")
			return nil
		}
	}
	t.Cleanup(func() { _ = stop() })
	return base, stop
}

func httpGet(t *testing.T, target string, header ...string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	require.NoError(t, err)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

func TestServe_ServesTheBarsOverHTTPFromTheCanonicalFilesAndStopsCleanly(t *testing.T) {
	root := storetest.Root(t)
	buildMonths(t, root, time.January) // EURUSD h1 2024-01-02 00:00 and 01:00
	base, stop := startServer(t, nil, "--config", writeConfig(t, ""), "--data-root", root, "--listen", "127.0.0.1:0")

	code, body := httpGet(t, base+"/v1/marketdata/bars?instrument=EURUSD&interval=h1&from=2024-01-01&to=2024-02-01")

	require.Equal(t, http.StatusOK, code, body)
	var page struct {
		Source string `json:"source"`
		Bars   []struct {
			Time        string `json:"time"`
			AvailableAt string `json:"available_at"`
		} `json:"bars"`
		Coverage struct {
			Complete bool `json:"complete"`
		} `json:"coverage"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &page))
	assert.Equal(t, "oanda", page.Source)
	require.Len(t, page.Bars, 2)
	assert.Equal(t, "2024-01-02T00:00:00Z", page.Bars[0].Time)
	assert.Equal(t, "2024-01-02T01:00:00Z", page.Bars[0].AvailableAt)
	assert.True(t, page.Coverage.Complete)

	missing, _ := httpGet(t, base+"/v1/marketdata/bars?instrument=EURUSD&interval=h1&from=2024-01-01&to=2024-03-01&partial=false")
	assert.Equal(t, http.StatusUnprocessableEntity, missing, "strict refuses the range with a missing month")

	require.NoError(t, stop(), "an interrupt is a clean stop")
	_, err := http.Get(base + "/v1/marketdata/bars")
	assert.Error(t, err, "nothing listens any more")
}

func TestServe_ATokenFromTheEnvironmentIsRequired(t *testing.T) {
	root := storetest.Root(t)
	buildMonths(t, root, time.January)
	const token = "s3cret-api-token"
	base, _ := startServer(t, map[string]string{envAPIToken: token}, "--config", writeConfig(t, ""), "--data-root", root, "--listen", "127.0.0.1:0")
	target := base + "/v1/marketdata/bars?instrument=EURUSD&interval=h1&from=2024-01-01&to=2024-02-01"

	noAuth, noAuthBody := httpGet(t, target)
	wrong, _ := httpGet(t, target, "Authorization", "Bearer nope")
	right, _ := httpGet(t, target, "Authorization", "Bearer "+token)

	assert.Equal(t, http.StatusUnauthorized, noAuth)
	assert.NotContains(t, noAuthBody, token)
	assert.Equal(t, http.StatusUnauthorized, wrong)
	assert.Equal(t, http.StatusOK, right)
}

func TestServe_RefusesWhatItShouldNotStartWith(t *testing.T) {
	root := storetest.Root(t)
	common := []string{"serve", "--config", writeConfig(t, ""), "--data-root", root}
	tests := map[string]struct {
		env  map[string]string
		args []string
		want string
	}{
		"every interface without a token": {nil, []string{"--listen", "0.0.0.0:0"}, envAPIToken},
		"an empty host without a token":   {nil, []string{"--listen", ":0"}, envAPIToken},
		"a LAN address without a token":   {nil, []string{"--listen", "192.168.1.5:0"}, envAPIToken},
		"not an address":                  {nil, []string{"--listen", "nonsense"}, "--listen"},
		"a zero timeout":                  {nil, []string{"--request-timeout", "0s"}, "--request-timeout"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := runData(t, tc.env, append(append([]string{}, common...), tc.args...)...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8080": true, "127.1.2.3:1": true, "[::1]:8080": true, "localhost:8080": true,
		"0.0.0.0:8080": false, ":8080": false, "[::]:8080": false, "192.168.1.5:80": false, "example.com:80": false,
	} {
		got, err := loopback(addr)
		require.NoError(t, err, addr)
		assert.Equal(t, want, got, addr)
	}
	_, err := loopback("no-port")
	assert.Error(t, err)
}

func TestServeCommand_IsTransportOnly(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod + "/service": true, mod + "/store": true, mod + "/internal/logging": true, mod + "/rest": true}
	f, err := parser.ParseFile(token.NewFileSet(), "serve.go", nil, parser.ImportsOnly)
	require.NoError(t, err)
	for _, imp := range f.Imports {
		path, uerr := strconv.Unquote(imp.Path.Value)
		require.NoError(t, uerr)
		if path == mod || strings.HasPrefix(path, mod+"/") {
			assert.True(t, allowed[path], "serve.go imports %s", path)
		}
	}
}
