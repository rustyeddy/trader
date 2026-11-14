package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToken = "tok-SECRET-abc123"

// fakeOANDA serves an hourly EURUSD series like the provider's candles endpoint:
// candles opening at or after from (the first one only with includeFirst),
// at most count of them, the last one still forming when forming is set.
type fakeOANDA struct {
	*httptest.Server
	hits    atomic.Int32
	series  []time.Time
	forming bool
}

func newFakeOANDA(t *testing.T, from time.Time, n int) *fakeOANDA {
	t.Helper()
	f := &fakeOANDA{}
	for i := range n {
		f.series = append(f.series, from.Add(time.Duration(i)*time.Hour))
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			http.Error(w, "unauthorized: "+r.Header.Get("Authorization"), http.StatusUnauthorized) // a hostile body that echoes the credential it was sent
			return
		}
		q := r.URL.Query()
		start, _ := time.Parse(time.RFC3339Nano, q.Get("from"))
		count, _ := strconv.Atoi(q.Get("count"))
		type candle struct {
			Complete bool              `json:"complete"`
			Time     string            `json:"time"`
			Volume   int               `json:"volume"`
			Bid      map[string]string `json:"bid"`
			Ask      map[string]string `json:"ask"`
		}
		var out []candle
		for i, ts := range f.series {
			if ts.Before(start) || (ts.Equal(start) && q.Get("includeFirst") == "false") {
				continue
			}
			c := candle{Complete: !f.forming || i != len(f.series)-1, Time: ts.Format(time.RFC3339Nano), Volume: 10,
				Bid: map[string]string{"o": "1.10000", "h": "1.10100", "l": "1.09900", "c": "1.10050"},
				Ask: map[string]string{"o": "1.10010", "h": "1.10110", "l": "1.09910", "c": "1.10060"}}
			out = append(out, c)
			if len(out) == count {
				break
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"candles": out})
	}))
	t.Cleanup(f.Close)
	return f
}

// runData runs the real command with the given environment and returns what it
// printed and the log file's text.
func runData(t *testing.T, env map[string]string, args ...string) (stdout, logs string, err error) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "run.log")
	a := newApp()
	a.getenv = func(k string) string { return env[k] }
	var out bytes.Buffer
	a.cmd.SetOut(&out)
	a.cmd.SetErr(io.Discard)
	a.cmd.SetArgs(append([]string{"--log-level", "DEBUG", "--log-output", logPath}, args...))
	prev := slogDefault()
	defer restoreSlog(prev)

	err = a.execute()
	data, rerr := os.ReadFile(logPath)
	require.NoError(t, rerr)
	return out.String(), string(data), err
}

func TestDataDownload_WritesTheMonthFilesAndPrintsASummary(t *testing.T) {
	srv := newFakeOANDA(t, utc(2024, 1, 31, 20), 8) // Jan 31 20:00 .. Feb 1 03:00
	root := t.TempDir()

	out, logs, err := runData(t, map[string]string{envOANDAToken: testToken},
		"data", "download", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD", "--interval", "h1",
		"--from", "2024-01-31", "--to", "2024-02-02", "--base-url", srv.URL)

	require.NoError(t, err)
	jan := filepath.Join(root, "oanda", "raw", "EURUSD", "2024", "01", "EURUSD-2024-01-h1.csv")
	feb := filepath.Join(root, "oanda", "raw", "EURUSD", "2024", "02", "EURUSD-2024-02-h1.csv")
	assert.Contains(t, out, jan+": fetched 4, added 4")
	assert.Contains(t, out, feb+": fetched 4, added 4")
	assert.Contains(t, out, "total: fetched 8, added 8, kept 0")
	head, rerr := os.ReadFile(jan)
	require.NoError(t, rerr)
	assert.True(t, strings.HasPrefix(string(head), "# schema=raw-v1 source=oanda instrument=EURUSD tf=h1 year=2024 month=01\n"))
	for _, text := range []string{out, logs, string(head)} {
		assert.NotContains(t, text, testToken, "the token appears nowhere: not in output, log or file")
	}
	assert.NotEmpty(t, logs, "the run is logged")
}

func TestDataDownload_ARerunAddsNothing(t *testing.T) {
	srv := newFakeOANDA(t, utc(2024, 1, 1, 0), 6)
	root := t.TempDir()
	args := []string{"data", "download", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD", "--interval", "h1",
		"--from", "2024-01-01", "--to", "2024-01-02", "--base-url", srv.URL}
	env := map[string]string{envOANDAToken: testToken}
	_, _, err := runData(t, env, args...)
	require.NoError(t, err)

	out, _, err := runData(t, env, args...)

	require.NoError(t, err)
	assert.Contains(t, out, "total: fetched 6, added 0, kept 6")
}

func TestDataDownload_FormingCandleIsNotStored(t *testing.T) {
	srv := newFakeOANDA(t, utc(2024, 1, 1, 0), 3)
	srv.forming = true

	out, _, err := runData(t, map[string]string{envOANDAToken: testToken},
		"data", "download", "--config", writeConfig(t, ""), "--data-root", t.TempDir(), "--instrument", "EURUSD", "--interval", "h1",
		"--from", "2024-01-01", "--to", "2024-01-02", "--base-url", srv.URL)

	require.NoError(t, err)
	assert.Contains(t, out, "fetched 3, added 2, kept 0, not stored (incomplete) 1")
}

func TestDataDownload_OptionsComeFromTheFileAndTheFlagWins(t *testing.T) {
	srv := newFakeOANDA(t, utc(2024, 1, 1, 0), 4)
	fileRoot, envRoot := t.TempDir(), t.TempDir()
	cfg := writeConfig(t, fmt.Sprintf("marketdata:\n  root: %s\n  provider: oanda\n  base_url: %s\n  instruments: [GBPUSD]\n  interval: h1\n", fileRoot, srv.URL))
	env := map[string]string{envOANDAToken: testToken, envDataRoot: envRoot}

	// Nothing but the range on the command line: the file supplies the instrument,
	// interval and host; the environment's root beats the file's.
	out, _, err := runData(t, env, "data", "download", "--config", cfg, "--from", "2024-01-01", "--to", "2024-01-02")
	require.NoError(t, err)
	assert.Contains(t, out, filepath.Join(envRoot, "oanda", "raw", "GBPUSD"))
	assert.NotContains(t, out, fileRoot)

	// The flag beats the environment and the file.
	flagRoot := t.TempDir()
	out, _, err = runData(t, env, "data", "download", "--config", cfg, "--data-root", flagRoot, "--instrument", "EURUSD", "--from", "2024-01-01", "--to", "2024-01-02")
	require.NoError(t, err)
	assert.Contains(t, out, filepath.Join(flagRoot, "oanda", "raw", "EURUSD"))
}

func TestDataDownload_RefusesBeforeAnyRequest(t *testing.T) {
	srv := newFakeOANDA(t, utc(2024, 1, 1, 0), 2)
	root := t.TempDir()
	base := func(extra ...string) []string {
		return append([]string{"data", "download", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD", "--interval", "h1",
			"--from", "2024-01-01", "--to", "2024-01-02", "--base-url", srv.URL}, extra...)
	}
	withToken := map[string]string{envOANDAToken: testToken}
	for name, tc := range map[string]struct {
		env  map[string]string
		args []string
		want string
	}{
		"no token":          {nil, base(), envOANDAToken},
		"no root":           {withToken, []string{"data", "download", "--config", writeConfig(t, ""), "--instrument", "EURUSD", "--interval", "h1", "--from", "2024-01-01", "--to", "2024-01-02"}, "no data root"},
		"no instrument":     {withToken, []string{"data", "download", "--config", writeConfig(t, ""), "--data-root", root, "--interval", "h1", "--from", "2024-01-01", "--to", "2024-01-02"}, "no instrument"},
		"no interval":       {withToken, []string{"data", "download", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD", "--from", "2024-01-01", "--to", "2024-01-02"}, "no interval"},
		"no range":          {withToken, []string{"data", "download", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD", "--interval", "h1"}, "--from and --to"},
		"reversed range":    {withToken, append(base()[:len(base())-2], "--from", "2024-02-01", "--to", "2024-01-01", "--base-url", srv.URL), "--to must be after --from"},
		"bad date":          {withToken, append(base(), "--from", "yesterday"), "--from"},
		"out of scope":      {withToken, append(base(), "--instrument", "XAUUSD"), "XAUUSD"},
		"bad interval":      {withToken, append(base(), "--interval", "w1"), "w1"},
		"plain http remote": {withToken, append(base(), "--base-url", "http://api.example.com"), "https"},
		"named config gone": {withToken, append(base(), "--config", filepath.Join(t.TempDir(), "missing.yml")), "missing.yml"},
		"secret in config":  {withToken, append(base(), "--config", writeConfig(t, "marketdata:\n  token: hunter2-VALUE\n")), "looks like a secret"},
		"other provider":    {withToken, append(base(), "--config", writeConfig(t, "marketdata:\n  provider: alpaca\n")), "alpaca"},
	} {
		t.Run(name, func(t *testing.T) {
			before := srv.hits.Load()

			out, logs, err := runData(t, tc.env, tc.args...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Equal(t, before, srv.hits.Load(), "no request is made")
			assert.NotContains(t, err.Error()+out+logs, "hunter2-VALUE")
		})
	}
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries, "a refused run writes nothing")
}

func TestDataDownload_AHostileProviderBodyNeverLeaksTheToken(t *testing.T) {
	srv := newFakeOANDA(t, utc(2024, 1, 1, 0), 2)
	root := t.TempDir()

	// The server rejects this token and echoes it back in the body.
	out, logs, err := runData(t, map[string]string{envOANDAToken: "wrong-token-ZZZ"},
		"data", "download", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD", "--interval", "h1",
		"--from", "2024-01-01", "--to", "2024-01-02", "--base-url", srv.URL)

	require.Error(t, err)
	assert.NotContains(t, err.Error()+out+logs, "wrong-token-ZZZ")
	entries, rerr := os.ReadDir(root)
	require.NoError(t, rerr)
	assert.Empty(t, entries)
}

// failingWriter accepts n writes and then fails, like stdout on a full disk.
type failingWriter struct{ n int }

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.n <= 0 {
		return 0, errors.New("disk full")
	}
	w.n--
	return len(p), nil
}

func TestDataDownload_ASummaryWriteFailureIsReported(t *testing.T) {
	for name, okWrites := range map[string]int{"partition line": 0, "total line": 1} {
		t.Run(name, func(t *testing.T) {
			srv := newFakeOANDA(t, utc(2024, 1, 1, 0), 3)
			a := newApp()
			a.getenv = func(k string) string { return map[string]string{envOANDAToken: testToken}[k] }
			a.cmd.SetOut(&failingWriter{n: okWrites})
			a.cmd.SetErr(io.Discard)
			a.cmd.SetArgs([]string{"--log-level", "DEBUG", "--log-output", filepath.Join(t.TempDir(), "run.log"),
				"data", "download", "--config", writeConfig(t, ""), "--data-root", t.TempDir(), "--instrument", "EURUSD", "--interval", "h1",
				"--from", "2024-01-01", "--to", "2024-01-02", "--base-url", srv.URL})
			prev := slogDefault()
			defer restoreSlog(prev)

			err := a.execute()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "disk full")
		})
	}
}

func TestDataCommand_PrintsHelpWithoutASubcommand(t *testing.T) {
	out, _, err := runData(t, nil, "data")

	require.NoError(t, err)
	assert.Contains(t, out, "download")
}

func TestParseRange(t *testing.T) {
	f, to, err := parseRange("2024-01-01", "2024-01-02T12:00:00+02:00")

	require.NoError(t, err)
	assert.True(t, f.Equal(utc(2024, 1, 1, 0)))
	assert.True(t, to.Equal(utc(2024, 1, 2, 10)), "an offset is converted to UTC")
	_, _, err = parseRange("2024-01-01", "")
	assert.Error(t, err)
}
