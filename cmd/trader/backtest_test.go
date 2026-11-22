package main

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/provider/oanda"
	"github.com/rustyeddy/trader/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// evenRec is a raw hourly candle at the given bid (in ticks) with a two-tick
// spread, so its mid is a whole tick.
func evenRec(t time.Time, ticks int64) oanda.Record {
	p := func(v int64) num.Price { return num.MustParsePrice(fmt.Sprintf("%d.%05d", v/100000, v%100000)) }
	return oanda.Record{Time: t, BidOpen: p(ticks), BidHigh: p(ticks + 10), BidLow: p(ticks - 10), BidClose: p(ticks),
		AskOpen: p(ticks + 2), AskHigh: p(ticks + 12), AskLow: p(ticks - 8), AskClose: p(ticks + 2), Volume: 100, Complete: true}
}

// putWave puts a month of hourly candles for the instrument in the raw store and
// builds the canonical month with the data build command.
func putWave(t *testing.T, root, symbol string, base int64) {
	t.Helper()
	var rows []oanda.Record
	for h, p := range []int64{0, 0, 0, 0, 500, 1000, 1500, 1000, 500, 0, -500, -1000} {
		rows = append(rows, evenRec(utc(2024, 1, 2, h), base+p))
	}
	putRawMonth(t, symbol, oanda.RawH1, 2024, time.January, rows...)
	args := buildArgs(t, root, "2024-02-01")
	args[7] = symbol // --instrument
	_, _, err := runData(t, nil, args...)
	require.NoError(t, err)
}

func backtestArgs(t *testing.T, root, to string, extra ...string) []string {
	t.Helper()
	return append([]string{"backtest", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD,GBPUSD",
		"--interval", "h1", "--from", "2024-01-01", "--to", to, "--results-root", t.TempDir()}, extra...)
}

func TestBacktest_RunsTwoInstrumentsFromTheStoreAndPrintsTheSummaryWithoutAToken(t *testing.T) {
	root := storetest.Root(t)
	putWave(t, root, "EURUSD", 110000)
	putWave(t, root, "GBPUSD", 125000)

	out, logs, err := runData(t, map[string]string{}, backtestArgs(t, root, "2024-02-01")...)

	require.NoError(t, err, "nothing is fetched, so no token")
	assert.Contains(t, out, "oanda EURUSD,GBPUSD h1 [2024-01-01T00:00:00Z, 2024-02-01T00:00:00Z): 24 bars")
	assert.Contains(t, out, "orders: submitted")
	assert.NotEmpty(t, logs)
}

// runDirs lists the run directories under root (each named by a RunID).
func runDirs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestBacktest_JournalsTheRunAndSaysWhere(t *testing.T) {
	root := storetest.Root(t)
	putWave(t, root, "EURUSD", 110000)
	putWave(t, root, "GBPUSD", 125000)
	results := t.TempDir()
	args := backtestArgs(t, root, "2024-02-01")
	args[len(args)-1] = results // --results-root

	out, _, err := runData(t, nil, args...)

	require.NoError(t, err)
	dirs := runDirs(t, results)
	require.Len(t, dirs, 1)
	assert.Contains(t, out, "run "+dirs[0]+": "+filepath.Join(results, dirs[0]), "the summary ends by saying where the run is")
	assert.FileExists(t, filepath.Join(results, dirs[0], "run.json"))
	assert.FileExists(t, filepath.Join(results, dirs[0], "events.jsonl"))
}

// TestBacktest_ResultsRootIsFlagThenEnvironmentThenFile checks the order the
// results root is resolved in, the same as the data root's.
func TestBacktest_ResultsRootIsFlagThenEnvironmentThenFile(t *testing.T) {
	root := storetest.Root(t)
	putWave(t, root, "EURUSD", 110000)
	putWave(t, root, "GBPUSD", 125000)
	flagDir, envDir, fileDir := t.TempDir(), t.TempDir(), t.TempDir()
	withFile := func() []string {
		args := backtestArgs(t, root, "2024-02-01")
		args = args[:len(args)-2] // no --results-root
		args[2] = writeConfig(t, "results:\n  root: "+fileDir+"\n")
		return args
	}

	_, _, err := runData(t, nil, withFile()...)
	require.NoError(t, err)
	assert.Len(t, runDirs(t, fileDir), 1, "the config file's root, with no flag or environment")

	_, _, err = runData(t, map[string]string{envResultsRoot: envDir}, withFile()...)
	require.NoError(t, err)
	assert.Len(t, runDirs(t, envDir), 1, "the environment beats the file")
	assert.Len(t, runDirs(t, fileDir), 1, "and the file got nothing more")

	_, _, err = runData(t, map[string]string{envResultsRoot: envDir}, append(withFile(), "--results-root", flagDir)...)
	require.NoError(t, err)
	assert.Len(t, runDirs(t, flagDir), 1, "the flag beats both")
	assert.Len(t, runDirs(t, envDir), 1)
}

func TestOrDefault(t *testing.T) {
	assert.Equal(t, "x", orDefault("x", "d"))
	assert.Equal(t, "d", orDefault("", "d"))
	assert.Equal(t, "/srv/trading/backtest/results", service.DefaultResultsRoot)
}

func TestBacktest_AFailedRunIsJournaledToo(t *testing.T) {
	root := storetest.Root(t)
	putWave(t, root, "EURUSD", 110000)
	putWave(t, root, "GBPUSD", 125000)
	results := t.TempDir()
	args := backtestArgs(t, root, "2024-03-01") // February is missing
	args[len(args)-1] = results

	_, _, err := runData(t, nil, args...)

	require.ErrorIs(t, err, service.ErrNotLoadable)
	dirs := runDirs(t, results)
	require.Len(t, dirs, 1)
	assert.ErrorContains(t, err, filepath.Join(results, dirs[0]), "the error says where the run is")
	data, rerr := os.ReadFile(filepath.Join(results, dirs[0], "run.json"))
	require.NoError(t, rerr)
	assert.Contains(t, string(data), `"status": "failed"`)
}

func TestBacktest_FailsOnAMissingMonthUnlessAllowed(t *testing.T) {
	root := storetest.Root(t)
	putWave(t, root, "EURUSD", 110000)
	putWave(t, root, "GBPUSD", 125000)

	out, _, err := runData(t, nil, backtestArgs(t, root, "2024-03-01")...)
	allowedOut, _, allowedErr := runData(t, nil, backtestArgs(t, root, "2024-03-01", "--allow-missing")...)

	require.ErrorIs(t, err, service.ErrNotLoadable)
	assert.Contains(t, err.Error(), "2024-02 missing")
	assert.NotContains(t, out, "bars", "nothing ran")
	require.NoError(t, allowedErr)
	assert.Contains(t, allowedOut, "24 bars")
}

func TestBacktest_RejectsBadOptionsBeforeDoingAnything(t *testing.T) {
	root := storetest.Root(t)
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"no instrument": {[]string{"backtest", "--config", writeConfig(t, ""), "--data-root", root, "--interval", "h1", "--from", "2024-01-01", "--to", "2024-02-01"}, "no instrument"},
		"no range":      {[]string{"backtest", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD", "--interval", "h1"}, "--from and --to"},
		"reversed":      {backtestArgs(t, root, "2023-12-01"), "--to must be after --from"},
		"other source":  {backtestArgs(t, root, "2024-02-01", "--source", "alpaca"), `source "alpaca" is not supported`},
	} {
		t.Run(name, func(t *testing.T) {
			out, _, err := runData(t, nil, tc.args...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, out, "bars")
		})
	}
}

// TestBacktestCommand_IsTransportOnly keeps the command thin: backtest.go imports
// the application service, the store and logging, never the engine, a provider or
// a market-data package.
func TestBacktestCommand_IsTransportOnly(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod + "/service": true, mod + "/internal/store": true, mod + "/internal/logging": true}
	f, err := parser.ParseFile(token.NewFileSet(), "backtest.go", nil, parser.ImportsOnly)
	require.NoError(t, err)
	for _, imp := range f.Imports {
		path, uerr := strconv.Unquote(imp.Path.Value)
		require.NoError(t, uerr)
		if path == mod || strings.HasPrefix(path, mod+"/") {
			assert.True(t, allowed[path], "backtest.go imports %s: the command may import only the application service, the store and logging", path)
		}
	}
}

// startEMAXProcess builds examples/emax and runs it as a separate process, as an
// operator would, and returns the address it prints.
func startEMAXProcess(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "emax")
	out, err := exec.Command("go", "build", "-o", bin, "../../examples/emax").CombinedOutput()
	require.NoError(t, err, string(out))

	cmd := exec.Command(bin, "-addr", "127.0.0.1:0")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	return strings.TrimSpace(strings.TrimPrefix(line, "listening on "))
}

// TestBacktest_WorkerTradesLikeTheBuiltInStrategy runs the command twice on the same
// stored data: with the built-in EMAX, and with the EMAX example as a separate process
// over loopback TCP. The summaries are the same.
func TestBacktest_WorkerTradesLikeTheBuiltInStrategy(t *testing.T) {
	root := storetest.Root(t)
	putWave(t, root, "EURUSD", 110000)
	putWave(t, root, "GBPUSD", 125000)
	builtin, _, err := runData(t, nil, backtestArgs(t, root, "2024-02-01")...)
	require.NoError(t, err)

	addr := startEMAXProcess(t)
	results := t.TempDir()
	args := backtestArgs(t, root, "2024-02-01", "--worker", addr)
	args[len(args)-3] = results // --results-root's value, which comes before --worker
	remote, _, err := runData(t, nil, args...)

	require.NoError(t, err)
	summary := func(out string) string { return out[:strings.LastIndex(strings.TrimSpace(out), "\nrun ")] }
	assert.Equal(t, summary(builtin), summary(remote), "the same orders, trades and money")
	assert.Contains(t, remote, "orders: submitted")
	assert.NotContains(t, remote, "submitted 0,", "it traded")
	dirs := runDirs(t, results)
	require.Len(t, dirs, 1)
	rec, rerr := os.ReadFile(filepath.Join(results, dirs[0], "run.json"))
	require.NoError(t, rerr)
	assert.Contains(t, string(rec), `"transport": "tcp"`)
	assert.Contains(t, string(rec), `"addr": "`+addr+`"`)
}

func TestBacktest_AWorkerThatIsNotListeningIsAFailedRun(t *testing.T) {
	root := storetest.Root(t)
	putWave(t, root, "EURUSD", 110000)
	putWave(t, root, "GBPUSD", 125000)
	results := t.TempDir()
	args := backtestArgs(t, root, "2024-02-01", "--worker", "127.0.0.1:1")
	args[len(args)-3] = results

	_, _, err := runData(t, nil, args...)

	require.Error(t, err)
	dirs := runDirs(t, results)
	require.Len(t, dirs, 1, "the run exists, and says it failed")
	rec, rerr := os.ReadFile(filepath.Join(results, dirs[0], "run.json"))
	require.NoError(t, rerr)
	assert.Contains(t, string(rec), `"status": "failed"`)
}
