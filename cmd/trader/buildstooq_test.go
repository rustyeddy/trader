package main

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
)

const stooqInput = "stooq/raw/spy.us.txt"

func putStooqInput(t *testing.T, rows string) {
	t.Helper()
	header := "<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\n"
	require.NoError(t, store.Write(context.Background(), stooqInput, []byte(header+rows)))
}

// stooqArgs is a build with a config file that describes the OANDA data, which
// the Stooq build must not take its instrument or interval from.
func stooqArgs(t *testing.T, root string, extra ...string) []string {
	t.Helper()
	config := writeConfig(t, "marketdata:\n  instruments: [EURUSD]\n  interval: h1\n")
	return append([]string{"data", "build", "--config", config, "--data-root", root, "--source", "stooq"}, extra...)
}

func TestDataBuildStooq_BuildsAndPrintsTheSnapshotAndTotals(t *testing.T) {
	root := storetest.Root(t)
	putStooqInput(t, "SPY.US,D,20240301,000000,10,11,9,10.5,100,0\n")
	args := stooqArgs(t, root, "--instrument", "spy", "--input", stooqInput, "--snapshot-as-of", "2026-09-10")

	out, _, err := runData(t, map[string]string{}, args...)

	require.NoError(t, err)
	assert.Contains(t, out, filepath.Join(root, "stooq/canonical/SPY/2024/03/SPY-2024-03-d1.csv")+": built, good 1, bad 0")
	assert.Contains(t, out, "snapshot: sha256:")
	assert.Contains(t, out, "as of 2026-09-10, archived at "+filepath.Join(root, "stooq/snapshots/SPY"))
	assert.Contains(t, out, "total: built 1, unchanged 0, replaced 0; good bars 1, bad records 0")
	again, _, err := runData(t, nil, args...)
	require.NoError(t, err)
	assert.Contains(t, again, "unchanged 1")
}

func TestDataBuildStooq_BadRecordsArePublishedAndFailTheCommand(t *testing.T) {
	root := storetest.Root(t)
	putStooqInput(t, "SPY.US,D,20240301,000000,10,11,9,10.5,100,0\nSPY.US,D,20240302,000000,10,11,9,10.5,100,0\n")

	out, _, err := runData(t, nil, stooqArgs(t, root, "--instrument", "SPY", "--input", stooqInput, "--snapshot-as-of", "2026-09-10")...)

	assert.ErrorIs(t, err, errBadRecords)
	assert.Contains(t, out, "good 1, bad 1")
}

func TestDataBuildStooq_Refusals(t *testing.T) {
	root := storetest.Root(t)
	putStooqInput(t, "SPY.US,D,20240301,000000,10,11,9,10.5,100,0\n")
	full := []string{"--instrument", "SPY", "--input", stooqInput, "--snapshot-as-of", "2026-09-10"}
	with := func(extra ...string) []string {
		return stooqArgs(t, root, append(append([]string{}, full...), extra...)...)
	}
	for want, args := range map[string][]string{
		"--from and --to do not apply":      with("--from", "2024-01-01"),
		"exactly one --instrument":          stooqArgs(t, root, "--input", stooqInput),
		"needs --input":                     stooqArgs(t, root, "--instrument", "SPY"),
		"only SPY and QQQ d1":               with("--interval", "h1"),
		"is not a ZIP":                      with("--member", "x"),
		"is not oanda or stooq":             {"data", "build", "--source", "alpaca"},
		"--input applies only to --source ": {"data", "build", "--input", "x"},
	} {
		t.Run(want, func(t *testing.T) {
			_, _, err := runData(t, nil, args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), want)
		})
	}
	_, err := store.List("stooq/canonical")
	assert.ErrorIs(t, err, fs.ErrNotExist, "no refusal wrote anything")
}
