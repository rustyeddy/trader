package main

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
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

func auditArgs(t *testing.T, root, to string, extra ...string) []string {
	t.Helper()
	return append([]string{"data", "audit", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD",
		"--interval", "h1", "--from", "2024-01-01", "--to", to}, extra...)
}

func TestDataAudit_SoundDataPrintsTextAndExitsZeroWithoutAToken(t *testing.T) {
	root := storetest.Root(t)
	buildMonths(t, root, time.January)

	out, _, err := runData(t, map[string]string{}, auditArgs(t, root, "2024-02-01")...)

	require.NoError(t, err)
	assert.Contains(t, out, "EURUSD\n")
	assert.Contains(t, out, "  2024-01: loaded, good 2\n")
	assert.Contains(t, out, "  raw 2024-01: verified, rows 2, bars 2, bad 0\n")
	assert.Contains(t, out, "  OK: 2 bars\n")
	assert.Contains(t, out, "total: months 1 (loaded 1, bad 0, missing 0, stale 0); bars 2; gaps 0 unexplained, 0 known closures; raw verified 1")
	assert.True(t, strings.HasSuffix(out, "audit: OK\n"))
}

func TestDataAudit_AGapAndADisagreementFailTheCommandAndAreShown(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.January,
		rawRec(utc(2024, 1, 9, 0), true), rawRec(utc(2024, 1, 9, 1), true), rawRec(utc(2024, 1, 9, 5), true))
	_, _, err := runData(t, nil, buildArgs(t, root, "2024-02-01")...)
	require.NoError(t, err)
	key := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.January}
	require.NoError(t, oanda.WritePartition(context.Background(), key,
		[]oanda.Record{rawRec(utc(2024, 1, 9, 0), true), rawRec(utc(2024, 1, 9, 1), true), rawRec(utc(2024, 1, 9, 5), true), rawRec(utc(2024, 1, 9, 6), true)}, false))

	out, _, err := runData(t, nil, auditArgs(t, root, "2024-02-01")...)

	require.ErrorIs(t, err, service.ErrAuditFailed)
	assert.Contains(t, out, "  gap unexplained 2024-01-09T02:00:00Z 2024-01-09T05:00:00Z, 3 bars\n")
	assert.Contains(t, out, "  raw 2024-01: FAILED, rows 4, bars 3, bad 0, 2 findings\n")
	assert.Contains(t, out, "    unaccounted: 2024-01-09T06:00Z row 4: the raw row is neither a bar nor a bad record\n")
	assert.Contains(t, out, "  not loadable: ")
	assert.True(t, strings.HasSuffix(out, "audit: FAILED\n"))
}

func TestDataAudit_AllowMissingAcceptsTheGap(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.January,
		rawRec(utc(2024, 1, 9, 0), true), rawRec(utc(2024, 1, 9, 1), true), rawRec(utc(2024, 1, 9, 5), true))
	_, _, err := runData(t, nil, buildArgs(t, root, "2024-02-01")...)
	require.NoError(t, err)

	out, _, err := runData(t, nil, auditArgs(t, root, "2024-02-01", "--allow-missing")...)

	require.NoError(t, err)
	assert.Contains(t, out, "  gap unexplained ", "an accepted gap is still shown")
	assert.True(t, strings.HasSuffix(out, "audit: OK\n"))
}

func TestDataAudit_JSONIsOneObjectForTools(t *testing.T) {
	root := storetest.Root(t)
	buildMonths(t, root, time.January)

	out, _, err := runData(t, nil, auditArgs(t, root, "2024-02-01", "--format", "json")...)

	require.NoError(t, err)
	var got service.AuditReport
	require.NoError(t, json.Unmarshal([]byte(out), &got), "the output is exactly one JSON document")
	assert.True(t, got.OK)
	assert.Equal(t, "EURUSD", got.Instruments[0].Instrument)
	assert.Equal(t, 1, got.Summary.RawVerified)
}

func TestDataAudit_JSONIsStillPrintedWhenTheAuditFails(t *testing.T) {
	root := storetest.Root(t)
	buildMonths(t, root, time.January)

	out, _, err := runData(t, nil, auditArgs(t, root, "2024-03-01", "--format", "json")...) // February is missing

	require.ErrorIs(t, err, service.ErrAuditFailed, "a non-zero exit")
	var got service.AuditReport
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.False(t, got.OK)
	assert.Equal(t, 1, got.Summary.MonthsMissing)
}

func TestDataAudit_RefusesWhatCannotRunAndWritesNothing(t *testing.T) {
	root := storetest.Root(t)
	buildMonths(t, root, time.January)
	before, err := store.List("")
	require.NoError(t, err)

	_, _, badFormat := runData(t, nil, auditArgs(t, root, "2024-02-01", "--format", "yaml")...)
	_, _, negative := runData(t, nil, auditArgs(t, root, "2024-02-01", "--sample", "-1")...)
	_, _, reversed := runData(t, nil, auditArgs(t, root, "2023-01-01")...)

	assert.ErrorContains(t, badFormat, "--format")
	assert.Error(t, negative)
	assert.Error(t, reversed)
	assert.NotErrorIs(t, badFormat, service.ErrAuditFailed)
	after, err := store.List("")
	require.NoError(t, err)
	assert.Equal(t, before, after, "audit only reads")
}

func TestAuditCommand_IsTransportOnly(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod + "/service": true, mod + "/store": true, mod + "/internal/logging": true}
	f, err := parser.ParseFile(token.NewFileSet(), "audit.go", nil, parser.ImportsOnly)
	require.NoError(t, err)
	for _, imp := range f.Imports {
		path, uerr := strconv.Unquote(imp.Path.Value)
		require.NoError(t, uerr)
		if path == mod || strings.HasPrefix(path, mod+"/") {
			assert.True(t, allowed[path], "audit.go imports %s", path)
		}
	}
}
