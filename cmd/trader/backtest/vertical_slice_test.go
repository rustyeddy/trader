package backtest_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cmdbacktest "github.com/rustyeddy/trader/cmd/trader/backtest"
)

// extractRunID pulls report.run.run_id out of a JSON-rendered
// report.BacktestReport, without depending on the report package's
// own (unexported-to-this-test) Go types.
func extractRunID(t *testing.T, jsonOutput string) string {
	t.Helper()
	var doc struct {
		Run struct {
			RunID string `json:"run_id"`
		} `json:"run"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonOutput), &doc))
	require.NotEmpty(t, doc.Run.RunID)
	return doc.Run.RunID
}

// TestVerticalSlice_RunThenShow exercises the full acceptance
// criterion (issue #222): CLI -> service/backtest -> backtest.Runner
// -> M4 pipeline -> simulator. "run" replays the committed EUR/USD H1
// fixture with the package's own demoStrategy (a single deterministic
// buy entry on the first bar), which must be sized, planned, risk-
// admitted, submitted to the simulated broker, and filled — a
// consequence that can only exist if the canonical M4 pipeline and
// simulator actually ran, not merely that the process exited 0.
// "show" then renders the exact same persisted report with no
// recomputation.
func TestVerticalSlice_RunThenShow(t *testing.T) {
	outputDir := t.TempDir()

	runCmd := cmdbacktest.New()
	var runOut bytes.Buffer
	runCmd.SetOut(&runOut)
	runCmd.SetArgs([]string{
		"run",
		"--symbol", "EURUSD",
		"--interval", "H1",
		"--from", "2024-01-08T00:00:00Z",
		"--to", "2024-01-08T04:00:00Z",
		"--starting-cash", "10000",
		"--currency", "USD",
		"--risk-fraction", "0.01",
		"--adverse-distance", "0.01000",
		"--data-raw-root", "testdata/raw/oanda",
		"--output-dir", outputDir,
		"--format", "json",
	})
	require.NoError(t, runCmd.Execute())

	runOutput := runOut.String()
	require.NotEmpty(t, runOutput)
	assert.Contains(t, runOutput, `"run_id"`)

	// A filled entry is the M4-pipeline/simulator consequence this test
	// exists to prove: either a closed trade or, since demoStrategy
	// never exits, an open one — either way, at least one position must
	// have actually been submitted and filled, not merely proposed.
	assert.True(t,
		bytes.Contains([]byte(runOutput), []byte(`"open_trades"`)) &&
			!bytes.Contains([]byte(runOutput), []byte(`"open_trades": []`)),
		"expected at least one open trade in run output, proving the M4 pipeline and simulator actually filled an order:\n%s", runOutput)

	runID := extractRunID(t, runOutput)

	showCmd := cmdbacktest.New()
	var showOut bytes.Buffer
	showCmd.SetOut(&showOut)
	showCmd.SetArgs([]string{
		"show", runID,
		"--output-dir", outputDir,
		"--format", "json",
	})
	require.NoError(t, showCmd.Execute())

	assert.JSONEq(t, runOutput, showOut.String(), "show must render the exact same persisted report run produced, with no recomputation")
}

// TestVerticalSlice_ShowRejectsUnknownRunID proves show fails clearly
// rather than fabricating output for a run that was never persisted.
func TestVerticalSlice_ShowRejectsUnknownRunID(t *testing.T) {
	outputDir := t.TempDir()

	showCmd := cmdbacktest.New()
	showCmd.SetArgs([]string{
		"show", "run_01HK153X003DMSTAVHVKTAA8HV",
		"--output-dir", outputDir,
	})
	require.Error(t, showCmd.Execute())
}
