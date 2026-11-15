package main

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/provider/oanda"
	"github.com/rustyeddy/trader/service"
	"github.com/rustyeddy/trader/store"
	"github.com/rustyeddy/trader/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rawRec(t time.Time, complete bool) oanda.Record {
	return oanda.Record{
		Time:    t,
		BidOpen: num.MustParsePrice("1.10000"), BidHigh: num.MustParsePrice("1.10100"),
		BidLow: num.MustParsePrice("1.09900"), BidClose: num.MustParsePrice("1.10050"),
		AskOpen: num.MustParsePrice("1.10010"), AskHigh: num.MustParsePrice("1.10110"),
		AskLow: num.MustParsePrice("1.09910"), AskClose: num.MustParsePrice("1.10060"),
		Volume: 100, Complete: complete,
	}
}

func putRawMonth(t *testing.T, symbol string, interval oanda.RawInterval, year int, month time.Month, records ...oanda.Record) {
	t.Helper()
	key := oanda.PartitionKey{Symbol: symbol, Interval: interval, Year: year, Month: month}
	require.NoError(t, oanda.WritePartition(context.Background(), key, records, true))
}

func buildArgs(t *testing.T, root, to string) []string {
	t.Helper()
	return []string{"data", "build", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD",
		"--interval", "h1", "--from", "2024-01-01", "--to", to}
}

func TestDataBuild_BuildsCanonicalFilesAndPrintsASummaryWithoutAToken(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.January, rawRec(utc(2024, 1, 2, 0), true), rawRec(utc(2024, 1, 2, 1), true))
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.February, rawRec(utc(2024, 2, 1, 0), true))

	out, logs, err := runData(t, map[string]string{}, buildArgs(t, root, "2024-03-01")...)

	require.NoError(t, err, "no token is needed: nothing is fetched")
	jan := filepath.Join(root, "oanda", "canonical", "EURUSD", "2024", "01", "EURUSD-2024-01-h1.csv")
	feb := filepath.Join(root, "oanda", "canonical", "EURUSD", "2024", "02", "EURUSD-2024-02-h1.csv")
	assert.Contains(t, out, jan+": built, good 2, bad 0")
	assert.Contains(t, out, feb+": built, good 1, bad 0")
	assert.Contains(t, out, "total: built 2, unchanged 0, replaced 0, missing raw 0, failed 0; good bars 3, bad records 0")
	data, rerr := store.Read("oanda/canonical/EURUSD/2024/01/EURUSD-2024-01-h1.csv")
	require.NoError(t, rerr)
	assert.True(t, strings.HasPrefix(string(data), "# schema=canonical-v2 "), string(data[:60]))
	assert.NotEmpty(t, logs, "the run is logged")
}

func TestDataBuild_ARerunIsUnchangedAndNewRawRowsReplace(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.January, rawRec(utc(2024, 1, 2, 0), true))
	args := buildArgs(t, root, "2024-03-01")
	_, _, err := runData(t, nil, args...)
	require.NoError(t, err)

	again, _, err := runData(t, nil, args...)
	require.NoError(t, err)
	require.NoError(t, oanda.UpdatePartition(context.Background(), oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.January},
		func(existing []oanda.Record) ([]oanda.Record, error) {
			return append(existing, rawRec(utc(2024, 1, 2, 1), true)), nil
		}))
	changed, _, err := runData(t, nil, args...)

	require.NoError(t, err)
	assert.Contains(t, again, ": unchanged, good 1, bad 0")
	assert.Contains(t, again, "unchanged 1")
	assert.Contains(t, changed, ": replaced, good 2, bad 0")
}

func TestDataBuild_ReportsMissingRawBadRecordsAndFailures(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.January, rawRec(utc(2024, 1, 2, 0), true), rawRec(utc(2024, 1, 2, 1), false))
	// February has no raw file; March's file is damaged.
	damaged, err := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.March}.Path()
	require.NoError(t, err)
	require.NoError(t, store.Write(context.Background(), damaged, []byte("not a raw file\n")))

	out, _, err := runData(t, nil, buildArgs(t, root, "2024-04-01")...)

	require.ErrorIs(t, err, service.ErrPartitionsFailed)
	assert.Contains(t, out, ": built, good 1, bad 1", "the bad record is counted")
	assert.Contains(t, out, "EURUSD 2024-02: no raw partition")
	assert.Contains(t, out, "EURUSD 2024-03: failed:")
	assert.Contains(t, out, "total: built 1, unchanged 0, replaced 0, missing raw 1, failed 1; good bars 1, bad records 1")
}

func TestDataBuild_TakesItsOptionsFromTheConfigFile(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "GBPUSD", oanda.RawH1, 2024, time.January, rawRec(utc(2024, 1, 2, 0), true))
	cfg := writeConfig(t, fmt.Sprintf("marketdata:\n  root: %s\n  provider: oanda\n  instruments: [GBPUSD]\n  interval: H1\n", root))

	out, _, err := runData(t, nil, "data", "build", "--config", cfg, "--from", "2024-01-01", "--to", "2024-02-01")

	require.NoError(t, err)
	assert.Contains(t, out, filepath.Join(root, "oanda", "canonical", "GBPUSD", "2024", "01", "GBPUSD-2024-01-h1.csv")+": built")
}

func TestDataBuild_RejectsBadOptionsBeforeDoingAnything(t *testing.T) {
	root := storetest.Root(t)
	cfg := writeConfig(t, "")
	base := func(drop ...string) []string {
		all := map[string]string{"--config": cfg, "--data-root": root, "--instrument": "EURUSD", "--interval": "h1", "--from": "2024-01-01", "--to": "2024-02-01"}
		args := []string{"data", "build"}
		for k, v := range all {
			skip := false
			for _, d := range drop {
				skip = skip || d == k
			}
			if !skip {
				args = append(args, k, v)
			}
		}
		return args
	}
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"no root":        {base("--data-root"), "no data root"},
		"no instrument":  {base("--instrument"), "no instrument"},
		"no interval":    {base("--interval"), "no interval"},
		"no range":       {base("--from", "--to"), "--from and --to"},
		"reversed range": {append(base("--from", "--to"), "--from", "2024-02-01", "--to", "2024-01-01"), "--to must be after --from"},
		"gold":           {append(base("--instrument"), "--instrument", "XAUUSD"), "XAUUSD"},
		"weekly":         {append(base("--interval"), "--interval", "w1"), "w1"},
		"other provider": {append(base("--config"), "--config", writeConfig(t, "marketdata:\n  provider: alpaca\n")), "not supported"},
	} {
		t.Run(name, func(t *testing.T) {
			out, _, err := runData(t, nil, tc.args...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, out, "total:", "nothing ran")
		})
	}
}

func TestDataBuild_NeverChangesRaw(t *testing.T) {
	root := storetest.Root(t)
	putRawMonth(t, "EURUSD", oanda.RawH1, 2024, time.January, rawRec(utc(2024, 1, 2, 0), true))
	const rawPath = "oanda/raw/EURUSD/2024/01/EURUSD-2024-01-h1.csv"
	before, err := store.Read(rawPath)
	require.NoError(t, err)

	_, _, err = runData(t, nil, buildArgs(t, root, "2024-03-01")...)
	require.NoError(t, err)

	after, err := store.Read(rawPath)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

// TestBuildCommand_IsTransportOnly keeps the build command thin: build.go imports
// the application service and the store (for the root), never a provider or a
// market-data package, so the layering transport, application service, domain
// service, provider holds (ADR-091).
func TestBuildCommand_IsTransportOnly(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod + "/service": true, mod + "/store": true, mod + "/internal/logging": true}
	f, err := parser.ParseFile(token.NewFileSet(), "build.go", nil, parser.ImportsOnly)
	require.NoError(t, err)
	for _, imp := range f.Imports {
		path, uerr := strconv.Unquote(imp.Path.Value)
		require.NoError(t, uerr)
		if path == mod || strings.HasPrefix(path, mod+"/") {
			assert.True(t, allowed[path], "build.go imports %s: the command may import only the application service, the store and logging", path)
		}
	}
}
