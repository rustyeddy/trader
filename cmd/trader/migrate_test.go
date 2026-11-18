package main

import (
	"go/parser"
	"go/token"
	"os"
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

// oldLayout lays out raw for two sources and a canonical-v1 directory in the old
// layout under a fresh root.
func oldLayout(t *testing.T) string {
	t.Helper()
	root := storetest.Root(t)
	for rel, content := range map[string]string{
		"raw/oanda/EURUSD/2024/01/EURUSD-2024-01-h1.csv":       "raw one\n",
		"raw/oanda/EURUSD/2024/02/EURUSD-2024-02-h1.csv":       "raw two\n",
		"raw/alpaca/SPY/2024/01/SPY-2024-01-d1.csv":            "raw three\n",
		"canonical/oanda/EURUSD/2024/01/EURUSD-2024-01-h1.csv": "# schema=canonical-v1\n",
	} {
		abs := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
	}
	return root
}

func migrateArgs(t *testing.T, root string, extra ...string) []string {
	t.Helper()
	return append([]string{"data", "migrate", "--config", writeConfig(t, ""), "--data-root", root}, extra...)
}

func TestDataMigrate_IsADryRunUnlessApplied(t *testing.T) {
	root := oldLayout(t)
	before, err := store.List("")
	require.NoError(t, err)

	out, _, err := runData(t, nil, migrateArgs(t, root)...)

	require.NoError(t, err)
	assert.Contains(t, out, "data root: "+root+"\n")
	assert.Contains(t, out, "dry run: nothing is moved; run again with --apply to move\n")
	assert.Contains(t, out, filepath.Join("raw", "oanda")+" -> "+filepath.Join("oanda", "raw")+": 2 files, 16 B [planned]\n")
	assert.Contains(t, out, "legacy canonical (canonical-v1, not moved; rebuild with trader data build, delete when trader data audit passes): oanda\n")
	assert.Contains(t, out, "total: 2 sources, 3 files, 26 B\n")
	assert.True(t, strings.HasSuffix(out, "migrate: OK\n"))
	after, err := store.List("")
	require.NoError(t, err)
	assert.Equal(t, before, after, "nothing moved")
}

func TestDataMigrate_ApplyMovesRawAndReverseMovesItBack(t *testing.T) {
	root := oldLayout(t)
	before, err := store.List("")
	require.NoError(t, err)

	out, _, err := runData(t, nil, migrateArgs(t, root, "--apply", "--checksum")...)

	require.NoError(t, err)
	assert.Contains(t, out, "applying\n")
	assert.Contains(t, out, "[verified]\n")
	assert.FileExists(t, filepath.Join(root, "oanda/raw/EURUSD/2024/01/EURUSD-2024-01-h1.csv"))
	assert.FileExists(t, filepath.Join(root, "canonical/oanda/EURUSD/2024/01/EURUSD-2024-01-h1.csv"), "canonical stays")
	assert.NoDirExists(t, filepath.Join(root, "raw/oanda"))

	out, _, err = runData(t, nil, migrateArgs(t, root, "--apply", "--reverse")...)

	require.NoError(t, err)
	assert.Contains(t, out, "applying (reverse)\n")
	after, lerr := store.List("")
	require.NoError(t, lerr)
	assert.Equal(t, before, after, "back as it was")
}

func TestDataMigrate_ARefusedPlanPrintsTheProblemAndFails(t *testing.T) {
	root := oldLayout(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "alpaca/raw"), 0o755))

	out, _, err := runData(t, nil, migrateArgs(t, root, "--apply")...)

	require.ErrorIs(t, err, service.ErrMigrationFailed)
	assert.Contains(t, out, "  PROBLEM: the destination "+filepath.Join("alpaca", "raw")+" exists")
	assert.True(t, strings.HasSuffix(out, "migrate: FAILED\n"))
	assert.FileExists(t, filepath.Join(root, "raw/oanda/EURUSD/2024/01/EURUSD-2024-01-h1.csv"), "nothing moved")
}

func TestDataMigrate_NothingToMigrate(t *testing.T) {
	root := storetest.Root(t)

	out, _, err := runData(t, nil, migrateArgs(t, root, "--apply")...)

	require.NoError(t, err)
	assert.Contains(t, out, "nothing to migrate\n")
	assert.True(t, strings.HasSuffix(out, "migrate: OK\n"))
}

func TestDataMigrate_ChecksumNeedsApplyAndPrintsNoReport(t *testing.T) {
	root := oldLayout(t)

	out, _, err := runData(t, nil, migrateArgs(t, root, "--checksum")...)

	assert.ErrorContains(t, err, "--checksum needs --apply")
	assert.NotErrorIs(t, err, service.ErrMigrationFailed)
	assert.Empty(t, out)
}

func TestDataMigrate_TheRootFollowsTheUsualPrecedence(t *testing.T) {
	root := oldLayout(t)

	out, _, err := runData(t, map[string]string{envDataRoot: root}, "data", "migrate", "--config", writeConfig(t, ""))

	require.NoError(t, err)
	assert.Contains(t, out, "data root: "+root+"\n")
}

func TestHumanBytes(t *testing.T) {
	assert.Equal(t, "0 B", humanBytes(0))
	assert.Equal(t, "1023 B", humanBytes(1023))
	assert.Equal(t, "1.0 KiB", humanBytes(1024))
	assert.Equal(t, "1.5 KiB", humanBytes(1536))
	assert.Equal(t, "147.0 MiB", humanBytes(147<<20))
	assert.Equal(t, "30.0 GiB", humanBytes(30<<30))
}

func TestMigrateCommand_IsTransportOnly(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod + "/service": true, mod + "/store": true, mod + "/internal/logging": true}
	f, err := parser.ParseFile(token.NewFileSet(), "migrate.go", nil, parser.ImportsOnly)
	require.NoError(t, err)
	for _, imp := range f.Imports {
		path, uerr := strconv.Unquote(imp.Path.Value)
		require.NoError(t, uerr)
		if path == mod || strings.HasPrefix(path, mod+"/") {
			assert.True(t, allowed[path], "migrate.go imports %s", path)
		}
	}
}

// TestDataMigrate_MigratedRawIsWhatBuildReads proves the target of the move is the
// layout the rest of the code reads: raw put in the old place is invisible to
// build, and after the migration build converts it and audit verifies it.
func TestDataMigrate_MigratedRawIsWhatBuildReads(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.January, rawRec(utc(2024, 1, 9, 0), true), rawRec(utc(2024, 1, 9, 1), true))
	_, _, err := runData(t, nil, migrateArgs(t, root, "--apply", "--reverse")...) // into the old layout
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, "raw/oanda/EURUSD/2024/01/EURUSD-2024-01-h1.csv"))

	old, _, err := runData(t, nil, buildArgs(t, root, "2024-02-01")...)
	require.NoError(t, err)
	assert.Contains(t, old, "no raw partition", "the old layout is not read: no silent fallback")

	_, _, err = runData(t, nil, migrateArgs(t, root, "--apply", "--checksum")...)
	require.NoError(t, err)
	built, _, err := runData(t, nil, buildArgs(t, root, "2024-02-01")...)
	require.NoError(t, err)
	assert.Contains(t, built, "built, good 2")
	audited, _, err := runData(t, nil, auditArgs(t, root, "2024-02-01")...)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(audited, "audit: OK\n"), audited)
}
