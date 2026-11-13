package layout_test

import (
	"path/filepath"
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
	root := t.TempDir()

	got, err := eurusd().Path(root)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "oanda", "raw", "EURUSD", "2020", "05", "EURUSD-2020-05-h1.csv"), got)
}

func TestPartitionDirAndFileName(t *testing.T) {
	root := t.TempDir()
	p := eurusd()
	p.Stage = layout.Canonical
	p.Interval = "d1"

	dir, err := p.Dir(root)
	require.NoError(t, err)
	name, err := p.FileName()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(root, "oanda", "canonical", "EURUSD", "2020", "05"), dir)
	assert.Equal(t, "EURUSD-2020-05-d1.csv", name)
}

func TestStageDir(t *testing.T) {
	root := t.TempDir()

	got, err := layout.StageDir(root, "oanda", layout.Raw)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "oanda", "raw"), got)
}

func TestStageDirRejectsBadInput(t *testing.T) {
	_, err := layout.StageDir("", "oanda", layout.Raw)
	assert.ErrorIs(t, err, layout.ErrInvalidRoot)
	_, err = layout.StageDir("/data", "../x", layout.Raw)
	assert.ErrorIs(t, err, layout.ErrInvalidComponent)
	_, err = layout.StageDir("/data", "oanda", "candles")
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
			_, err := p.Path("/data")
			assert.ErrorIs(t, err, layout.ErrInvalidComponent)
		})
	}
}

func TestPathRejectsEmptyRoot(t *testing.T) {
	_, err := eurusd().Path("")
	assert.ErrorIs(t, err, layout.ErrInvalidRoot)
}

func TestParseRoundTrips(t *testing.T) {
	root := t.TempDir()
	for _, p := range []layout.Partition{
		eurusd(),
		{Source: "oanda", Stage: layout.Canonical, Instrument: "USDJPY", Year: 2026, Month: time.December, Interval: "d1"},
		{Source: "stooq", Stage: layout.Raw, Instrument: "BRK-B", Year: 1999, Month: time.January, Interval: "m1"},
	} {
		path, err := p.Path(root)
		require.NoError(t, err)

		got, err := layout.Parse(root, path)

		require.NoError(t, err, path)
		assert.Equal(t, p, got)
	}
}

func TestParseRejectsNonPartitionPaths(t *testing.T) {
	root := t.TempDir()
	good, err := eurusd().Path(root)
	require.NoError(t, err)
	for name, path := range map[string]string{
		"outside the root":   "/elsewhere/oanda/raw/EURUSD/2020/05/EURUSD-2020-05-h1.csv",
		"legacy order":       filepath.Join(root, "raw", "oanda", "EURUSD", "2020", "05", "EURUSD-2020-05-h1.csv"),
		"too shallow":        filepath.Join(root, "oanda", "raw", "EURUSD-2020-05-h1.csv"),
		"too deep":           filepath.Join(good, "x"),
		"not csv":            filepath.Join(root, "oanda", "raw", "EURUSD", "2020", "05", "EURUSD-2020-05-h1.txt"),
		"short month":        filepath.Join(root, "oanda", "raw", "EURUSD", "2020", "5", "EURUSD-2020-5-h1.csv"),
		"signed month":       filepath.Join(root, "oanda", "raw", "EURUSD", "2020", "+1", "EURUSD-2020-01-h1.csv"),
		"signed year":        filepath.Join(root, "oanda", "raw", "EURUSD", "+2020", "01", "EURUSD-2020-01-h1.csv"),
		"minus month":        filepath.Join(root, "oanda", "raw", "EURUSD", "2020", "-1", "EURUSD-2020--1-h1.csv"),
		"directory mismatch": filepath.Join(root, "oanda", "raw", "EURUSD", "2020", "06", "EURUSD-2020-05-h1.csv"),
		"symbol mismatch":    filepath.Join(root, "oanda", "raw", "EURUSD", "2020", "05", "GBPUSD-2020-05-h1.csv"),
		"unknown stage":      filepath.Join(root, "oanda", "candles", "EURUSD", "2020", "05", "EURUSD-2020-05-h1.csv"),
		"no interval":        filepath.Join(root, "oanda", "raw", "EURUSD", "2020", "05", "EURUSD.csv"),
		"upper interval":     filepath.Join(root, "oanda", "raw", "EURUSD", "2020", "05", "EURUSD-2020-05-H1.csv"),
		"dot-dot":            filepath.Join(root, "..", "oanda", "raw", "EURUSD", "2020", "05", "EURUSD-2020-05-h1.csv"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := layout.Parse(root, path)

			assert.ErrorIs(t, err, layout.ErrNotPartitionPath)
		})
	}
	_, err = layout.Parse("", good)
	assert.ErrorIs(t, err, layout.ErrInvalidRoot)
}

func TestStageValid(t *testing.T) {
	assert.True(t, layout.Raw.Valid())
	assert.True(t, layout.Canonical.Valid())
	assert.False(t, layout.Stage("raw ").Valid())
}

func TestPathIsAbsoluteSoRelativeAndAbsoluteRootsAgree(t *testing.T) {
	t.Chdir(t.TempDir())
	abs, err := filepath.Abs("data")
	require.NoError(t, err)

	rel, err := eurusd().Path("data")
	require.NoError(t, err)
	viaAbs, err := eurusd().Path(abs)
	require.NoError(t, err)

	assert.True(t, filepath.IsAbs(rel))
	assert.Equal(t, viaAbs, rel)
	dir, err := layout.StageDir("./data/", "oanda", layout.Raw)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(abs, "oanda", "raw"), dir)
}

func TestParseAcceptsARelativeRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := eurusd().Path("data")
	require.NoError(t, err)

	got, err := layout.Parse("data", path)

	require.NoError(t, err)
	assert.Equal(t, eurusd(), got)
}
