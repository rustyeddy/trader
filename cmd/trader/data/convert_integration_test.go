package data_test

import (
	"archive/zip"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDataConvertStooqZIPToCanonical(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "daily.zip")
	content := []byte("<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\n" +
		"SPY.US,D,20200131,000000,100,101,99,100.5,1000,0\n" +
		"SPY.US,D,20200203,000000,100.5,102,100,101.5,2000,0\n")
	writeZIPMember(t, archivePath, "data/daily/us/nyse etfs/2/spy.us.txt", content)
	before := sha256.Sum256(mustReadFile(t, archivePath))
	rawRoot, storeRoot := t.TempDir(), t.TempDir()

	out, err := runData(t, storeRoot, rawRoot,
		"convert", "SPY", "D1", "--provider", "stooq", "--archive", archivePath,
		"--exchange", "ARCA", "--kind", "etf", "--from", "2020-01-01", "--to", "2020-03-01")
	require.NoError(t, err)
	require.Contains(t, out, "imported 2 rows across 2 raw months; published 2 canonical partitions")
	require.Equal(t, before, sha256.Sum256(mustReadFile(t, archivePath)))
	require.FileExists(t, filepath.Join(rawRoot, "SPY", "2020", "01", "SPY-2020-01-d1.csv"))
	canonical := filepath.Join(storeRoot, "stooq", "SPY", "2020", "01", "SPY-2020-01-d1.csv")
	require.FileExists(t, canonical)
	require.Contains(t, string(mustReadFile(t, canonical)), `"provider":"stooq"`)
}

func writeZIPMember(t *testing.T, path, name string, content []byte) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, err := zw.Create(name)
	require.NoError(t, err)
	_, err = w.Write(content)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func TestStq2BarsUsesStooqDefaultsAndKnownInstrument(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "spy_us_d.zip")
	content := []byte("<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\n" +
		"SPY.US,D,20200131,000000,100,101,99,100.5,1000,0\n")
	writeZIPMember(t, archivePath, "data/daily/us/nyse etfs/spy.us.txt", content)
	rawRoot, storeRoot := t.TempDir(), t.TempDir()

	out, err := runData(t, storeRoot, rawRoot, "stq2bars", "SPY", "--archive", archivePath)
	require.NoError(t, err)
	require.Contains(t, out, "converted SPY (updated)")
	rebuilt, err := runData(t, storeRoot, rawRoot, "stq2bars", "SPY", "--archive", archivePath, "--from", "2020-01-01", "--to", "2020-02-01", "--rebuild")
	require.NoError(t, err)
	require.Contains(t, rebuilt, "converted SPY (rebuilt)")
	require.Contains(t, rebuilt, "published 1 canonical partitions")
	require.FileExists(t, filepath.Join(storeRoot, "stooq", "SPY", "2020", "01", "SPY-2020-01-d1.csv"))
}

func TestStq2BarsPreservesConfiguredProvider(t *testing.T) {
	t.Setenv("TRADER_PROVIDER", "alpaca")
	_, err := runData(t, t.TempDir(), t.TempDir(), "stq2bars", "SPY", "--archive", filepath.Join(t.TempDir(), "missing.zip"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires provider stooq")
}

func TestStq2BarsRejectsUnknownInstrumentWithoutIdentityOverride(t *testing.T) {
	_, err := runData(t, t.TempDir(), t.TempDir(), "stq2bars", "UNKNOWN", "--archive", filepath.Join(t.TempDir(), "missing.zip"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "provide --exchange and --kind")
}
