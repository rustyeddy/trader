package backtest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cmdbacktest "github.com/rustyeddy/trader/cmd/trader/backtest"
	"github.com/rustyeddy/trader/internal/adapters/journal/jsonl"
	"github.com/rustyeddy/trader/internal/journal"
	"github.com/rustyeddy/trader/num"
)

// marginRunArgs is the committed EUR/USD H1 buy-and-hold fixture with
// no --initial-margin-ratio (so the 1.0 default applies unless extra
// adds one). At 1% risk over a 0.01 adverse distance on 10000, the
// demo strategy's entry sizes 10000 EUR at ~1.10: about 1.1x equity.
func marginRunArgs(t *testing.T, journalPath string, extra ...string) []string {
	t.Helper()
	args := []string{
		"run",
		"--symbol", "EURUSD",
		"--interval", "H1",
		"--from", "2024-01-08T00:00:00Z",
		"--to", "2024-01-08T04:00:00Z",
		"--starting-cash", "10000",
		"--currency", "USD",
		"--adverse-distance", "0.01000",
		"--data-raw-root", "testdata/raw/oanda",
		"--data-store-root", t.TempDir(),
		"--output-dir", t.TempDir(),
		"--format", "json",
	}
	if journalPath != "" {
		args = append(args, "--journal", journalPath)
	}
	return append(args, extra...)
}

// runReport executes args and returns the rendered JSON report.
func runReport(t *testing.T, args []string) (string, error) {
	t.Helper()
	cmd := cmdbacktest.New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

type marginReport struct {
	Run struct {
		ConfigDigest string `json:"config_digest"`
	} `json:"run"`
	OpenTrades []json.RawMessage `json:"open_trades"`
	Margin     struct {
		InitialMarginRatio  *string `json:"initial_margin_ratio"`
		Rejections          int     `json:"margin_rejection_count"`
		AdmissionRejections int     `json:"admission_rejection_count"`
		FillRejections      int     `json:"fill_rejection_count"`
		PeakGrossNotional   *struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"peak_gross_notional"`
		PeakGrossLeverage *string `json:"peak_gross_leverage"`
	} `json:"margin"`
}

func parseMarginReport(t *testing.T, out string) marginReport {
	t.Helper()
	var r marginReport
	require.NoError(t, json.Unmarshal([]byte(out), &r))
	return r
}

// journalEntries reads every entry of a JSONL journal.
func journalEntries(t *testing.T, path string) []journal.Entry {
	t.Helper()
	r, err := jsonl.OpenReader(path)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	var entries []journal.Entry
	for {
		e, err := r.Next(context.Background())
		if errors.Is(err, io.EOF) {
			return entries
		}
		require.NoError(t, err)
		entries = append(entries, e)
	}
}

// marginRejections returns the journaled risk decisions rejected by the
// account_initial_margin rule.
func marginRejections(entries []journal.Entry) int {
	n := 0
	for _, e := range entries {
		if e.Kind != journal.KindDecision || e.Decision == nil || e.Decision.Allowed {
			continue
		}
		for _, v := range e.Decision.Violations {
			if v.Rule == "account_initial_margin" {
				n++
			}
		}
	}
	return n
}

// runHeader decodes the run-started record's manifest header.
func runHeader(t *testing.T, entries []journal.Entry) map[string]json.RawMessage {
	t.Helper()
	for _, e := range entries {
		if e.Kind == journal.KindRunStarted {
			var h map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(e.RunStarted.Header, &h))
			return h
		}
	}
	t.Fatal("no run-started record")
	return nil
}

func TestRunCLI_DefaultMarginRatioRejectsOverLimitEntry(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "run.jsonl")
	out, err := runReport(t, marginRunArgs(t, journalPath))
	require.NoError(t, err, "a margin rejection is a normal run outcome")

	assert.Empty(t, parseMarginReport(t, out).OpenTrades, "the ~1.1x entry must not open a position at the 1.0 default")
	entries := journalEntries(t, journalPath)
	assert.Equal(t, 1, marginRejections(entries), "the rejection is journaled with the account_initial_margin rule")

	h := runHeader(t, entries)
	assert.JSONEq(t, `[{"name":"account_initial_margin","version":"v1","parameters":{"initial_margin_ratio":"1"}}]`, string(h["risk_rules"]))
	assert.JSONEq(t, `{"name":"initial-margin-ratio","version":"v1","parameters":{"initial_margin_ratio":"1"}}`, string(h["margin_model"]))
}

func TestRunCLI_EntryWithinMarginLimitFills(t *testing.T) {
	// 0.9% risk sizes 9000 EUR at ~1.10, about 9900: within 10000.
	out, err := runReport(t, marginRunArgs(t, "", "--risk-fraction", "0.009"))
	require.NoError(t, err)
	assert.Len(t, parseMarginReport(t, out).OpenTrades, 1)
}

func TestRunCLI_ExplicitLeverageRatioFillsLeveragedEntry(t *testing.T) {
	// 0.25 permits up to 4x gross exposure, so the ~1.1x entry fills.
	// No finite ratio reproduces the old, unbounded simulator (ADR-066).
	journalPath := filepath.Join(t.TempDir(), "run.jsonl")
	out, err := runReport(t, marginRunArgs(t, journalPath, "--initial-margin-ratio", "0.25"))
	require.NoError(t, err)
	assert.Len(t, parseMarginReport(t, out).OpenTrades, 1)
	assert.Zero(t, marginRejections(journalEntries(t, journalPath)))
}

func TestRunCLI_MarginRatioChangesConfigDigest(t *testing.T) {
	// One shared data store: ConfigDigest embeds each dataset's BuiltAt
	// time (a known gap, ADR-042), so rebuilding the data per run would
	// change the digest by itself.
	store := t.TempDir()
	digest := func(ratio string) string {
		out, err := runReport(t, marginRunArgs(t, "", "--initial-margin-ratio", ratio, "--data-store-root", store))
		require.NoError(t, err)
		return parseMarginReport(t, out).Run.ConfigDigest
	}
	one, half := digest("1"), digest("0.5")
	assert.NotEmpty(t, one)
	assert.NotEqual(t, one, half)
	assert.Equal(t, one, digest("1"), "the same ratio reproduces the same digest")
}

func TestRunCLI_InvalidMarginRatioRejectedAtConfigLoad(t *testing.T) {
	for _, bad := range []string{"0", "-0.5", "abc"} {
		t.Run(bad, func(t *testing.T) {
			_, err := runReport(t, marginRunArgs(t, "", "--initial-margin-ratio", bad))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "margin")
		})
	}
	t.Run("config file", func(t *testing.T) {
		configPath := writeConfigFile(t, `
backtest:
  symbol: EURUSD
  interval: H1
  from: 2024-01-08T00:00:00Z
  to: 2024-01-08T04:00:00Z
  adverse_distance: 0.01000
  initial_margin_ratio: 0
`)
		_, err := runReport(t, []string{"run", "--config", configPath, "--data-raw-root", "testdata/raw/oanda", "--data-store-root", t.TempDir(), "--output-dir", t.TempDir()})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backtest.initial_margin_ratio must be positive")
	})
}

// TestRunCLI_ExternalStrategyEnforcesMarginRule proves the rule is
// installed for --strategy-exec guests too: every entry
// examples/sdk-minimal's flip-flop attempts (~1.1x equity) is rejected
// by account_initial_margin at the 1.0 default. Because flip-flop
// decides from its View, it never exits a position it doesn't hold, so
// the run completes normally (#427).
func TestRunCLI_ExternalStrategyEnforcesMarginRule(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "run.jsonl")
	out, err := runReport(t, marginRunArgs(t, journalPath, "--strategy-exec", flipFlopPath))
	require.NoError(t, err)
	assert.Empty(t, parseMarginReport(t, out).OpenTrades)
	assert.Positive(t, marginRejections(journalEntries(t, journalPath)), "the guest's entries were rejected by account_initial_margin")
}

// TestRunCLI_ReportsMarginSection covers #416 end to end: the JSON
// report's margin section for a run with a known admission rejection,
// and for a leveraged run that opens a position.
func TestRunCLI_ReportsMarginSection(t *testing.T) {
	t.Run("default ratio, rejected entry", func(t *testing.T) {
		out, err := runReport(t, marginRunArgs(t, ""))
		require.NoError(t, err)
		mg := parseMarginReport(t, out).Margin
		require.NotNil(t, mg.InitialMarginRatio)
		assert.Equal(t, "1", *mg.InitialMarginRatio)
		assert.Equal(t, 1, mg.Rejections)
		assert.Equal(t, 1, mg.AdmissionRejections)
		assert.Equal(t, 0, mg.FillRejections)
		require.NotNil(t, mg.PeakGrossNotional)
		assert.Equal(t, "0", mg.PeakGrossNotional.Amount, "never opened a position")
		require.NotNil(t, mg.PeakGrossLeverage)
		assert.Equal(t, "0", *mg.PeakGrossLeverage)
	})
	t.Run("leveraged run", func(t *testing.T) {
		out, err := runReport(t, marginRunArgs(t, "", "--initial-margin-ratio", "0.25"))
		require.NoError(t, err)
		mg := parseMarginReport(t, out).Margin
		assert.Equal(t, "0.25", *mg.InitialMarginRatio)
		assert.Zero(t, mg.Rejections)
		require.NotNil(t, mg.PeakGrossNotional)
		gross := num.MustParseMoney(mg.PeakGrossNotional.Amount, num.MustParseCurrency("USD"))
		assert.False(t, gross.IsZero())
		lev := num.MustParseRate(*mg.PeakGrossLeverage)
		assert.Equal(t, 1, lev.Cmp(num.MustParseRate("1")), "the ~1.1x entry shows leverage above 1: %s", lev)
		assert.Equal(t, -1, lev.Cmp(num.MustParseRate("1.2")), "and below 1.2: %s", lev)
	})
}
