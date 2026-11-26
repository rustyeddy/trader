package layout_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/marketdata/layout"
)

func eurusd() layout.Partition {
	return layout.Partition{Source: "oanda", Stage: layout.Raw, Instrument: "EURUSD", Year: 2020, Month: time.May, Interval: "h1"}
}

func TestPartitionPathUsesSourceThenStage(t *testing.T) {
	got, err := eurusd().Path()

	require.NoError(t, err)
	assert.Equal(t, filepath.Join("oanda", "raw", "EURUSD", "2020", "05", "EURUSD-2020-05-h1.csv"), got)
}

func TestPartitionDirAndFileName(t *testing.T) {
	p := eurusd()
	p.Stage = layout.Canonical
	p.Interval = "d1"

	dir, err := p.Dir()
	require.NoError(t, err)
	name, err := p.FileName()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join("oanda", "canonical", "EURUSD", "2020", "05"), dir)
	assert.Equal(t, "EURUSD-2020-05-d1.csv", name)
}

func TestStageDir(t *testing.T) {
	got, err := layout.StageDir("oanda", layout.Raw)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join("oanda", "raw"), got)
}

func TestStageDirRejectsBadInput(t *testing.T) {
	_, err := layout.StageDir("../x", layout.Raw)
	assert.ErrorIs(t, err, layout.ErrInvalidComponent)
	_, err = layout.StageDir("oanda", "candles")
	assert.ErrorIs(t, err, layout.ErrInvalidComponent)
}

func TestValidateRejectsUnsafeComponents(t *testing.T) {
	for name, mutate := range map[string]func(*layout.Partition){
		"empty source":       func(p *layout.Partition) { p.Source = "" },
		"traversal source":   func(p *layout.Partition) { p.Source = ".." },
		"slash source":       func(p *layout.Partition) { p.Source = "a/b" },
		"backslash source":   func(p *layout.Partition) { p.Source = `a\b` },
		"dot source":         func(p *layout.Partition) { p.Source = ".hidden" },
		"empty instrument":   func(p *layout.Partition) { p.Instrument = "" },
		"traversal inst":     func(p *layout.Partition) { p.Instrument = "../../etc" },
		"absolute inst":      func(p *layout.Partition) { p.Instrument = "/etc" },
		"space inst":         func(p *layout.Partition) { p.Instrument = "EUR USD" },
		"nul inst":           func(p *layout.Partition) { p.Instrument = "EUR\x00USD" },
		"bad stage":          func(p *layout.Partition) { p.Stage = "candles" },
		"empty stage":        func(p *layout.Partition) { p.Stage = "" },
		"short year":         func(p *layout.Partition) { p.Year = 999 },
		"long year":          func(p *layout.Partition) { p.Year = 10000 },
		"month zero":         func(p *layout.Partition) { p.Month = 0 },
		"month thirteen":     func(p *layout.Partition) { p.Month = 13 },
		"upper interval":     func(p *layout.Partition) { p.Interval = "H1" },
		"dashed interval":    func(p *layout.Partition) { p.Interval = "h-1" },
		"empty interval":     func(p *layout.Partition) { p.Interval = "" },
		"traversal interval": func(p *layout.Partition) { p.Interval = "../h1" },
	} {
		t.Run(name, func(t *testing.T) {
			p := eurusd()
			mutate(&p)

			require.ErrorIs(t, p.Validate(), layout.ErrInvalidComponent)
			_, err := p.Path()
			assert.ErrorIs(t, err, layout.ErrInvalidComponent)
		})
	}
}

func TestParseRoundTrips(t *testing.T) {
	for _, p := range []layout.Partition{
		eurusd(),
		{Source: "oanda", Stage: layout.Canonical, Instrument: "USDJPY", Year: 2026, Month: time.December, Interval: "d1"},
		{Source: "stooq", Stage: layout.Raw, Instrument: "BRK-B", Year: 1999, Month: time.January, Interval: "m1"},
	} {
		path, err := p.Path()
		require.NoError(t, err)

		got, err := layout.Parse(path)

		require.NoError(t, err, path)
		assert.Equal(t, p, got)
	}
}

func TestParseRejectsNonPartitionPaths(t *testing.T) {
	good, err := eurusd().Path()
	require.NoError(t, err)
	for name, path := range map[string]string{
		"absolute":           "/elsewhere/oanda/raw/EURUSD/2020/05/EURUSD-2020-05-h1.csv",
		"legacy order":       filepath.Join("raw", "oanda", "EURUSD", "2020", "05", "EURUSD-2020-05-h1.csv"),
		"too shallow":        filepath.Join("oanda", "raw", "EURUSD-2020-05-h1.csv"),
		"too deep":           filepath.Join(good, "x"),
		"not csv":            filepath.Join("oanda", "raw", "EURUSD", "2020", "05", "EURUSD-2020-05-h1.txt"),
		"short month":        filepath.Join("oanda", "raw", "EURUSD", "2020", "5", "EURUSD-2020-5-h1.csv"),
		"signed month":       filepath.Join("oanda", "raw", "EURUSD", "2020", "+1", "EURUSD-2020-01-h1.csv"),
		"signed year":        filepath.Join("oanda", "raw", "EURUSD", "+2020", "01", "EURUSD-2020-01-h1.csv"),
		"minus month":        filepath.Join("oanda", "raw", "EURUSD", "2020", "-1", "EURUSD-2020--1-h1.csv"),
		"directory mismatch": filepath.Join("oanda", "raw", "EURUSD", "2020", "06", "EURUSD-2020-05-h1.csv"),
		"symbol mismatch":    filepath.Join("oanda", "raw", "EURUSD", "2020", "05", "GBPUSD-2020-05-h1.csv"),
		"unknown stage":      filepath.Join("oanda", "candles", "EURUSD", "2020", "05", "EURUSD-2020-05-h1.csv"),
		"no interval":        filepath.Join("oanda", "raw", "EURUSD", "2020", "05", "EURUSD.csv"),
		"upper interval":     filepath.Join("oanda", "raw", "EURUSD", "2020", "05", "EURUSD-2020-05-H1.csv"),
		"dot-dot":            filepath.Join("..", "oanda", "raw", "EURUSD", "2020", "05", "EURUSD-2020-05-h1.csv"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := layout.Parse(path)

			assert.ErrorIs(t, err, layout.ErrNotPartitionPath)
		})
	}
	_, err = layout.Parse("")
	assert.ErrorIs(t, err, layout.ErrNotPartitionPath)
}

func TestStageValid(t *testing.T) {
	assert.True(t, layout.Raw.Valid())
	assert.True(t, layout.Unzipped.Valid())
	assert.True(t, layout.Canonical.Valid())
	assert.False(t, layout.Stage("raw ").Valid())
	assert.False(t, layout.Stage("Unzipped").Valid())
	assert.False(t, layout.Stage("").Valid())
}

func TestLegacyRawDir_MapsTheOldRawDirectoryToTheCurrentOne(t *testing.T) {
	for _, source := range []string{"oanda", "dukascopy", "stooq-managed", "alpaca"} {
		legacy, current, err := layout.LegacyRawDir(source)

		require.NoError(t, err)
		assert.Equal(t, "raw/"+source, filepath.ToSlash(legacy))
		assert.Equal(t, source+"/raw", filepath.ToSlash(current))
		want, _ := layout.StageDir(source, layout.Raw)
		assert.Equal(t, want, current, "the current directory is the layout's own")
	}
}

func TestLegacyRawDir_RefusesANameThatCannotBeASourceOrIsAStage(t *testing.T) {
	for _, source := range []string{"", ".hidden", "../x", "a/b", "has space", "raw", "canonical"} {
		_, _, err := layout.LegacyRawDir(source)

		assert.ErrorIs(t, err, layout.ErrInvalidComponent, "%q", source)
	}
}

func TestUnzippedStage_ResolvesAndParsesLikeTheOthers(t *testing.T) {
	p := layout.Partition{Source: "stooq", Stage: layout.Unzipped, Instrument: "SPY", Year: 2017, Month: time.May, Interval: "d1"}

	path, err := p.Path()

	require.NoError(t, err)
	assert.Equal(t, "stooq/unzipped/SPY/2017/05/SPY-2017-05-d1.csv", filepath.ToSlash(path))
	back, err := layout.Parse(path)
	require.NoError(t, err)
	assert.Equal(t, p, back)
	dir, err := layout.StageDir("stooq", layout.Unzipped)
	require.NoError(t, err)
	assert.Equal(t, "stooq/unzipped", filepath.ToSlash(dir))
	_, err = layout.StageDir("stooq", layout.Stage("unzip"))
	assert.ErrorIs(t, err, layout.ErrInvalidComponent)
}

func TestSnapshot_PathsAndRefusals(t *testing.T) {
	hash := strings.Repeat("0a", 32)

	raw, record, err := layout.Snapshot("stooq", "SPY", hash)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join("stooq", "snapshots", "SPY", hash+".raw"), raw)
	assert.Equal(t, filepath.Join("stooq", "snapshots", "SPY", hash+".json"), record)
	for _, bad := range [][3]string{{"", "SPY", hash}, {"stooq", "..", hash}, {"stooq", "SPY", "abc"},
		{"stooq", "SPY", strings.Repeat("0A", 32)}, {"stooq", "SPY", strings.Repeat("g", 64)}} {
		_, _, err := layout.Snapshot(bad[0], bad[1], bad[2])
		assert.ErrorIs(t, err, layout.ErrInvalidComponent, "%q", bad)
	}
}
