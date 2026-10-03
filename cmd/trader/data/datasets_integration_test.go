package data_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

func TestDataBuild_OmittedRangeBuildsWholeRawSpan(t *testing.T) {
	out, err := runData(t, t.TempDir(), copyFixtureRaw(t), "build", "EURUSD", "H1")
	require.NoError(t, err)
	assert.Contains(t, out, "2024-01")
	assert.Contains(t, out, "2024-02")
}

func TestDataBuild_NoRawDataInRangeFails(t *testing.T) {
	_, err := runData(t, t.TempDir(), copyFixtureRaw(t), "build", "EURUSD", "H1", "--from", "2024-03-01", "--to", "2024-04-01")
	require.ErrorIs(t, err, svc.ErrNoRawData)
}

func TestDataUpdate_OmittedRangeNeedsCanonicalData(t *testing.T) {
	_, err := runData(t, t.TempDir(), copyFixtureRaw(t), "update", "EURUSD", "H1")
	require.ErrorIs(t, err, svc.ErrNoCanonicalData)
}

func TestDataUpdate_StooqReconvertsArchive(t *testing.T) {
	archiveRoot := t.TempDir()
	content := []byte("<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\n" +
		"SPY.US,D,20200131,000000,100,101,99,100.5,1000,0\n")
	writeZIPMember(t, filepath.Join(archiveRoot, "spy_us_d.zip"), "spy.us.txt", content)
	rawRoot, storeRoot := t.TempDir(), t.TempDir()
	_, err := runData(t, storeRoot, rawRoot, "stq2bars", "SPY", "--archive-root", archiveRoot)
	require.NoError(t, err)

	out, err := runData(t, storeRoot, rawRoot, "update", "SPY", "D1", "--provider", "stooq", "--archive-root", archiveRoot)
	require.NoError(t, err, "stooq updates by re-converting its archive, with no download")
	assert.NotContains(t, out, "download")
}

func TestStq2BarsFallsBackToImportedRaw(t *testing.T) {
	archiveDir := t.TempDir()
	content := []byte("<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\n" +
		"SPY.US,D,20200131,000000,100,101,99,100.5,1000,0\n")
	archive := filepath.Join(archiveDir, "spy_us_d.zip")
	writeZIPMember(t, archive, "spy.us.txt", content)
	rawRoot, storeRoot := t.TempDir(), t.TempDir()
	_, err := runData(t, storeRoot, rawRoot, "stq2bars", "SPY", "--archive", archive)
	require.NoError(t, err)

	out, err := runData(t, storeRoot, rawRoot, "stq2bars", "SPY", "--archive-root", t.TempDir(), "--rebuild")
	require.NoError(t, err)
	assert.Contains(t, out, "no archive found; built from existing raw data; published 1 canonical partitions")
}
