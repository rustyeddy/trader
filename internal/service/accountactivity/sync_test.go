package accountactivity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/accountactivity/storetest"
	"github.com/rustyeddy/trader/internal/adapters/accountactivity/filestore"
	"github.com/rustyeddy/trader/internal/clock"
	svc "github.com/rustyeddy/trader/internal/service/accountactivity"
	"github.com/rustyeddy/trader/num"
)

var now = time.Date(2024, 4, 1, 12, 0, 0, 0, time.UTC)

// scripted is a Source that returns pages by the cursor it is given.
type scripted struct {
	pages map[string]accountactivity.Page
	err   error
	calls []string
}

func (s *scripted) Stream() accountactivity.SyncKey { return storetest.Sync }

func (s *scripted) Next(_ context.Context, cursor string) (accountactivity.Page, error) {
	s.calls = append(s.calls, cursor)
	if s.err != nil {
		return accountactivity.Page{}, s.err
	}
	p, ok := s.pages[cursor]
	if !ok {
		return accountactivity.Page{Cursor: cursor, Done: true}, nil
	}
	// Source output has no ImportedAt; the loop stamps it.
	acts := make([]accountactivity.Activity, len(p.Activities))
	for i, a := range p.Activities {
		a.ImportedAt = time.Time{}
		acts[i] = a
	}
	p.Activities = acts
	return p, nil
}

func newStore(t *testing.T) accountactivity.Store {
	t.Helper()
	s, err := filestore.New(t.TempDir())
	require.NoError(t, err)
	return s
}

func params(store accountactivity.Store, src accountactivity.Source) svc.Params {
	return svc.Params{Store: store, Source: src, Clock: clock.NewSimulated(now)}
}

func dep(id string, h int) accountactivity.Activity {
	return storetest.Deposit(id, time.Duration(h)*time.Hour)
}

func otherEvent(id string, h int) accountactivity.Activity {
	a := dep(id, h)
	a.Kind, a.Cash, a.ProviderType = accountactivity.KindOther, accountactivity.Value[num.Money]{}, "CLIENT_CONFIGURE"
	return a
}

func TestSync_StampsImportedAtFromTheInjectedClockAndCountsEverything(t *testing.T) {
	store := newStore(t)
	src := &scripted{pages: map[string]accountactivity.Page{
		"":  {Activities: []accountactivity.Activity{dep("1", 0), otherEvent("2", 1)}, Cursor: "2"},
		"2": {Activities: []accountactivity.Activity{dep("3", 2)}, Cursor: "3", Done: true},
	}}

	res, err := svc.Sync(context.Background(), params(store, src))
	require.NoError(t, err)
	assert.Equal(t, svc.Result{Pages: 2, Fetched: 3, Added: 3, Other: 1, Cursor: "3"}, res)
	assert.Equal(t, []string{"", "2"}, src.calls)

	got, err := store.Query(context.Background(), accountactivity.Query{Sync: storetest.Sync})
	require.NoError(t, err)
	require.Len(t, got, 3)
	for _, a := range got {
		assert.True(t, now.Equal(a.ImportedAt), "ImportedAt is the injected clock, not the wall clock")
	}
	cp, ok, err := store.Checkpoint(context.Background(), storetest.Sync)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, now.Equal(cp.UpdatedAt))
}

func TestSync_StartsFromTheDurableCheckpoint(t *testing.T) {
	store := newStore(t)
	src := &scripted{pages: map[string]accountactivity.Page{
		"": {Activities: []accountactivity.Activity{dep("1", 0)}, Cursor: "1", Done: true},
	}}
	_, err := svc.Sync(context.Background(), params(store, src))
	require.NoError(t, err)

	src2 := &scripted{pages: map[string]accountactivity.Page{
		"1": {Activities: []accountactivity.Activity{dep("2", 1)}, Cursor: "2", Done: true},
	}}
	res, err := svc.Sync(context.Background(), params(store, src2))
	require.NoError(t, err)
	assert.Equal(t, []string{"1"}, src2.calls, "the source is asked for what follows the checkpoint")
	assert.Equal(t, 1, res.Added)
	assert.Equal(t, "2", res.Cursor)
}

func TestSync_NothingNewAppliesNothing(t *testing.T) {
	store := newStore(t)
	src := &scripted{pages: map[string]accountactivity.Page{}} // unknown cursor: empty and Done
	res, err := svc.Sync(context.Background(), params(store, src))
	require.NoError(t, err)
	assert.Zero(t, res.Pages)
	_, ok, err := store.Checkpoint(context.Background(), storetest.Sync)
	require.NoError(t, err)
	assert.False(t, ok, "an empty run does not invent a checkpoint")
}

func TestSync_ASourceThatStallsIsAnError(t *testing.T) {
	cases := map[string]accountactivity.Page{
		"empty, unchanged, not done":    {Cursor: ""},
		"activities but cursor unmoved": {Activities: []accountactivity.Activity{dep("1", 0)}, Cursor: ""},
	}
	for name, page := range cases {
		t.Run(name, func(t *testing.T) {
			src := &scripted{pages: map[string]accountactivity.Page{"": page}}
			_, err := svc.Sync(context.Background(), params(newStore(t), src))
			assert.ErrorIs(t, err, svc.ErrNoProgress)
		})
	}
}

func TestSync_SourceFailureLeavesTheCheckpointOnTheLastStoredPage(t *testing.T) {
	store := newStore(t)
	boom := errors.New("provider down")
	src := &scripted{pages: map[string]accountactivity.Page{
		"": {Activities: []accountactivity.Activity{dep("1", 0)}, Cursor: "1"},
	}}
	// Page two fails to fetch.
	failing := &failAfter{scripted: src, after: 1, err: boom}
	res, err := svc.Sync(context.Background(), params(store, failing))
	require.ErrorIs(t, err, boom)
	assert.Equal(t, 1, res.Pages)
	assert.Equal(t, "1", res.Cursor)

	cp, ok, err := store.Checkpoint(context.Background(), storetest.Sync)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "1", cp.Cursor)
}

type failAfter struct {
	*scripted
	after int
	err   error
}

func (f *failAfter) Next(ctx context.Context, cursor string) (accountactivity.Page, error) {
	if len(f.calls) >= f.after {
		f.calls = append(f.calls, cursor)
		return accountactivity.Page{}, f.err
	}
	return f.scripted.Next(ctx, cursor)
}

func TestSync_RejectsMissingCollaboratorsAndHonoursCancellation(t *testing.T) {
	_, err := svc.Sync(context.Background(), svc.Params{})
	assert.Error(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := &scripted{pages: map[string]accountactivity.Page{"": {Activities: []accountactivity.Activity{dep("1", 0)}, Cursor: "1"}}}
	_, err = svc.Sync(ctx, params(newStore(t), src))
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, src.calls, "nothing is fetched once cancelled")
}
