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

	"github.com/rustyeddy/trader/provider/oanda"
	"github.com/rustyeddy/trader/service"
	"github.com/rustyeddy/trader/store"
	"github.com/rustyeddy/trader/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func statusArgs(t *testing.T, root, to string, extra ...string) []string {
	t.Helper()
	return append([]string{"data", "status", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD",
		"--interval", "h1", "--from", "2024-01-01", "--to", to}, extra...)
}

// buildMonths puts raw months in the store and builds them, so there is canonical
// data to look at.
func buildMonths(t *testing.T, root string, months ...time.Month) {
	t.Helper()
	for _, m := range months {
		putRawMonth(t, "EURUSD", oanda.RawH1, 2024, m, rawRec(utc(2024, m, 2, 0), true), rawRec(utc(2024, m, 2, 1), true))
	}
	_, _, err := runData(t, nil, buildArgs(t, root, "2024-04-01")...)
	require.NoError(t, err)
}

func TestDataStatus_ReportsEachMonthAndATotalWithoutAToken(t *testing.T) {
	root := storetest.Root(t)
	buildMonths(t, root, time.January, time.February)

	out, logs, err := runData(t, map[string]string{}, statusArgs(t, root, "2024-03-01")...)

	require.NoError(t, err, "nothing is fetched, so no token")
	assert.Contains(t, out, "EURUSD 2024-01: loaded, good 2")
	assert.Contains(t, out, "EURUSD 2024-02: loaded, good 2")
	assert.Contains(t, out, "EURUSD: 4 bars")
	assert.Contains(t, out, "total: loaded 2, bad 0, missing 0, stale 0; bars 4")
	assert.NotEmpty(t, logs)
}

func TestDataStatus_FailsOnAMissingMonthUnlessAllowed(t *testing.T) {
	root := storetest.Root(t)
	buildMonths(t, root, time.January)

	out, _, err := runData(t, nil, statusArgs(t, root, "2024-03-01")...)
	allowedOut, _, allowedErr := runData(t, nil, statusArgs(t, root, "2024-03-01", "--allow-missing")...)

	require.ErrorIs(t, err, service.ErrNotLoadable)
	assert.Contains(t, out, "EURUSD 2024-02: missing")
	assert.Contains(t, out, "EURUSD: not loadable")
	assert.Contains(t, out, "total: loaded 1, bad 0, missing 1, stale 0; bars 0", "a refused instrument contributes no bars")
	require.NoError(t, allowedErr)
	assert.Contains(t, allowedOut, "EURUSD: 2 bars")
	assert.Contains(t, allowedOut, "EURUSD 2024-02: missing")
}

func TestDataStatus_ABadMonthIsReportedAndFailsUnlessAllowed(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.January, rawRec(utc(2024, 1, 2, 0), true), rawRec(utc(2024, 1, 2, 1), false))
	_, _, err := runData(t, nil, buildArgs(t, root, "2024-02-01")...)
	require.NoError(t, err)

	out, _, strictErr := runData(t, nil, statusArgs(t, root, "2024-02-01")...)
	allowedOut, _, allowedErr := runData(t, nil, statusArgs(t, root, "2024-02-01", "--allow-bad")...)

	require.ErrorIs(t, strictErr, service.ErrNotLoadable)
	assert.Contains(t, out, "EURUSD 2024-01: bad, good 1, bad 1")
	require.NoError(t, allowedErr)
	assert.Contains(t, allowedOut, "EURUSD 2024-01: bad, good 1, bad 1", "still reported as bad when allowed")
	assert.Contains(t, allowedOut, "EURUSD: 1 bars")
}

func TestDataStatus_AStaleMonthIsNeverAccepted(t *testing.T) {
	root := storetest.Root(t)
	path := "oanda/canonical/EURUSD/2024/01/EURUSD-2024-01-h1.csv"
	require.NoError(t, store.Write(context.Background(), path, []byte("# schema=canonical-v1\nold,format\n")))

	out, _, err := runData(t, nil, statusArgs(t, root, "2024-02-01", "--allow-bad", "--allow-missing")...)

	require.ErrorIs(t, err, service.ErrNotLoadable)
	assert.Contains(t, out, "EURUSD 2024-01: stale: not readable as canonical-v2")
	assert.Contains(t, out, "stale 1")
}

func TestDataStatus_WritesNothing(t *testing.T) {
	root := storetest.Root(t)
	buildMonths(t, root, time.January)
	before, err := store.List("")
	require.NoError(t, err)

	_, _, err = runData(t, nil, statusArgs(t, root, "2024-03-01", "--allow-missing")...)

	require.NoError(t, err)
	after, err := store.List("")
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestDataStatus_WithNoConfiguredRootUsesTheStoresRoot(t *testing.T) {
	root := storetest.Root(t)
	buildMonths(t, root, time.January)

	out, _, err := runData(t, nil, "data", "status", "--config", writeConfig(t, ""), "--instrument", "EURUSD",
		"--interval", "h1", "--from", "2024-01-01", "--to", "2024-02-01")

	require.NoError(t, err)
	assert.Contains(t, out, "EURUSD 2024-01: loaded")
	assert.True(t, strings.HasPrefix(root, "/"), root)
	assert.FileExists(t, filepath.Join(root, "oanda", "canonical", "EURUSD", "2024", "01", "EURUSD-2024-01-h1.csv"))
}

func TestDataStatus_RejectsBadOptionsBeforeDoingAnything(t *testing.T) {
	root := storetest.Root(t)
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"no instrument": {[]string{"data", "status", "--config", writeConfig(t, ""), "--data-root", root, "--interval", "h1", "--from", "2024-01-01", "--to", "2024-02-01"}, "no instrument"},
		"no interval":   {[]string{"data", "status", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD", "--from", "2024-01-01", "--to", "2024-02-01"}, "no interval"},
		"no range":      {[]string{"data", "status", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD", "--interval", "h1"}, "--from and --to"},
		"reversed":      {statusArgs(t, root, "2023-12-01"), "--to must be after --from"},
	} {
		t.Run(name, func(t *testing.T) {
			out, _, err := runData(t, nil, tc.args...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, out, "total:", "nothing ran")
		})
	}
}

// TestStatusCommand_IsTransportOnly keeps the command thin, as build's: status.go
// imports the application service, the store and logging, never a provider or a
// market-data package.
func TestStatusCommand_IsTransportOnly(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod + "/service": true, mod + "/store": true, mod + "/internal/logging": true}
	f, err := parser.ParseFile(token.NewFileSet(), "status.go", nil, parser.ImportsOnly)
	require.NoError(t, err)
	for _, imp := range f.Imports {
		path, uerr := strconv.Unquote(imp.Path.Value)
		require.NoError(t, uerr)
		if path == mod || strings.HasPrefix(path, mod+"/") {
			assert.True(t, allowed[path], "status.go imports %s: the command may import only the application service, the store and logging", path)
		}
	}
}
