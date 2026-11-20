package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/strategy/proto"
	strategyserver "github.com/rustyeddy/trader/strategy/server"
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
	base, _, stop = startServerOut(t, env, args...)
	return base, stop
}

// startServerOut is startServer that also returns what the command has printed so
// far. Unless the arguments name --strategy-listen it turns the strategy service
// off, so tests never fight over the default port.
func startServerOut(t *testing.T, env map[string]string, args ...string) (base string, out *syncBuffer, stop func() error) {
	t.Helper()
	if !slices.Contains(args, "--strategy-listen") {
		args = append(args, "--strategy-listen", strategyOff)
	}
	a := newApp()
	a.getenv = func(k string) string { return env[k] }
	out = &syncBuffer{}
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
	return base, out, stop
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
		"strategy on every interface":     {nil, []string{"--listen", "127.0.0.1:0", "--strategy-listen", "0.0.0.0:0"}, "--strategy-listen"},
		"strategy on a LAN address":       {nil, []string{"--listen", "127.0.0.1:0", "--strategy-listen", "192.168.1.5:0"}, "--strategy-listen"},
		"strategy on an empty host":       {nil, []string{"--listen", "127.0.0.1:0", "--strategy-listen", ":0"}, "--strategy-listen"},
		"strategy not an address":         {nil, []string{"--listen", "127.0.0.1:0", "--strategy-listen", "nonsense"}, "--strategy-listen"},
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
	allowed := map[string]bool{mod + "/service": true, mod + "/internal/store": true, mod + "/internal/logging": true, mod + "/rest": true,
		mod + "/strategy/proto": true, mod + "/strategy/server": true}
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

func TestServe_TheStrategyServiceDefaultsToALoopbackPort(t *testing.T) {
	f := newServeCmd(newApp()).Flags().Lookup("strategy-listen")

	require.NotNil(t, f)
	assert.Equal(t, defaultStrategyListen, f.DefValue)
	local, err := loopback(defaultStrategyListen)
	require.NoError(t, err)
	assert.True(t, local, "the default is loopback")
	assert.NotEqual(t, defaultListen, defaultStrategyListen, "it must not take the API's port")
}

func TestServe_TheStrategyServiceListensAndRefusesRegistrationsUntilTheEngineIsAttached(t *testing.T) {
	root := storetest.Root(t)
	_, out, stop := startServerOut(t, nil, "--config", writeConfig(t, ""), "--data-root", root, "--listen", "127.0.0.1:0", "--strategy-listen", "127.0.0.1:0")
	re := regexp.MustCompile(`strategy protocol on tcp://(\S+)`)
	require.Eventually(t, func() bool { return re.MatchString(out.String()) }, 5*time.Second, 10*time.Millisecond, "it announces its address")
	addr := re.FindStringSubmatch(out.String())[1]

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	reg := &proto.Register{Header: proto.Header{Seq: 1}, Name: "emax", Capabilities: []string{"bars"},
		Run: proto.Run{Mode: proto.ModeBacktest, Interval: "H1", Instruments: []string{"EURUSD"},
			From: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)}}
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, proto.Write(conn, reg, proto.DefaultMaxFrame))
	m, err := proto.Read(conn, proto.DefaultMaxFrame)

	require.NoError(t, err)
	f, ok := m.(*proto.Failure)
	require.True(t, ok, "expected an error message, got %T", m)
	assert.Equal(t, proto.CodeRejected, f.Code)
	assert.Contains(t, f.Message, "not available")
	assert.Contains(t, f.Message, "#703")

	require.NoError(t, stop(), "an interrupt stops both services cleanly")
	_, derr := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	assert.Error(t, derr, "nothing listens on the strategy port any more")
}

func TestServe_TheStrategyServiceCanBeTurnedOff(t *testing.T) {
	root := storetest.Root(t)
	_, out, _ := startServerOut(t, nil, "--config", writeConfig(t, ""), "--data-root", root, "--listen", "127.0.0.1:0")

	assert.Contains(t, out.String(), "strategy protocol off")
	assert.NotContains(t, out.String(), "strategy protocol on")
}

func TestServe_AStrategyPortThatIsTakenIsAStartupError(t *testing.T) {
	root := storetest.Root(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = taken.Close() }()

	_, _, err = runData(t, nil, "serve", "--config", writeConfig(t, ""), "--data-root", root,
		"--listen", "127.0.0.1:0", "--strategy-listen", taken.Addr().String())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--strategy-listen")
}

func TestAdmitNoEngine_RefusesEveryRegistrationWithAnExplanation(t *testing.T) {
	_, err := admitNoEngine(context.Background(), &proto.Register{})

	require.ErrorIs(t, err, strategyserver.ErrRejected)
	assert.Contains(t, err.Error(), "not available")
}
