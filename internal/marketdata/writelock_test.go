package marketdata

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/marketdata/internal/provider/oanda"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// stalledFirstDoer answers OANDA candle requests: the first returns an
// older fetch (01:00 only) but only after release is closed, having
// signaled arrived; every later one returns the newer fetch (01:00 and
// 02:00) at once.
type stalledFirstDoer struct {
	mu      sync.Mutex
	calls   int
	arrived chan struct{}
	release chan struct{}
}

func (d *stalledFirstDoer) Do(*http.Request) (*http.Response, error) {
	d.mu.Lock()
	d.calls++
	first := d.calls == 1
	d.mu.Unlock()
	hour := func(h int) time.Time { return time.Date(2020, 3, 2, h, 0, 0, 0, time.UTC) }
	body := candlesJSONForTest([]time.Time{hour(1), hour(2)}, true)
	if first {
		close(d.arrived)
		<-d.release
		body = candlesJSONForTest([]time.Time{hour(1)}, true)
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func syncManager(t *testing.T, rawRoot string, doer oanda.HTTPDoer, lock *WriteLock) *Manager {
	t.Helper()
	client, err := oanda.NewClient(oanda.ClientConfig{
		BaseURL: "https://fake.example.com", Credential: oanda.StaticCredential("test-token"),
		HTTPClient: doer, RetryBaseDelay: time.Millisecond,
	})
	require.NoError(t, err)
	m, err := New(Config{
		Clock: testClock(), StoreRoot: t.TempDir(), RawRoot: rawRoot, Resolver: testResolver(t),
		ProviderName: "oanda", WriteLock: lock, oandaClient: client,
	})
	require.NoError(t, err)
	return m
}

// overlappingExtends runs two Syncs extending the same raw partition from
// two Managers (as trader-mcp builds one per request): A reads the
// partition and stalls in its fetch; B starts while A is stalled. It
// returns the partition's final record count. lockA and lockB are the
// Managers' write locks.
func overlappingExtends(t *testing.T, lockA, lockB *WriteLock) (records int, bFinishedWhileAStalled bool) {
	t.Helper()
	ctx := context.Background()
	rawRoot := t.TempDir()
	px := num.MustParsePrice("1.1")
	seed := []oanda.Record{{Time: time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC),
		BidOpen: px, BidHigh: px, BidLow: px, BidClose: px, AskOpen: px, AskHigh: px, AskLow: px, AskClose: px,
		Volume: 10, Complete: true}}
	require.NoError(t, oanda.WritePartition(ctx, rawRoot, "EURUSD", oanda.RawH1, 2020, time.March, seed, true))

	doer := &stalledFirstDoer{arrived: make(chan struct{}), release: make(chan struct{})}
	a, b := syncManager(t, rawRoot, doer, lockA), syncManager(t, rawRoot, doer, lockB)
	plan := Plan{Actions: []Action{{Kind: ActionDownloadRaw, Instrument: eurusd(), Interval: marketdata.H1,
		Year: 2020, Month: time.March, Reason: "extend"}}}

	aDone, bDone := make(chan error, 1), make(chan error, 1)
	go func() { _, err := a.Sync(ctx, plan); aDone <- err }()
	<-doer.arrived // A has read the partition and is mid-fetch
	go func() { _, err := b.Sync(ctx, plan); bDone <- err }()

	select {
	case err := <-bDone:
		require.NoError(t, err)
		bFinishedWhileAStalled = true
	case <-time.After(200 * time.Millisecond):
	}
	close(doer.release)
	require.NoError(t, <-aDone)
	if !bFinishedWhileAStalled {
		require.NoError(t, <-bDone)
	}

	got, err := oanda.ReadPartitionRecords(ctx, rawRoot, "EURUSD", oanda.RawH1, 2020, time.March)
	require.NoError(t, err)
	return len(got), bFinishedWhileAStalled
}

// TestWriteLock_OverlappingSyncsKeepNewerRecords is the regression for the
// PR #452 review: two overlapping extends of one raw partition, from two
// Managers sharing a WriteLock, never regress the partition.
func TestWriteLock_OverlappingSyncsKeepNewerRecords(t *testing.T) {
	shared := NewWriteLock()
	records, bEarly := overlappingExtends(t, shared, shared)
	assert.False(t, bEarly, "B waits for A's whole read-modify-write")
	assert.Equal(t, 3, records, "00:00 seed, A's 01:00, and B's 02:00 all survive")
}

// TestWriteLock_WithoutSharingLosesRecords documents the race the shared
// lock prevents: with separate locks, B finishes first and A's older
// snapshot replaces B's newer partition.
func TestWriteLock_WithoutSharingLosesRecords(t *testing.T) {
	records, bEarly := overlappingExtends(t, NewWriteLock(), NewWriteLock())
	require.True(t, bEarly)
	assert.Equal(t, 2, records, "B's 02:00 record was lost to A's stale snapshot")
}

func TestWriteLock_AcquireRespectsCancellation(t *testing.T) {
	l := NewWriteLock()
	require.NoError(t, l.acquire(context.Background()))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, l.acquire(ctx), context.DeadlineExceeded, "a waiter can give up")
	l.release()
	require.NoError(t, l.acquire(context.Background()), "released")
	l.release()

	var nilLock *WriteLock
	require.NoError(t, nilLock.acquire(context.Background()))
	nilLock.release()
}

func TestWriteLock_HeldByBuildAndImport(t *testing.T) {
	lock := NewWriteLock()
	m, err := New(Config{Clock: testClock(), StoreRoot: t.TempDir(), RawRoot: t.TempDir(), Resolver: testResolver(t), ProviderName: "oanda", WriteLock: lock})
	require.NoError(t, err)
	require.NoError(t, lock.acquire(context.Background())) // another writer holds it
	defer lock.release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = m.Build(ctx, Plan{})
	assert.ErrorIs(t, err, context.DeadlineExceeded, "Build waits for the write lock")
	_, err = m.Sync(ctx, Plan{})
	assert.ErrorIs(t, err, context.DeadlineExceeded, "Sync waits for the write lock")

	stooqMgr := newStooqTestManagerWithLock(t, lock)
	_, err = stooqMgr.ImportStooqArchive(ctx, "unused", spyID(t))
	assert.ErrorIs(t, err, context.DeadlineExceeded, "import waits for the write lock")
}

func newStooqTestManagerWithLock(t *testing.T, lock *WriteLock) *Manager {
	t.Helper()
	r := instrument.NewMemoryResolver()
	require.NoError(t, r.Register(spyListing(t)))
	m, err := New(Config{
		Clock: testClock(), StoreRoot: t.TempDir(), RawRoot: t.TempDir(), Resolver: r,
		ProviderName: "stooq", Calendar: testUSEquityCalendar(), WriteLock: lock,
	})
	require.NoError(t, err)
	return m
}
