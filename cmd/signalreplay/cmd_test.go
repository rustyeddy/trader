package signalreplay

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
)

func TestCmdGen_RequiresOut(t *testing.T) {
	cmd := cmdGen()
	cmd.SetArgs([]string{
		"--signals", "testdata/gen_fixture.csv",
		"--exit", "chandelier",
	})
	err := cmd.Execute()
	assert.ErrorContains(t, err, "--out")
}

func TestCmdGen_WritesConfigFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "replay.yml")

	cmd := cmdGen()
	cmd.SetArgs([]string{
		"--signals", "testdata/gen_fixture.csv",
		"--exit", "chandelier",
		"--exit-params", "atr_period=14,multiplier=2.0",
		"--out", out,
	})
	require.NoError(t, cmd.Execute())

	b, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(b), "kind: signalreplay")
	assert.Contains(t, string(b), "kind: chandelier")
}

func TestResolveRunConfigPath_UsesPositionalArg(t *testing.T) {
	var f genFlags
	addGenFlags(&cobra.Command{}, &f)
	path, err := resolveRunConfigPath([]string{"existing.yml"}, &f)
	require.NoError(t, err)
	assert.Equal(t, "existing.yml", path)
}

func TestResolveRunConfigPath_GeneratesTempFileWhenNoArg(t *testing.T) {
	f := genFlags{opts: DefaultGenOptions()}
	f.opts.SignalsPath = "testdata/gen_fixture.csv"
	f.opts.ExitKind = "chandelier"

	path, err := resolveRunConfigPath(nil, &f)
	require.NoError(t, err)
	defer os.Remove(path)

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(b), "kind: signalreplay")
}

func TestResolveRunConfigPath_PropagatesGenerateErrors(t *testing.T) {
	f := genFlags{opts: DefaultGenOptions()}
	f.opts.SignalsPath = "testdata/gen_fixture.csv"
	f.opts.ExitKind = "" // missing exit -> GenerateConfig error

	_, err := resolveRunConfigPath(nil, &f)
	assert.ErrorContains(t, err, "--exit")
}

func TestCmdRun_RequiresReportDir(t *testing.T) {
	cmd := cmdRun()
	cmd.SetArgs([]string{"testdata/gen_fixture.csv"})
	err := cmd.Execute()
	assert.ErrorContains(t, err, "--report-dir")
}

func TestCmdReport_RequiresFlags(t *testing.T) {
	cmd := cmdReport()
	err := cmd.Execute()
	assert.ErrorContains(t, err, "--reports")
}

func TestCmdReport_RequiresSignals(t *testing.T) {
	dir := t.TempDir()
	cmd := cmdReport()
	cmd.SetArgs([]string{"--reports", dir})
	err := cmd.Execute()
	assert.ErrorContains(t, err, "--signals")
}

func TestCmdReport_RequiresOut(t *testing.T) {
	dir := t.TempDir()
	cmd := cmdReport()
	cmd.SetArgs([]string{"--reports", dir, "--signals", "testdata/report_fixture.csv"})
	err := cmd.Execute()
	assert.ErrorContains(t, err, "--out")
}

func TestCmdReport_WritesOutcomeCSV(t *testing.T) {
	dir := t.TempDir()
	writeSampleReport(t, dir, "eurusd-signalreplay", []backtest.BacktestReportTrade{
		{
			Instrument: "EUR_USD", Side: "long", Reason: "signalreplay:2024-01-02T00:00:00Z",
			OpenTime: "2024-01-03T00:00:00Z", CloseTime: "2024-01-08T00:00:00Z",
			OpenPrice: 1.10, ClosePrice: 1.12, InitialStopPrice: 1.09, CloseCause: "StopLoss", PNL: 20,
		},
	})

	out := filepath.Join(dir, "outcome.csv")
	cmd := cmdReport()
	cmd.SetArgs([]string{"--reports", dir, "--signals", "testdata/report_fixture.csv", "--out", out})
	require.NoError(t, cmd.Execute())

	b, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(b), "signal_date,instrument,bias")
	assert.Contains(t, string(b), "EURUSD,long")
}
