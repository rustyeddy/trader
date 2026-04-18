package trader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/ulikunitz/xz/lzma"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func writeLZMAFile(t *testing.T, path string, payload []byte) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	w, err := lzma.NewWriter(f)
	require.NoError(t, err)
	_, err = w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

func testTickKey(hour int) Key {
	return Key{
		Instrument: "EURUSD",
		Source:     "dukascopy",
		Kind:       KindTick,
		TF:         Ticks,
		Year:       2026,
		Month:      1,
		Day:        5, // Monday
		Hour:       hour,
	}
}

func TestDataManagerSync_BuildInventoryError(t *testing.T) {
	tmp := t.TempDir()
	missingDir := filepath.Join(tmp, "missing-dir")

	oldStore := store
	store = &Store{basedir: missingDir}
	t.Cleanup(func() { store = oldStore })

	dm := NewDataManager([]string{"EURUSD"}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	dm.Init()

	err := dm.Sync(context.Background(), false, false)
	require.Error(t, err)
}

func TestExecuteDownloads_NonEmptyPlanCompletes(t *testing.T) {
	useTempStore(t)

	key := testTickKey(10)
	dm := &DataManager{
		inventory: NewInventory(),
		plan:      &Plan{Download: []Key{key}},
		downloader: &downloader{
			Client: &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("ok")),
						Header:     make(http.Header),
						Request:    req,
					}, nil
				}),
			},
			downloaders: 1,
		},
	}

	err := dm.ExecuteDownloads(context.Background())
	require.NoError(t, err)
	require.True(t, dm.inventory.HasComplete(key))
}

func TestCandleSetIterator_NextCandleAndCandleTimeBoundaries(t *testing.T) {
	cs := newMonthlyCandleSet(t, "EURUSD", 2026, time.January, H1)
	cs.Candles[0] = Candle{Open: 1, High: 2, Low: 1, Close: 2, Ticks: 1}
	cs.SetValid(0)

	it := NewCandleSetIterator(cs, TimeRange{})
	first, ok := it.NextCandle()
	require.True(t, ok)
	require.Equal(t, cs.Candles[0], first)

	ct := it.CandleTime()
	require.Equal(t, first, ct.Candle)
	require.NotZero(t, ct.Timestamp)

	require.False(t, it.Next())
	require.Equal(t, CandleTime{}, it.CandleTime())

	last, ok := it.NextCandle()
	require.False(t, ok)
	require.Equal(t, Candle{}, last)
}

func TestChainedCandleIterator_TransitionsAcrossEmptyAndNil(t *testing.T) {
	empty := newMonthlyCandleSet(t, "EURUSD", 2026, time.January, H1)
	one := newMonthlyCandleSet(t, "EURUSD", 2026, time.February, H1)
	one.Candles[0] = Candle{Open: 10, High: 10, Low: 10, Close: 10, Ticks: 1}
	one.SetValid(0)

	it := NewChainedCandleIterator(
		NewCandleSetIterator(empty, TimeRange{}),
		nil,
		NewCandleSetIterator(one, TimeRange{}),
	)

	c, ok := it.NextCandle()
	require.True(t, ok)
	require.Equal(t, one.Candles[0], c)

	require.False(t, it.Next())
	require.NoError(t, it.Err())
	require.Equal(t, CandleTime{}, it.CandleTime())
}

func TestDukasfileIsValid_EmptyFileOutsideClosedHours(t *testing.T) {
	s := useTempStore(t)
	df := newDatafile("EURUSD", time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC))
	path := s.PathForAsset(df.Key())
	require.NoError(t, makeParentsAndFile(path, nil))

	df.bytes = 0
	err := df.IsValid(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty file outside market-closed hours")
}

func TestDukasfileIsValid_EmptyWeekendFileAllowed(t *testing.T) {
	s := useTempStore(t)
	df := newDatafile("EURUSD", time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)) // Saturday
	path := s.PathForAsset(df.Key())
	require.NoError(t, makeParentsAndFile(path, nil))

	df.bytes = 0
	err := df.IsValid(context.Background())
	require.NoError(t, err)
}

func TestDukasfileForEachTick1_MalformedPayload(t *testing.T) {
	s := useTempStore(t)
	df := newDatafile("EURUSD", time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC))
	path := s.PathForAsset(df.Key())
	writeLZMAFile(t, path, []byte{1, 2, 3, 4}) // truncated BI5 record

	err := df.forEachTick1(context.Background(), func(tick RawTick) error { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), "truncated tick record")
}

func TestDukasfileForEachTick1_CallbackError(t *testing.T) {
	s := useTempStore(t)
	df := newDatafile("EURUSD", time.Date(2026, 1, 5, 11, 0, 0, 0, time.UTC))
	path := s.PathForAsset(df.Key())
	writeLZMAFile(t, path, makeBi5Record(1_000, 100, 99, 1.0, 1.0))

	sentinel := errors.New("stop")
	err := df.forEachTick1(context.Background(), func(tick RawTick) error { return sentinel })
	require.ErrorIs(t, err, sentinel)
}

func TestDownloaderDownload_Non200(t *testing.T) {
	useTempStore(t)

	dl := &downloader{
		Client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Body:       io.NopCloser(strings.NewReader("bad gateway")),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		},
		downloaders: 1,
	}

	_, err := dl.download(context.Background(), testTickKey(12))
	require.Error(t, err)
	require.Contains(t, err.Error(), "http 502")
}

func TestStartDownloader_PartialFailureThenSuccess(t *testing.T) {
	useTempStore(t)

	var calls int32
	dl := &downloader{
		Client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				n := atomic.AddInt32(&calls, 1)
				if n == 1 {
					return &http.Response{
						StatusCode: http.StatusServiceUnavailable,
						Body:       io.NopCloser(strings.NewReader("retry")),
						Header:     make(http.Header),
						Request:    req,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("ok")),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		},
		downloaders: 1,
	}

	key := testTickKey(13)
	dm := &DataManager{inventory: NewInventory()}
	q := make(chan Key, 2)
	q <- key
	q <- key
	close(q)

	wg := dl.startDownloader(context.Background(), dm, q)
	wg.Wait()

	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
	require.True(t, dm.inventory.HasComplete(key))
}
