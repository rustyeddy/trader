package backtest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/journal/jsonl"
	cmdbacktest "github.com/rustyeddy/trader/cmd/trader/backtest"
	"github.com/rustyeddy/trader/journal"
)

// writeConfigFile writes contents to a fresh temp file and returns its
// path, so --config tests never depend on a checked-in fixture whose
// dates would eventually fall outside testdata/raw/oanda's coverage.
func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backtest.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

// TestRunCLI_ConfigFileDrivesFullBacktest proves issue #247's own
// acceptance criterion end to end: a backtest can be configured (and
// run to completion) purely from a YAML file, without any of the
// individual --symbol/--from/--to/... flags this command also accepts.
func TestRunCLI_ConfigFileDrivesFullBacktest(t *testing.T) {
	configPath := writeConfigFile(t, `
backtest:
  symbol: EURUSD
  interval: H1
  from: 2024-01-08T00:00:00Z
  to: 2024-01-08T04:00:00Z
  starting_capital: 10000
  risk_fraction: 0.01
  adverse_distance: 0.01000
`)

	outputDir := t.TempDir()
	runCmd := cmdbacktest.New()
	var out bytes.Buffer
	runCmd.SetOut(&out)
	runCmd.SetArgs([]string{
		"run",
		"--config", configPath,
		"--data-raw-root", "testdata/raw/oanda",
		"--output-dir", outputDir,
		"--format", "json",
	})
	require.NoError(t, runCmd.Execute())
	assert.Contains(t, out.String(), `"run_id"`)
}

// TestRunCLI_ExplicitFlagBeatsConfigFile proves the precedence
// CONTRIBUTING.org/#247 require: an explicit flag wins over a
// conflicting --config value. The config file names a starting
// capital that would leave --risk-fraction's 1% sizing unable to
// afford anything meaningful; --starting-cash overrides it back to a
// normal value, so a successful run only occurs if the override was
// actually honored.
func TestRunCLI_ExplicitFlagBeatsConfigFile(t *testing.T) {
	configPath := writeConfigFile(t, `
backtest:
  symbol: EURUSD
  interval: H1
  from: 2024-01-08T00:00:00Z
  to: 2024-01-08T04:00:00Z
  starting_capital: 1
  risk_fraction: 0.01
  adverse_distance: 0.01000
`)

	outputDir := t.TempDir()
	runCmd := cmdbacktest.New()
	var out bytes.Buffer
	runCmd.SetOut(&out)
	runCmd.SetArgs([]string{
		"run",
		"--config", configPath,
		"--starting-cash", "10000", // overrides the file's starting_capital: 1
		"--data-raw-root", "testdata/raw/oanda",
		"--output-dir", outputDir,
		"--format", "json",
	})
	require.NoError(t, runCmd.Execute())
	assert.Contains(t, out.String(), `"run_id"`)

	var doc struct {
		Performance struct {
			StartingCapital struct {
				Amount   string `json:"amount"`
				Currency string `json:"currency"`
			} `json:"starting_capital"`
		} `json:"performance"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	assert.Equal(t, "10000", doc.Performance.StartingCapital.Amount, "expected --starting-cash to override the config file's starting_capital: 1")
	assert.Equal(t, "USD", doc.Performance.StartingCapital.Currency)
}

// TestRunCLI_ConfigFileValidationFailsBeforeRun proves invalid
// configuration is rejected before any backtest infrastructure runs:
// this config's periods are invalid but --data-raw-root points at a
// real fixture, so a failure here can only come from buildRunConfig's
// validation running before market data or the simulator are ever
// touched, not from some unrelated data-access error.
func TestRunCLI_ConfigFileValidationFailsBeforeRun(t *testing.T) {
	configPath := writeConfigFile(t, `
backtest:
  symbol: EURUSD
  from: 2024-01-08T00:00:00Z
  to: 2024-01-08T04:00:00Z
  adverse_distance: 0.01000

strategy:
  fast_period: 50
  slow_period: 20
`)

	runCmd := cmdbacktest.New()
	runCmd.SetArgs([]string{
		"run",
		"--config", configPath,
		"--data-raw-root", "testdata/raw/oanda",
	})
	err := runCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slow_period")
}

// TestRunCLI_MultiSymbolWithConfigRejected proves --config's own
// single-instrument scope (issue #247) is enforced, rather than
// silently using only the first --symbol or only the config's symbol.
func TestRunCLI_MultiSymbolWithConfigRejected(t *testing.T) {
	configPath := writeConfigFile(t, `
backtest:
  from: 2024-01-08T00:00:00Z
  to: 2024-01-08T04:00:00Z
  adverse_distance: 0.01000
`)

	runCmd := cmdbacktest.New()
	runCmd.SetArgs([]string{
		"run",
		"--config", configPath,
		"--symbol", "EURUSD",
		"--symbol", "GBPUSD",
		"--data-raw-root", "testdata/raw/oanda",
	})
	err := runCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "single-instrument")
}

// TestRunCLI_ConfigDrivesRealEMACrossoverStrategy proves issue #252's
// own central requirement: --config runs the real EMA crossover
// strategy (strategy/emacross), not the demo strategy, and it actually
// trades — this fixture (shared with strategy/emacross's own EMA-06
// tests) is engineered so a fast=3/slow=5 EMA produces one bullish
// cross (bar 7) and one bearish reversal (bar 12). Confirming a real,
// data-dependent trade occurred (not merely that the command exited 0)
// is what distinguishes this from a wiring smoke test.
func TestRunCLI_ConfigDrivesRealEMACrossoverStrategy(t *testing.T) {
	configPath := writeConfigFile(t, `
backtest:
  symbol: EURUSD
  interval: H1
  from: 2024-03-04T00:00:00Z
  to: 2024-03-04T14:00:00Z
  starting_capital: 10000
  risk_fraction: 0.01
  adverse_distance: 0.01000

strategy:
  name: ema-cross
  fast_period: 3
  slow_period: 5
`)

	outputDir := t.TempDir()
	runCmd := cmdbacktest.New()
	var out bytes.Buffer
	runCmd.SetOut(&out)
	runCmd.SetArgs([]string{
		"run",
		"--config", configPath,
		"--data-raw-root", "testdata/raw/oanda",
		"--output-dir", outputDir,
		"--format", "json",
	})
	require.NoError(t, runCmd.Execute())

	var doc struct {
		Run struct {
			StrategyName string `json:"strategy_name"`
			ConfigDigest string `json:"config_digest"`
		} `json:"run"`
		ClosedTrades []struct {
			Side string `json:"side"`
		} `json:"closed_trades"`
		OpenTrades []struct {
			Side string `json:"side"`
		} `json:"open_trades"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))

	assert.Equal(t, "ema-cross", doc.Run.StrategyName)
	assert.NotEmpty(t, doc.Run.ConfigDigest)
	require.Len(t, doc.ClosedTrades, 1, "the bar-12 reversal must have closed the bar-7 long")
	assert.Equal(t, "long", doc.ClosedTrades[0].Side)
	require.Len(t, doc.OpenTrades, 1, "the bar-12 reversal's re-entry must remain open")
	assert.Equal(t, "short", doc.OpenTrades[0].Side)
}

// TestRunCLI_UnsupportedStrategyNameRejected proves strategy.name is
// a truthful, validated claim rather than an ignored label: there is
// no strategy registry, so a name other than "ema-cross" must fail
// loudly instead of silently running EMA crossover anyway (PR #263
// review).
func TestRunCLI_UnsupportedStrategyNameRejected(t *testing.T) {
	configPath := writeConfigFile(t, `
backtest:
  symbol: EURUSD
  from: 2024-01-08T00:00:00Z
  to: 2024-01-08T04:00:00Z
  adverse_distance: 0.01000

strategy:
  name: some-other-strategy
`)

	runCmd := cmdbacktest.New()
	runCmd.SetArgs([]string{
		"run",
		"--config", configPath,
		"--data-raw-root", "testdata/raw/oanda",
	})
	err := runCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "some-other-strategy")
	assert.Contains(t, err.Error(), "ema-cross")
}

// TestRunCLI_JournalWritesDurableJSONLRecord proves --journal (added
// for issue #256, EMA-11's own "journal/report artifact references"
// requirement) actually writes a valid, readable JSONL journal for the
// run, bracketed by KindRunStarted and KindRunCompleted — not merely
// that the flag is accepted. --journal is off by default; this test
// exists specifically to prove the opt-in path works, since no other
// "trader backtest run" test exercises it.
func TestRunCLI_JournalWritesDurableJSONLRecord(t *testing.T) {
	configPath := writeConfigFile(t, `
backtest:
  symbol: EURUSD
  interval: H1
  from: 2024-01-08T00:00:00Z
  to: 2024-01-08T04:00:00Z
  starting_capital: 10000
  risk_fraction: 0.01
  adverse_distance: 0.01000
`)

	outputDir := t.TempDir()
	journalPath := filepath.Join(t.TempDir(), "run.jsonl")

	runCmd := cmdbacktest.New()
	var out bytes.Buffer
	runCmd.SetOut(&out)
	runCmd.SetArgs([]string{
		"run",
		"--config", configPath,
		"--data-raw-root", "testdata/raw/oanda",
		"--output-dir", outputDir,
		"--format", "json",
		"--journal", journalPath,
	})
	require.NoError(t, runCmd.Execute())
	assert.Contains(t, out.String(), `"run_id"`)

	reader, err := jsonl.OpenReader(journalPath)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	var kinds []journal.Kind
	ctx := context.Background()
	for {
		entry, err := reader.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		kinds = append(kinds, entry.Record.Kind)
	}

	require.NotEmpty(t, kinds, "the journal must contain at least the run-bracketing records")
	assert.Equal(t, journal.KindRunStarted, kinds[0], "the journal's first record must be KindRunStarted")
	assert.Equal(t, journal.KindRunCompleted, kinds[len(kinds)-1], "the journal's last record must be KindRunCompleted")
}
