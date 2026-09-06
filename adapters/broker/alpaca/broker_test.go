package alpaca

import (
	"context"
	"testing"
	"time"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDeps(t *testing.T, resolver instrument.Resolver) Deps {
	t.Helper()
	start, err := time.Parse(time.RFC3339, "2026-01-05T15:00:00Z")
	require.NoError(t, err)
	return Deps{
		Clock:    clock.NewSimulated(start),
		IDs:      testIDs(t),
		Resolver: resolver,
	}
}

// testIDs returns a deterministic *id.Generator for tests that need to
// call translate.go's functions directly, outside a full Deps value.
func testIDs(t *testing.T) *id.Generator {
	t.Helper()
	return id.NewGenerator(clock.Real{}, id.NewDeterministic(1, 2))
}

func testBroker(t *testing.T, server *fakeAlpacaServer, listings ...instrument.Listing) *Broker {
	t.Helper()
	client, err := NewClient(ClientConfig{BaseURL: DefaultPaperBaseURL, Credential: StaticCredential{KeyID: "k", SecretKey: "s"}, HTTPClient: server})
	require.NoError(t, err)
	resolver := testResolver(t, listings...)
	deps := testDeps(t, resolver)
	broker, err := NewBroker("alpaca", client, deps, AccountConfig{AccountID: id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")})
	require.NoError(t, err)
	return broker
}

func TestNewBroker_RequiresFields(t *testing.T) {
	client, err := NewClient(ClientConfig{BaseURL: DefaultPaperBaseURL, Credential: StaticCredential{}})
	require.NoError(t, err)
	deps := testDeps(t, testResolver(t))
	cfg := AccountConfig{AccountID: id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")}

	_, err = NewBroker("", client, deps, cfg)
	require.Error(t, err)
	_, err = NewBroker("alpaca", nil, deps, cfg)
	require.Error(t, err)
	_, err = NewBroker("alpaca", client, Deps{}, cfg)
	require.Error(t, err)
	_, err = NewBroker("alpaca", client, deps, AccountConfig{})
	require.Error(t, err)
}

func TestBroker_AccountsAndOpenAccount(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer())

	refs, err := broker.Accounts(context.Background())
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "alpaca", refs[0].Broker)

	acc, err := broker.OpenAccount(context.Background(), refs[0].AccountID)
	require.NoError(t, err)
	assert.Equal(t, refs[0], acc.Reference())

	_, err = broker.OpenAccount(context.Background(), id.MustParseAccountID("acc_01BX5ZZKBKACTAV9WEVGEMMVRZ"))
	assert.ErrorIs(t, err, brokerpkg.ErrAccountNotFound)
}

func TestBroker_CloseThenClosed(t *testing.T) {
	broker := testBroker(t, newFakeAlpacaServer())
	require.NoError(t, broker.Close())
	require.NoError(t, broker.Close()) // idempotent

	_, err := broker.Accounts(context.Background())
	assert.ErrorIs(t, err, brokerpkg.ErrClosed)

	_, err = broker.OpenAccount(context.Background(), id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV"))
	assert.ErrorIs(t, err, brokerpkg.ErrClosed)
}
