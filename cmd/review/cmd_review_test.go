package review

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

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
	for _, name := range []string{"instruments", "watch", "hotlist", "tradeable", "output", "token", "env"} {
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
	for _, ok := range []string{"table", "json", "org"} {
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
