package main

import (
	"context"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/provider/oanda"
	"github.com/rustyeddy/trader/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func verifyArgs(t *testing.T, root, to string, extra ...string) []string {
	t.Helper()
	return append([]string{"data", "verify", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD",
		"--interval", "h1", "--from", "2024-01-01", "--to", to}, extra...)
}

func TestDataVerify_ACleanBuildVerifiesAndCountsTheExpectedDifferences(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.January,
		rawRec(utc(2024, 1, 9, 0), true), rawRec(utc(2024, 1, 9, 1), true), rawRec(utc(2024, 1, 9, 2), false))
	_, _, err := runData(t, nil, buildArgs(t, root, "2024-02-01")...)
	require.NoError(t, err)

	out, _, err := runData(t, map[string]string{}, verifyArgs(t, root, "2024-02-01")...)

	require.NoError(t, err, "nothing is fetched, so no token")
	assert.Contains(t, out, filepath.Join(root, "oanda", "canonical", "EURUSD", "2024", "01")+"/EURUSD-2024-01-h1.csv: verified, raw rows 3, bars 2, bad 1 (explained: incomplete 1)")
	assert.Contains(t, out, "total: verified 1, failed 0, no raw 0, no canonical 0; raw rows 3, findings 0 (explained: incomplete 1)")
}

func TestDataVerify_ADisagreementIsPrintedAndFailsTheCommand(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.January, rawRec(utc(2024, 1, 9, 0), true), rawRec(utc(2024, 1, 9, 1), true))
	_, _, err := runData(t, nil, buildArgs(t, root, "2024-02-01")...)
	require.NoError(t, err)
	key := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.January}
	require.NoError(t, oanda.WritePartition(context.Background(), key,
		[]oanda.Record{rawRec(utc(2024, 1, 9, 0), true), rawRec(utc(2024, 1, 9, 1), true), rawRec(utc(2024, 1, 9, 2), true)}, false))

	out, _, err := runData(t, nil, verifyArgs(t, root, "2024-02-01")...)

	require.ErrorIs(t, err, service.ErrVerifyFailed)
	assert.Contains(t, out, ": FAILED, raw rows 3, bars 2, bad 0, 2 findings")
	assert.Contains(t, out, "  fingerprint: built from ")
	assert.Contains(t, out, "  unaccounted 2024-01-09T02:00Z row 3: the raw row is neither a bar nor a bad record")
	assert.Contains(t, out, "total: verified 0, failed 1, no raw 0, no canonical 0; raw rows 3, findings 2")
}

func TestDataVerify_MissingRawAndMissingCanonicalAreReportedNotFailed(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.January, rawRec(utc(2024, 1, 9, 0), true))

	out, _, err := runData(t, nil, verifyArgs(t, root, "2024-03-01")...)

	require.NoError(t, err)
	assert.Contains(t, out, "EURUSD 2024-01: raw rows 1, no canonical file")
	assert.Contains(t, out, "EURUSD 2024-02: no raw partition")
	assert.Contains(t, out, "no raw 1, no canonical 1")
}

func TestDataVerify_SampleChecksFewerMonths(t *testing.T) {
	root := storetest.Root(t)
	for _, m := range []time.Month{time.January, time.February, time.March} {
		putRawMonth(t, "EURUSD", oanda.RawH1, 2024, m, rawRec(utc(2024, m, 9, 0), true))
	}
	_, _, err := runData(t, nil, buildArgs(t, root, "2024-04-01")...)
	require.NoError(t, err)

	out, _, err := runData(t, nil, verifyArgs(t, root, "2024-04-01", "--sample", "2")...)

	require.NoError(t, err)
	assert.Contains(t, out, "EURUSD-2024-01-h1.csv: verified")
	assert.NotContains(t, out, "EURUSD-2024-02-h1.csv")
	assert.Contains(t, out, "EURUSD-2024-03-h1.csv: verified")
}

func TestDataVerify_RefusesWhatCannotRunAndWritesNothing(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.January, rawRec(utc(2024, 1, 9, 0), true))
	before, err := store.List("")
	require.NoError(t, err)

	_, _, negative := runData(t, nil, verifyArgs(t, root, "2024-02-01", "--sample", "-1")...)
	_, _, reversed := runData(t, nil, append(verifyArgs(t, root, "2023-01-01"), "--sample", "0")...)

	assert.Error(t, negative)
	assert.Error(t, reversed)
	after, err := store.List("")
	require.NoError(t, err)
	assert.Equal(t, before, after, "verify only reads")
}

func TestExplainedText(t *testing.T) {
	assert.Equal(t, "", explainedText(nil))
	assert.Equal(t, " (explained: crossed 1, incomplete 3)", explainedText(map[string]int{"incomplete": 3, "crossed": 1}), "sorted by reason")
}

// TestVerifyCommand_IsTransportOnly keeps verify.go thin, as build.go and
// status.go are: the application service, the store and logging only.
func TestVerifyCommand_IsTransportOnly(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod + "/service": true, mod + "/internal/store": true, mod + "/internal/logging": true}
	f, err := parser.ParseFile(token.NewFileSet(), "verify.go", nil, parser.ImportsOnly)
	require.NoError(t, err)
	for _, imp := range f.Imports {
		path, uerr := strconv.Unquote(imp.Path.Value)
		require.NoError(t, uerr)
		if path == mod || strings.HasPrefix(path, mod+"/") {
			assert.True(t, allowed[path], "verify.go imports %s", path)
		}
	}
}
