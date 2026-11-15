package oanda

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/marketdata/layout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPartitionKeyPath_UsesTheLayoutResolver(t *testing.T) {
	root := t.TempDir()

	got, err := PartitionKey{"EURUSD", RawH4, 2024, time.July}.Path(root)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "oanda", "raw", "EURUSD", "2024", "07", "EURUSD-2024-07-h4.csv"), got)
	p, err := layout.Parse(root, got)
	require.NoError(t, err)
	assert.Equal(t, layout.Partition{Source: "oanda", Stage: layout.Raw, Instrument: "EURUSD", Year: 2024, Month: time.July, Interval: "h4"}, p)
}

func TestPartitionKeyPath_RejectsWhatIsNotInTheCorpus(t *testing.T) {
	root := t.TempDir()
	for name, tc := range map[string]struct {
		symbol   string
		interval RawInterval
		year     int
		month    time.Month
		want     error
	}{
		"gold":            {"XAUUSD", RawH1, 2024, 1, ErrInstrumentOutOfScope},
		"traversal":       {"../EURUSD", RawH1, 2024, 1, ErrMalformedData},
		"lower case":      {"eurusd", RawH1, 2024, 1, ErrMalformedData},
		"weekly":          {"EURUSD", RawInterval("w1"), 2024, 1, ErrUnsupportedInterval},
		"daily spelled d": {"EURUSD", RawInterval("d"), 2024, 1, ErrUnsupportedInterval},
		"upper interval":  {"EURUSD", RawInterval("H1"), 2024, 1, ErrUnsupportedInterval},
		"month zero":      {"EURUSD", RawH1, 2024, 0, layout.ErrInvalidComponent},
		"month 13":        {"EURUSD", RawH1, 2024, 13, layout.ErrInvalidComponent},
		"year 99":         {"EURUSD", RawH1, 99, 1, layout.ErrInvalidComponent},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := PartitionKey{tc.symbol, tc.interval, tc.year, tc.month}.Path(root)

			assert.ErrorIs(t, err, tc.want)
		})
	}
	_, err := PartitionKey{"EURUSD", RawH1, 2024, 1}.Path("")
	assert.ErrorIs(t, err, layout.ErrInvalidRoot)
}

func TestPartitionKeyValidate(t *testing.T) {
	assert.NoError(t, PartitionKey{"EURUSD", RawD1, 2024, time.December}.Validate())
	assert.ErrorIs(t, PartitionKey{"XAUUSD", RawH1, 2024, 1}.Validate(), ErrInstrumentOutOfScope)
	assert.ErrorIs(t, PartitionKey{"EURUSD", RawInterval("w1"), 2024, 1}.Validate(), ErrUnsupportedInterval)
	assert.ErrorIs(t, PartitionKey{"EURUSD", RawH1, 2024, 13}.Validate(), layout.ErrInvalidComponent)
	assert.ErrorIs(t, PartitionKey{}.Validate(), ErrMalformedData, "the zero key names nothing")
}

func TestMetaKeyRoundTripsThroughThePath(t *testing.T) {
	root := t.TempDir()
	for _, f := range fixtures {
		meta, _, err := parsePathMeta(filepath.Join("testdata", f.file))
		require.NoError(t, err)

		key := meta.Key()

		assert.Equal(t, PartitionKey{f.symbol, f.interval, f.year, f.month}, key, f.file)
		path, err := key.Path(root)
		require.NoError(t, err)
		assert.Equal(t, f.file, filepath.Base(path), "the file name is the fixture's name")
	}
}
