package review

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/review"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_UseName(t *testing.T) {
	cmd := New(nil)
	assert.Equal(t, "review", cmd.Use)
}

func TestNew_HasExpectedFlags(t *testing.T) {
	cmd := New(nil)
	for _, name := range []string{"instruments", "watch", "hotlist", "tradeable", "output", "token", "env", "asof", "from", "to", "interval"} {
		assert.NotNil(t, cmd.Flags().Lookup(name), "missing --%s flag", name)
	}
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "EURUSD", []string{"EURUSD"}},
		{"multiple", "EURUSD,GBPUSD,USDJPY", []string{"EURUSD", "GBPUSD", "USDJPY"}},
		{"whitespace and blanks trimmed", " EURUSD , ,GBPUSD ", []string{"EURUSD", "GBPUSD"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, splitCSV(tt.in))
		})
	}
}

func TestSortByBucket_OrdersTradeableThenHotThenWatch(t *testing.T) {
	results := []review.ReviewResult{
		{Instrument: "AUDUSD", Bucket: "watch"},
		{Instrument: "USDJPY", Bucket: "hot"},
		{Instrument: "EURUSD", Bucket: "tradeable"},
		{Instrument: "GBPJPY", Bucket: "hot"},
		{Instrument: "NZDUSD", Bucket: "tradeable"},
	}
	sortByBucket(results)

	got := make([]string, len(results))
	for i, r := range results {
		got[i] = r.Instrument
	}
	// Tradeable pairs first (stable order), then hot, then watch.
	assert.Equal(t, []string{"EURUSD", "NZDUSD", "USDJPY", "GBPJPY", "AUDUSD"}, got)
}

func TestValidateOutputFormat(t *testing.T) {
	for _, ok := range []string{"table", "json", "org", "csv"} {
		assert.NoError(t, validateOutputFormat(ok))
	}
	err := validateOutputFormat("xml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --output")
}

func resetCategoryFlags(t *testing.T) {
	t.Helper()
	showWatch, showHotlist, showTradeable = false, false, false
}

func TestSelectedBuckets_DefaultsToAllThree(t *testing.T) {
	resetCategoryFlags(t)
	assert.Equal(t, map[string]bool{"watch": true, "hot": true, "tradeable": true}, selectedBuckets())
}

func TestSelectedBuckets_HonorsExplicitSelection(t *testing.T) {
	resetCategoryFlags(t)
	showHotlist = true
	defer resetCategoryFlags(t)
	assert.Equal(t, map[string]bool{"watch": false, "hot": true, "tradeable": false}, selectedBuckets())
}

func TestRunReview_InvalidOutputReturnsError(t *testing.T) {
	// Output validation happens before buildService/OANDA access, so this
	// stays offline even though runReview otherwise talks to OANDA.
	cmd := New(nil)
	require.NoError(t, cmd.Flags().Set("output", "bogus"))
	err := runReview(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --output")
}

func TestRenderJSON(t *testing.T) {
	results := []review.ReviewResult{
		{Instrument: "EURUSD", Bucket: "tradeable", Bias: "long"},
	}
	var buf bytes.Buffer
	require.NoError(t, renderJSON(&buf, results))

	var got []review.ReviewResult
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "EURUSD", got[0].Instrument)
	assert.Equal(t, "tradeable", got[0].Bucket)
}

func TestRenderOrg_EmitsHlineBetweenBucketGroups(t *testing.T) {
	results := []review.ReviewResult{
		{Instrument: "EURUSD", Bucket: "tradeable", Bias: "long"},
		{Instrument: "USDJPY", Bucket: "hot", Bias: "short"},
	}
	var buf bytes.Buffer
	require.NoError(t, renderOrg(&buf, results))

	out := buf.String()
	assert.Contains(t, out, "| PAIR | BUCKET |")
	assert.Contains(t, out, "| EURUSD | tradeable | long")
	assert.Contains(t, out, "| USDJPY | hot | short")
	// One hline after the header, one between the two bucket groups.
	assert.Equal(t, 2, strings.Count(out, "|-\n"))
}

func TestRenderOrg_EmptyResults(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, renderOrg(&buf, nil))
	assert.Equal(t, "No results.\n", buf.String())
}

func TestRenderTable_EmptyResults(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, renderTable(&buf, nil))
	assert.Equal(t, "No results.\n", buf.String())
}

func TestParseHistoricalRange_NoFlagsIsLive(t *testing.T) {
	cmd := New(nil)
	from, to, historical, err := parseHistoricalRange(cmd)
	require.NoError(t, err)
	assert.False(t, historical)
	assert.True(t, from.IsZero())
	assert.True(t, to.IsZero())
}

func TestParseHistoricalRange_AsOfSetsFromEqualsTo(t *testing.T) {
	cmd := New(nil)
	require.NoError(t, cmd.Flags().Set("asof", "2026-06-15"))

	from, to, historical, err := parseHistoricalRange(cmd)
	require.NoError(t, err)
	assert.True(t, historical)
	assert.True(t, from.Equal(to))
	assert.Equal(t, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC), from)
}

func TestParseHistoricalRange_AsOfCombinedWithFromToErrors(t *testing.T) {
	cmd := New(nil)
	require.NoError(t, cmd.Flags().Set("asof", "2026-06-15"))
	require.NoError(t, cmd.Flags().Set("from", "2026-06-01"))
	require.NoError(t, cmd.Flags().Set("to", "2026-06-15"))

	_, _, _, err := parseHistoricalRange(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be combined")
}

func TestParseHistoricalRange_FromWithoutToErrors(t *testing.T) {
	cmd := New(nil)
	require.NoError(t, cmd.Flags().Set("from", "2026-06-01"))

	_, _, _, err := parseHistoricalRange(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be set together")
}

func TestParseHistoricalRange_ToBeforeFromErrors(t *testing.T) {
	cmd := New(nil)
	require.NoError(t, cmd.Flags().Set("from", "2026-06-15"))
	require.NoError(t, cmd.Flags().Set("to", "2026-06-01"))

	_, _, _, err := parseHistoricalRange(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be before")
}

func TestParseHistoricalRange_InvalidDateErrors(t *testing.T) {
	cmd := New(nil)
	require.NoError(t, cmd.Flags().Set("asof", "not-a-date"))

	_, _, _, err := parseHistoricalRange(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --asof")
}

func TestRunReview_MultiStepRejectsTableOutput(t *testing.T) {
	// output validation happens before buildService/OANDA access, so this
	// stays offline even though runReview otherwise talks to OANDA.
	cmd := New(nil)
	require.NoError(t, cmd.Flags().Set("from", "2026-06-01"))
	require.NoError(t, cmd.Flags().Set("to", "2026-06-15"))

	err := runReview(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multi-date sweep")
}

// TestRenderResults_SingleDateReachesEveryFormat is a regression test: a
// single-date result set (multiStep == false, e.g. a bare --asof run) must
// reach every renderer named in --output's help text, including csv. Before
// this fix, renderResults' single-date branch only special-cased "json"
// and "org" and fell through to renderTable for anything else — so
// `--asof <date> --output csv` silently produced a table instead of CSV.
func TestRenderResults_SingleDateReachesEveryFormat(t *testing.T) {
	scannedAt := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	results := []review.ReviewResult{
		{Instrument: "EURUSD", Bucket: "tradeable", Bias: "long", ScannedAt: scannedAt},
	}

	for _, format := range []string{"table", "json", "org", "csv"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, renderResults(&buf, format, append([]review.ReviewResult{}, results...), false))

			out := buf.String()
			require.NotEmpty(t, out)
			switch format {
			case "csv":
				assert.True(t, strings.HasPrefix(out, "DATE,PAIR,BUCKET"), "csv output must be CSV, not a table: %q", out)
			case "table":
				assert.Contains(t, out, "PAIR")
				assert.NotContains(t, out, "DATE,")
			}
		})
	}
}

func TestSortByInstrumentThenDate(t *testing.T) {
	day1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	results := []review.ReviewResult{
		{Instrument: "USDJPY", ScannedAt: day2},
		{Instrument: "EURUSD", ScannedAt: day2},
		{Instrument: "USDJPY", ScannedAt: day1},
		{Instrument: "EURUSD", ScannedAt: day1},
	}
	sortByInstrumentThenDate(results)

	var got []string
	for _, r := range results {
		got = append(got, r.Instrument+"@"+r.ScannedAt.Format("2006-01-02"))
	}
	assert.Equal(t, []string{"EURUSD@2026-06-01", "EURUSD@2026-06-02", "USDJPY@2026-06-01", "USDJPY@2026-06-02"}, got)
}

func TestRenderCSV(t *testing.T) {
	scannedAt := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	results := []review.ReviewResult{
		{Instrument: "EURUSD", Bucket: "tradeable", Bias: "long", ScannedAt: scannedAt},
	}
	var buf bytes.Buffer
	require.NoError(t, renderCSV(&buf, results))

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 2, "header + 1 data row")
	assert.True(t, strings.HasPrefix(lines[0], "DATE,PAIR,BUCKET"))
	assert.True(t, strings.HasPrefix(lines[1], scannedAt.Format(time.RFC3339)+",EURUSD,tradeable,long"))
}
