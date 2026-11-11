package marketdata

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMergeStooqArchive_RequiresConfiguredStooqManager(t *testing.T) {
	_, err := (&Manager{}).MergeStooqArchive(context.Background(), "unused", spyID(t))
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestMergeStooqArchive_RejectsOtherProviders(t *testing.T) {
	m, err := New(Config{Clock: testClock(), StoreRoot: t.TempDir(), RawRoot: t.TempDir(), Resolver: testResolver(t), ProviderName: "oanda"})
	require.NoError(t, err)
	_, err = m.MergeStooqArchive(context.Background(), "unused", spyID(t))
	require.ErrorContains(t, err, "requires provider stooq")
}

func TestMergeStooqArchive_WaitsForWriteLock(t *testing.T) {
	lock := NewWriteLock()
	require.NoError(t, lock.acquire(context.Background()))
	defer lock.release()
	m := newStooqTestManagerWithLock(t, lock)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := m.MergeStooqArchive(ctx, "unused", spyID(t))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
