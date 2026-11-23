package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// strategyAddr is the address the server says it accepts strategy clients on.
func strategyAddr(t *testing.T, out *syncBuffer) string {
	t.Helper()
	m := regexp.MustCompile(`strategy clients on (\S+) `).FindStringSubmatch(out.String())
	require.NotNil(t, m, "the server announces where strategies connect: %s", out.String())
	return m[1]
}

// buildEMAX builds examples/emax, the client an operator would run.
func buildEMAX(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "emax")
	out, err := exec.Command("go", "build", "-o", bin, "../../examples/emax").CombinedOutput()
	require.NoError(t, err, string(out))
	return bin
}

// runEMAX runs the built example against the server: it asks for January for both
// instruments, trades it and prints the result.
func runEMAX(t *testing.T, bin, addr string) (string, error) {
	t.Helper()
	out, err := exec.Command(bin, "-trader", addr, "-instrument", "EURUSD,GBPUSD", "-interval", "h1",
		"-from", "2024-01-01", "-to", "2024-02-01").CombinedOutput()
	return string(out), err
}

// runRecord is a run directory's run.json and events.jsonl.
func runRecord(t *testing.T, results, name string) (rec map[string]any, events string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(results, name, "run.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &rec))
	ev, err := os.ReadFile(filepath.Join(results, name, "events.jsonl"))
	require.NoError(t, err)
	return rec, string(ev)
}

// normalized is what two runs of the same trading must share: the record without
// what names the run or its strategy or reads the clock.
func normalized(rec map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range rec {
		switch k {
		case "run_id", "strategy", "started_at", "ended_at":
		default:
			out[k] = v
		}
	}
	if res, ok := out["result"].(map[string]any); ok {
		cp := map[string]any{}
		for k, v := range res {
			if k != "strategy" {
				cp[k] = v
			}
		}
		out["result"] = cp
	}
	return out
}

// The server is up for good: a real example process, built and run separately,
// asks for a backtest, trades it and gets the result; then another does, against
// the same server. Both trade as the built-in strategy does.
func TestServe_AStrategyClientTradesLikeTheBuiltInStrategyAndTheServerStaysUpForASecond(t *testing.T) {
	root := storetest.Root(t)
	putWave(t, root, "EURUSD", 110000)
	putWave(t, root, "GBPUSD", 125000)
	builtinResults := t.TempDir()
	args := backtestArgs(t, root, "2024-02-01")
	args[len(args)-1] = builtinResults // --results-root's value
	_, _, err := runData(t, nil, args...)
	require.NoError(t, err)
	names := runDirs(t, builtinResults)
	require.Len(t, names, 1)
	builtinRec, builtinEvents := runRecord(t, builtinResults, names[0])

	results := t.TempDir()
	_, out, stop := startServerOutput(t, nil, "--config", writeConfig(t, ""), "--data-root", root, "--listen", "127.0.0.1:0", "--results-root", results)
	addr := strategyAddr(t, out)
	bin := buildEMAX(t)

	for i := 1; i <= 2; i++ {
		printed, err := runEMAX(t, bin, addr)
		require.NoError(t, err, "client %d: %s", i, printed)
		assert.Regexp(t, `run run_\w+: completed`, printed)
		assert.Contains(t, printed, `"balance"`, "the result carries the summary")
	}

	dirs := runDirs(t, results)
	require.Len(t, dirs, 2, "each client got its own run")
	for _, name := range dirs {
		rec, events := runRecord(t, results, name)
		assert.Equal(t, "completed", rec["status"])
		assert.Equal(t, map[string]any{"name": "remote", "transport": "tcp"}, withoutAddr(rec["strategy"]))
		assert.Equal(t, normalized(builtinRec), normalized(rec), "the same orders, trades and money as the built-in strategy")
		assert.Equal(t, builtinEvents, events, "the same journal, to the price")
	}
	assert.NotEqual(t, dirs[0], dirs[1])
	require.NoError(t, stop(), "an interrupt is a clean stop")
}

func withoutAddr(v any) map[string]any {
	m, _ := v.(map[string]any)
	out := map[string]any{}
	for k, x := range m {
		if k != "addr" {
			out[k] = x
		}
	}
	return out
}

// A client that crashes, a request that is garbage and a client that is refused
// each end only themselves: the server runs the next client's backtest.
func TestServe_ABadClientEndsOnlyItsRunAndTheNextRunSucceeds(t *testing.T) {
	root := storetest.Root(t)
	putWave(t, root, "EURUSD", 110000)
	putWave(t, root, "GBPUSD", 125000)
	results := t.TempDir()
	_, out, stop := startServerOutput(t, nil, "--config", writeConfig(t, ""), "--data-root", root, "--listen", "127.0.0.1:0", "--results-root", results)
	addr := strategyAddr(t, out)
	req := strategy.RunRequest{Instruments: []string{"EURUSD", "GBPUSD"}, Interval: "h1",
		From: utc(2024, 1, 1, 0), To: utc(2024, 2, 1, 0)}
	idle := func([]marketdata.Bar, []strategy.Event) ([]strategy.Order, error) { return nil, nil }

	t.Run("a client that crashes mid-run", func(t *testing.T) {
		groups := 0
		_, err := strategy.Run(context.Background(), addr, req, func([]marketdata.Bar, []strategy.Event) ([]strategy.Order, error) {
			if groups++; groups == 3 {
				return nil, errors.New("strategy crashed")
			}
			return nil, nil
		})
		require.ErrorContains(t, err, "strategy crashed")
	})
	t.Run("a request that is garbage", func(t *testing.T) {
		conn, err := net.Dial("tcp", addr)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		body := `{"run": nope`
		_, err = conn.Write(append(binary.BigEndian.AppendUint32(nil, uint32(len(body))), body...))
		require.NoError(t, err)
		reply := make([]byte, 4)
		_, err = conn.Read(reply)
		require.NoError(t, err, "the server answers with a refusal")
	})
	t.Run("a run Trader refuses", func(t *testing.T) {
		bad := req
		bad.Interval = "m1" // not a supported interval: the run is made, loading fails
		res, err := strategy.Run(context.Background(), addr, bad, idle)
		if err == nil {
			assert.Equal(t, strategy.StatusFailed, res.Status)
			assert.NotEmpty(t, res.Error)
			assert.NotEmpty(t, res.RunID, "a run that failed after it was made keeps its RunID")
		}
	})
	t.Run("then a good client", func(t *testing.T) {
		res, err := strategy.Run(context.Background(), addr, req, idle)
		require.NoError(t, err)
		assert.Equal(t, strategy.StatusCompleted, res.Status)
		assert.NotEmpty(t, res.RunID)
		assert.NotEmpty(t, res.Summary)
	})

	// The crashed run is on disk as failed with its RunID; the good one as completed.
	statuses := map[string]int{}
	for _, name := range runDirs(t, results) {
		rec, _ := runRecord(t, results, name)
		statuses[rec["status"].(string)]++
	}
	assert.Equal(t, 1, statuses["completed"])
	assert.GreaterOrEqual(t, statuses["failed"], 1, "the crashed run is inspectable")
	require.NoError(t, stop())
}

// Stopping the server mid-run cancels that run, which is journaled as cancelled.
func TestServe_StoppingTheServerCancelsTheRunInProgressAndJournalsItAsCancelled(t *testing.T) {
	root := storetest.Root(t)
	putWave(t, root, "EURUSD", 110000)
	putWave(t, root, "GBPUSD", 125000)
	results := t.TempDir()
	_, out, stop := startServerOutput(t, nil, "--config", writeConfig(t, ""), "--data-root", root, "--listen", "127.0.0.1:0", "--results-root", results)
	addr := strategyAddr(t, out)
	req := strategy.RunRequest{Instruments: []string{"EURUSD", "GBPUSD"}, Interval: "h1",
		From: utc(2024, 1, 1, 0), To: utc(2024, 2, 1, 0)}

	inRun, release := make(chan struct{}), make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		first := true
		_, _ = strategy.Run(context.Background(), addr, req, func([]marketdata.Bar, []strategy.Event) ([]strategy.Order, error) {
			if first {
				first = false
				close(inRun)
			}
			<-release // the client never answers: the run is in progress when the server stops
			return nil, nil
		})
	}()
	<-inRun

	require.NoError(t, stop(), "the server stops cleanly with a run in progress")

	dirs := runDirs(t, results)
	require.Len(t, dirs, 1)
	rec, _ := runRecord(t, results, dirs[0])
	assert.Equal(t, "cancelled", rec["status"], "the run was finalized before the server exited")
	close(release)
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the client did not end")
	}
}

func TestServe_TheStrategyListenerIsOnByDefaultAndAnEmptyAddressTurnsItOff(t *testing.T) {
	f := newServeCmd(newApp()).Flags().Lookup("strategy-listen")
	require.NotNil(t, f)
	assert.Equal(t, "127.0.0.1:9100", f.DefValue, "on unless it is turned off")
	assert.Equal(t, defaultStrategyListen, f.DefValue)

	root := storetest.Root(t)
	_, out, stop := startServerOutput(t, nil, "--config", writeConfig(t, ""), "--data-root", root, "--listen", "127.0.0.1:0", "--strategy-listen", "")
	assert.NotContains(t, out.String(), "strategy clients on")
	require.NoError(t, stop())
}

func TestServe_AStrategyPortInUseSaysHowToTurnTheListenerOff(t *testing.T) {
	root := storetest.Root(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = taken.Close() }()

	_, _, err = runData(t, nil, "serve", "--config", writeConfig(t, ""), "--data-root", root, "--listen", "127.0.0.1:0",
		"--strategy-listen", taken.Addr().String())

	require.Error(t, err)
	assert.Contains(t, err.Error(), `--strategy-listen ""`)
}

func TestListenStrategy(t *testing.T) {
	ln, err := listenStrategy("")
	require.NoError(t, err)
	assert.Nil(t, ln, "empty is off")

	ln, err = listenStrategy("127.0.0.1:0")
	require.NoError(t, err)
	require.NotNil(t, ln)
	assert.True(t, ln.Addr().(*net.TCPAddr).IP.IsLoopback())
	require.NoError(t, ln.Close())

	for _, addr := range []string{"0.0.0.0:0", ":0", "192.168.1.5:0", "example.com:1"} {
		_, err := listenStrategy(addr)
		assert.ErrorContains(t, err, "loopback", addr)
	}
	_, err = listenStrategy("nonsense")
	assert.ErrorContains(t, err, "--strategy-listen")
}

// A request for an instrument with no data fails after the run was made, so the client has its RunID.
func TestStrategyRuns_AnUnknownInstrumentIsAFailedRunWithItsRunID(t *testing.T) {
	root := storetest.Root(t)
	results := t.TempDir()
	_, out, stop := startServerOutput(t, nil, "--config", writeConfig(t, ""), "--data-root", root, "--listen", "127.0.0.1:0", "--results-root", results)
	addr := strategyAddr(t, out)

	res, err := strategy.Run(context.Background(), addr, strategy.RunRequest{Instruments: []string{"NOPE"}, Interval: "h1",
		From: utc(2024, 1, 1, 0), To: utc(2024, 2, 1, 0)},
		func([]marketdata.Bar, []strategy.Event) ([]strategy.Order, error) { return nil, nil })

	require.NoError(t, err, "a failed run is a result")
	assert.Equal(t, strategy.StatusFailed, res.Status)
	assert.NotEmpty(t, res.RunID)
	assert.NotEmpty(t, res.Error)
	require.NoError(t, stop())
}
