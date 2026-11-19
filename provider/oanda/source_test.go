package oanda

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadSourcePartition_MapsRowsFingerprintAndNamesWithoutJudging(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	key := PartitionKey{"EURUSD", RawH1, 2024, time.January}
	crossed := testRecord(at(2024, 1, 1, 2), true)
	crossed.AskOpen = num.MustParsePrice("1.09990") // below the bid: the domain's call, not the provider's
	records := []Record{testRecord(at(2024, 1, 1, 0), true), testRecord(at(2024, 1, 1, 1), false), crossed}
	require.NoError(t, WritePartition(ctx, key, records, true))

	got, err := ReadSourcePartition(ctx, key)

	require.NoError(t, err)
	fp, ferr := FingerprintPartition(key)
	require.NoError(t, ferr)
	assert.Equal(t, "oanda", got.Source)
	assert.Equal(t, "EURUSD", got.Instrument)
	assert.Equal(t, "h1", got.Interval)
	assert.Equal(t, 2024, got.Year)
	assert.Equal(t, time.January, got.Month)
	assert.Equal(t, fp, got.Fingerprint, "the fingerprint of the bytes the rows came from")
	require.NoError(t, got.Validate())
	require.Len(t, got.Rows, 3)
	for i, r := range got.Rows {
		assert.Equal(t, i+1, r.Row, "1-based among the data rows")
		assert.Equal(t, "EURUSD", r.Bar.Instrument, "the mapper names each row's bar")
		assert.Equal(t, records[i].Time, r.Bar.Time)
		assert.Equal(t, records[i].Complete, r.Complete)
		assert.Equal(t, int64(100), r.Bar.Volume)
		assert.True(t, r.Bar.AvailableAt.IsZero(), "availability is the domain's to set")
		assert.True(t, r.Bar.Mid.IsZero(), "the mid is the domain's to derive")
	}
	assert.Equal(t, marketdata.OHLC{Open: num.MustParsePrice("1.10000"), High: num.MustParsePrice("1.10100"),
		Low: num.MustParsePrice("1.09900"), Close: num.MustParsePrice("1.10050")}, got.Rows[0].Bar.Bid)
	assert.Equal(t, num.MustParsePrice("1.10060"), got.Rows[0].Bar.Ask.Close)
	assert.Equal(t, num.MustParsePrice("1.09990"), got.Rows[2].Bar.Ask.Open, "a crossed row is passed through")
	assert.False(t, got.Rows[1].Complete, "the provider's flag is passed through")
}

func TestReadSourcePartition_FingerprintFollowsTheBytes(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	key := PartitionKey{"EURUSD", RawH1, 2024, time.January}
	require.NoError(t, WritePartition(ctx, key, []Record{testRecord(at(2024, 1, 1, 0), true)}, true))
	before, err := ReadSourcePartition(ctx, key)
	require.NoError(t, err)
	require.NoError(t, UpdatePartition(ctx, key, func(existing []Record) ([]Record, error) {
		return append(existing, testRecord(at(2024, 1, 1, 1), true)), nil
	}))

	after, err := ReadSourcePartition(ctx, key)

	require.NoError(t, err)
	assert.NotEqual(t, before.Fingerprint, after.Fingerprint)
	assert.Len(t, after.Rows, 2)
}

func TestReadSourcePartition_ErrorsAreReportedNotMapped(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	key := PartitionKey{"EURUSD", RawH1, 2024, time.January}

	_, missing := ReadSourcePartition(ctx, key)
	path, err := key.Path()
	require.NoError(t, err)
	require.NoError(t, store.Write(ctx, path, []byte("garbage\n")))
	_, malformed := ReadSourcePartition(ctx, key)
	_, badKey := ReadSourcePartition(ctx, PartitionKey{"XAUUSD", RawH1, 2024, time.January})
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.NoError(t, WritePartition(ctx, key, []Record{testRecord(at(2024, 1, 1, 0), true)}, false))
	_, cancelErr := ReadSourcePartition(cancelled, key)

	assert.ErrorIs(t, missing, fs.ErrNotExist)
	assert.ErrorIs(t, malformed, ErrMalformedData)
	assert.ErrorIs(t, badKey, ErrInstrumentOutOfScope)
	assert.ErrorIs(t, cancelErr, context.Canceled)
}
