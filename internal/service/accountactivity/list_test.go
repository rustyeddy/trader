package accountactivity_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/accountactivity/storetest"
	svc "github.com/rustyeddy/trader/internal/service/accountactivity"
)

func seeded(t *testing.T, n int) accountactivity.Store {
	t.Helper()
	store := newStore(t)
	var as []accountactivity.Activity
	for i := 1; i <= n; i++ {
		as = append(as, storetest.Deposit(string(rune('0'+i)), time.Duration(i)*time.Hour))
	}
	_, err := store.Apply(context.Background(), accountactivity.Batch{Sync: storetest.Sync, Activities: as, Cursor: "c", At: now})
	require.NoError(t, err)
	return store
}

func TestList_BoundedByTimeAndLimit(t *testing.T) {
	store := seeded(t, 5)
	ctx := context.Background()
	base := storetest.Deposit("x", 0).OccurredAt

	all, err := svc.List(ctx, svc.ListParams{Store: store, Stream: storetest.Sync})
	require.NoError(t, err)
	assert.Len(t, all.Activities, 5)
	assert.False(t, all.Truncated())
	assert.Equal(t, svc.DefaultListLimit, all.Limit)
	assert.True(t, all.HasCheckpoint)
	assert.Equal(t, "c", all.Checkpoint.Cursor)

	// [from, to) is half-open: the activity exactly at To is out.
	r, err := svc.List(ctx, svc.ListParams{Store: store, Stream: storetest.Sync, From: base.Add(2 * time.Hour), To: base.Add(4 * time.Hour)})
	require.NoError(t, err)
	assert.Equal(t, []string{"2", "3"}, ids(r.Activities))

	// The limit keeps the earliest and says it hid the rest.
	r, err = svc.List(ctx, svc.ListParams{Store: store, Stream: storetest.Sync, Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2"}, ids(r.Activities))
	assert.Equal(t, 5, r.Matched)
	assert.True(t, r.Truncated())

	// An empty range is an empty slice, not nil, and not an error.
	r, err = svc.List(ctx, svc.ListParams{Store: store, Stream: storetest.Sync, From: base.Add(100 * time.Hour)})
	require.NoError(t, err)
	assert.NotNil(t, r.Activities)
	assert.Empty(t, r.Activities)
}

func TestList_RefusesBadInput(t *testing.T) {
	store := seeded(t, 1)
	_, err := svc.List(context.Background(), svc.ListParams{Store: store, Stream: storetest.Sync, Limit: -1})
	assert.Error(t, err)
	_, err = svc.List(context.Background(), svc.ListParams{Stream: storetest.Sync})
	assert.Error(t, err)
	base := storetest.Deposit("x", 0).OccurredAt
	_, err = svc.List(context.Background(), svc.ListParams{Store: store, Stream: storetest.Sync, From: base.Add(time.Hour), To: base})
	assert.ErrorIs(t, err, accountactivity.ErrInvalidQuery)
}

func TestList_NoCheckpointBeforeAnySync(t *testing.T) {
	r, err := svc.List(context.Background(), svc.ListParams{Store: newStore(t), Stream: storetest.Sync})
	require.NoError(t, err)
	assert.False(t, r.HasCheckpoint)
	assert.Empty(t, r.Activities)
}

func ids(as []accountactivity.Activity) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.ProviderActivityID
	}
	return out
}
