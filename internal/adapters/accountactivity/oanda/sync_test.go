package oanda

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/adapters/accountactivity/filestore"
	"github.com/rustyeddy/trader/internal/clock"
	svc "github.com/rustyeddy/trader/internal/service/accountactivity"
)

var importAt = time.Date(2024, 4, 1, 12, 0, 0, 0, time.UTC)

func newFileStore(t *testing.T) (accountactivity.Store, string) {
	t.Helper()
	root := t.TempDir()
	s, err := filestore.New(root)
	require.NoError(t, err)
	return s, root
}

func run(t *testing.T, store accountactivity.Store, f *fakeOANDA, mod ...func(*Config)) (svc.Result, error) {
	t.Helper()
	return svc.Sync(context.Background(), svc.Params{
		Store:  store,
		Source: newSource(t, f, mod...),
		Clock:  clock.NewSimulated(importAt),
	})
}

func query(t *testing.T, store accountactivity.Store) []accountactivity.Activity {
	t.Helper()
	got, err := store.Query(context.Background(), accountactivity.Query{Sync: stream})
	require.NoError(t, err)
	return got
}

func TestSync_InitialImportStoresEveryTransactionAndCheckpoints(t *testing.T) {
	store, _ := newFileStore(t)
	f := loadFixture(t)

	res, err := run(t, store, f)
	require.NoError(t, err)
	assert.Equal(t, 3, res.Pages)
	assert.Equal(t, 12, res.Fetched)
	assert.Equal(t, 12, res.Added)
	assert.Equal(t, "12", res.Cursor)
	assert.Equal(t, 5, res.Other, "three orders, a client configuration and the CFD fill are kept as other events")

	got := query(t, store)
	require.Len(t, got, 12, "nothing is dropped")
	byKind := map[accountactivity.Kind]int{}
	for _, a := range got {
		byKind[a.Kind]++
		assert.True(t, importAt.Equal(a.ImportedAt), "ImportedAt comes from the injected clock")
	}
	assert.Equal(t, map[accountactivity.Kind]int{
		accountactivity.KindFill: 3, accountactivity.KindFinancing: 1, accountactivity.KindDeposit: 1,
		accountactivity.KindWithdrawal: 1, accountactivity.KindDividend: 1, accountactivity.KindOther: 5,
	}, byKind)

	cp, ok, err := store.Checkpoint(context.Background(), stream)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "12", cp.Cursor)
}

func TestSync_RerunningIsHarmlessAndPicksUpOnlyWhatIsNew(t *testing.T) {
	store, _ := newFileStore(t)
	f := loadFixture(t)
	_, err := run(t, store, f)
	require.NoError(t, err)

	// Nothing new: no pages, no change.
	res, err := run(t, store, f)
	require.NoError(t, err)
	assert.Zero(t, res.Pages)
	assert.Zero(t, res.Added)
	assert.Len(t, query(t, store), 12)

	// Two new transactions arrive on the account.
	f.txs = append(f.txs,
		[]byte(`{"id":"13","type":"DAILY_FINANCING","time":"2024-03-08T22:00:00.000000000Z","financing":"-0.9000"}`),
		[]byte(`{"id":"14","type":"TRANSFER_FUNDS","time":"2024-03-09T08:00:00.000000000Z","amount":"250.0000"}`),
	)
	before := len(f.idranges())
	res, err = run(t, store, f)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Pages)
	assert.Equal(t, 2, res.Added)
	assert.Equal(t, "14", res.Cursor)
	assert.Equal(t, []string{"13-14"}, f.idranges()[before:], "incremental sync fetched only the new range")
	assert.Len(t, query(t, store), 14)
}

// Wiping the checkpoint and re-importing everything creates no duplicates
// and changes nothing already stored.
func TestSync_ReimportingFromScratchIsIdempotent(t *testing.T) {
	store, root := newFileStore(t)
	f := loadFixture(t)
	_, err := run(t, store, f)
	require.NoError(t, err)
	before, err := os.ReadFile(filepath.Join(root, "oanda", "practice", testAccount, "activities.jsonl"))
	require.NoError(t, err)

	require.NoError(t, os.Remove(filepath.Join(root, "oanda", "practice", testAccount, "checkpoint.json")))
	res, err := run(t, store, f)
	require.NoError(t, err)
	assert.Equal(t, 12, res.Unchanged)
	assert.Zero(t, res.Added)

	after, err := os.ReadFile(filepath.Join(root, "oanda", "practice", testAccount, "activities.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
}

// failingStore fails Apply for chosen calls, as a crash or full disk would.
type failingStore struct {
	accountactivity.Store
	calls  int
	failOn map[int]bool
}

func (s *failingStore) Apply(ctx context.Context, b accountactivity.Batch) (accountactivity.ApplyResult, error) {
	s.calls++
	if s.failOn[s.calls] {
		return accountactivity.ApplyResult{}, errors.New("injected store failure")
	}
	return s.Store.Apply(ctx, b)
}

// A failure before the checkpoint advances is safely retryable: the
// checkpoint is at the last stored page and the retry converges.
func TestSync_FailureBeforeCheckpointAdvanceIsRetryable(t *testing.T) {
	inner, _ := newFileStore(t)
	store := &failingStore{Store: inner, failOn: map[int]bool{2: true}}
	f := loadFixture(t)

	res, err := run(t, store, f)
	require.ErrorContains(t, err, "injected store failure")
	assert.Equal(t, 1, res.Pages)

	cp, ok, err := inner.Checkpoint(context.Background(), stream)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "5", cp.Cursor, "the checkpoint stopped at the last durable page")
	assert.Len(t, query(t, inner), 5)

	before := len(f.idranges())
	res, err = run(t, store, f)
	require.NoError(t, err)
	assert.Equal(t, "12", res.Cursor)
	assert.Equal(t, []string{"6-10", "11-12"}, f.idranges()[before:], "the retry resumed after the checkpoint")
	assert.Len(t, query(t, inner), 12, "no duplicates after the retry")
}

// A fetch failure mid-run leaves earlier pages stored and the checkpoint on them.
func TestSync_FetchFailureMidRunKeepsEarlierPagesAndResumes(t *testing.T) {
	store, _ := newFileStore(t)
	f := loadFixture(t)
	f.txs[7] = []byte(`{"id":"8","type":"TRANSFER_FUNDS","time":"2024-03-07T08:00:00Z","amount":"oops"}`)

	res, err := run(t, store, f)
	require.Error(t, err)
	assert.Equal(t, 1, res.Pages)
	cp, _, err := store.Checkpoint(context.Background(), stream)
	require.NoError(t, err)
	assert.Equal(t, "5", cp.Cursor, "a page with an unparseable recognized transaction is not stored")
	assert.Len(t, query(t, store), 5)

	// Fixed upstream data: the retry completes.
	f.txs[7] = []byte(`{"id":"8","type":"TRANSFER_FUNDS","time":"2024-03-07T08:00:00Z","amount":"-500.0000"}`)
	res, err = run(t, store, f)
	require.NoError(t, err)
	assert.Equal(t, "12", res.Cursor)
	assert.Len(t, query(t, store), 12)
}

func TestSync_BoundedBootstrapThenIncrementalContinues(t *testing.T) {
	store, _ := newFileStore(t)
	f := loadFixture(t)
	f.window = [][2]int64{{1, 6}}
	bounds := Bounds{From: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 3, 6, 12, 0, 0, 0, time.UTC)}

	res, err := run(t, store, f, func(c *Config) { c.Bounds = bounds })
	require.NoError(t, err)
	assert.Equal(t, "6", res.Cursor)
	assert.Len(t, query(t, store), 6, "only the bounded window was imported")

	// An unbounded follow-up run continues from the checkpoint.
	res, err = run(t, store, f)
	require.NoError(t, err)
	assert.Equal(t, "12", res.Cursor)
	assert.Equal(t, 6, res.Added)
	assert.Len(t, query(t, store), 12)
}

// The token never reaches the store or an error.
func TestSync_NoSecretInStoredDataOrErrors(t *testing.T) {
	store, root := newFileStore(t)
	f := loadFixture(t)
	_, err := run(t, store, f)
	require.NoError(t, err)
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		require.NoError(t, err)
		if !d.IsDir() {
			data, err := os.ReadFile(p)
			require.NoError(t, err)
			assert.NotContains(t, string(data), testToken)
		}
		return nil
	})
	require.NoError(t, err)

	f.status = 401
	_, err = run(t, store, f)
	require.ErrorIs(t, err, ErrUnauthorized)
	assert.NotContains(t, err.Error(), testToken)
}

// A response that lost its transactions array must not move the checkpoint
// past history that was never fetched.
func TestSync_AMalformedPageLeavesTheCheckpointUnchanged(t *testing.T) {
	store, _ := newFileStore(t)
	f := loadFixture(t)
	_, err := run(t, store, f, func(c *Config) { c.PageSize = 5 })
	require.NoError(t, err)

	f.txs = append(f.txs, []byte(`{"id":"13","type":"DAILY_FINANCING","time":"2024-03-08T22:00:00Z","financing":"-0.9000"}`))
	f.idrangeBody = `{}`
	_, err = run(t, store, f)
	require.ErrorIs(t, err, ErrBadResponse)

	cp, _, err := store.Checkpoint(context.Background(), stream)
	require.NoError(t, err)
	assert.Equal(t, "12", cp.Cursor, "the checkpoint did not move past undelivered history")
	assert.Len(t, query(t, store), 12)

	f.idrangeBody = ""
	res, err := run(t, store, f)
	require.NoError(t, err)
	assert.Equal(t, "13", res.Cursor)
}

// Batch IDs and provider instrument names survive import and storage.
func TestSync_BatchIDsAndProviderInstrumentsRoundTripThroughTheStore(t *testing.T) {
	store, _ := newFileStore(t)
	_, err := run(t, store, loadFixture(t))
	require.NoError(t, err)

	byID := map[string]accountactivity.Activity{}
	for _, a := range query(t, store) {
		byID[a.ProviderActivityID] = a
	}
	assert.Equal(t, "2", byID["2"].BrokerBatchID)
	assert.Equal(t, "2", byID["3"].BrokerBatchID, "an order and its fill still share a batch after storage")
	assert.Equal(t, "6", byID["6"].BrokerBatchID)
	assert.Equal(t, "6", byID["7"].BrokerBatchID)
	assert.Equal(t, "EUR_USD", byID["3"].ProviderInstrument)
	assert.Equal(t, "SPX500_USD", byID["10"].ProviderInstrument, "the unmapped instrument is kept for later re-normalization")
	assert.Equal(t, accountactivity.KindOther, byID["10"].Kind)
}
