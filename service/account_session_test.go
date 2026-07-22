package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccount_EmptyIDErrors(t *testing.T) {
	svc := &Service{}
	_, err := svc.Account(context.Background(), "")
	require.Error(t, err)
	assert.ErrorContains(t, err, "empty account ID")
}

func TestAccount_CachesSession(t *testing.T) {
	svc := &Service{}
	a1, err := svc.Account(context.Background(), "acc-1")
	require.NoError(t, err)
	a2, err := svc.Account(context.Background(), "acc-1")
	require.NoError(t, err)
	assert.Same(t, a1, a2, "same ID must return the same cached *Account")

	b, err := svc.Account(context.Background(), "acc-2")
	require.NoError(t, err)
	assert.NotSame(t, a1, b, "different IDs must return different sessions")
	assert.Equal(t, "acc-1", a1.ID)
	assert.Equal(t, "acc-2", b.ID)
}

func TestAccount_CacheIsConcurrencySafe(t *testing.T) {
	svc := &Service{}
	const n = 50
	got := make([]*account.Account, n)
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, err := svc.Account(context.Background(), "acc-1")
			require.NoError(t, err)
			got[i] = a
		}(i)
	}
	wg.Wait()
	for i := 1; i < n; i++ {
		assert.Same(t, got[0], got[i], "all concurrent lookups must resolve to one session")
	}
}

func TestAccounts_ListsAndCaches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v3/accounts", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accounts":[{"id":"acc-1"},{"id":"acc-2"}]}`))
	}))
	defer srv.Close()

	svc := &Service{OANDA: &oanda.Client{BaseURL: srv.URL, Token: "test"}}
	accts, err := svc.Accounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accts, 2)
	assert.Equal(t, "acc-1", accts[0].ID)
	assert.Equal(t, "acc-2", accts[1].ID)

	// The returned sessions must be the same cached instances Account returns.
	cached, err := svc.Account(context.Background(), "acc-1")
	require.NoError(t, err)
	assert.Same(t, accts[0], cached)
}

func TestAccounts_NoOANDAErrors(t *testing.T) {
	svc := &Service{}
	_, err := svc.Accounts(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "OANDA client not configured")
}

// TestScopedSummary_TargetsAccountID verifies a scoped Account hits OANDA with
// its own ID, while the Service-level wrapper targets the default account —
// proving two accounts on one Service address distinct OANDA accounts.
func TestScopedSummary_TargetsAccountID(t *testing.T) {
	var mu sync.Mutex
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path is /v3/accounts/{id}/summary — capture the id.
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		mu.Lock()
		requested = append(requested, parts[2])
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"account":{"id":"x","balance":"1000","NAV":"1000","marginUsed":"0","marginAvailable":"1000","unrealizedPL":"0","currency":"USD"}}`))
	}))
	defer srv.Close()

	svc := &Service{
		OANDA:     &oanda.Client{BaseURL: srv.URL, Token: "test"},
		AccountID: "default-acc",
	}

	// Scoped call targets the explicit account.
	other, err := svc.Account(context.Background(), "other-acc")
	require.NoError(t, err)
	_, err = other.GetAccountSummary(context.Background())
	require.NoError(t, err)

	// Service-level wrapper targets the default account.
	_, err = svc.GetAccountSummary(context.Background())
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"other-acc", "default-acc"}, requested)
}

// TestDefaultAccount_AutoResolves confirms defaultAccount discovers the sole
// account when AccountID is unset, mirroring ResolveAccount behaviour.
func TestDefaultAccount_AutoResolves(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accounts":[{"id":"only-acc"}]}`))
	}))
	defer srv.Close()

	svc := &Service{OANDA: &oanda.Client{BaseURL: srv.URL, Token: "test"}}
	acc, err := svc.DefaultAccount(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "only-acc", acc.ID)
	assert.Equal(t, "only-acc", svc.AccountID, "ResolveAccount should set the default")
}
