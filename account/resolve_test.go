package account

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Account IDs in this file are prefixed to avoid colliding with other
// tests in this package that also touch the shared package-level cache.

func TestResolve_EmptyIDErrors(t *testing.T) {
	_, err := Resolve(context.Background(), "", nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "empty account ID")
}

func TestResolve_CachesSession(t *testing.T) {
	a1, err := Resolve(context.Background(), "resolve-acc-1", nil, nil)
	require.NoError(t, err)
	a2, err := Resolve(context.Background(), "resolve-acc-1", nil, nil)
	require.NoError(t, err)
	assert.Same(t, a1, a2, "same ID must return the same cached *Account")

	b, err := Resolve(context.Background(), "resolve-acc-2", nil, nil)
	require.NoError(t, err)
	assert.NotSame(t, a1, b, "different IDs must return different sessions")
	assert.Equal(t, "resolve-acc-1", a1.ID)
	assert.Equal(t, "resolve-acc-2", b.ID)
}

func TestResolve_CacheIsConcurrencySafe(t *testing.T) {
	const n = 50
	got := make([]*Account, n)
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, err := Resolve(context.Background(), "resolve-acc-concurrent", nil, nil)
			require.NoError(t, err)
			got[i] = a
		}(i)
	}
	wg.Wait()
	for i := 1; i < n; i++ {
		assert.Same(t, got[0], got[i], "all concurrent lookups must resolve to one session")
	}
}

func TestResolveAll_ListsAndCaches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v3/accounts", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accounts":[{"id":"resolve-all-acc-1"},{"id":"resolve-all-acc-2"}]}`))
	}))
	defer srv.Close()

	client := &oanda.Client{BaseURL: srv.URL, Token: "test"}
	accts, err := ResolveAll(context.Background(), client, nil)
	require.NoError(t, err)
	require.Len(t, accts, 2)
	assert.Equal(t, "resolve-all-acc-1", accts[0].ID)
	assert.Equal(t, "resolve-all-acc-2", accts[1].ID)

	// The returned sessions must be the same cached instances Resolve returns.
	cached, err := Resolve(context.Background(), "resolve-all-acc-1", client, nil)
	require.NoError(t, err)
	assert.Same(t, accts[0], cached)
}

func TestResolveAll_NilClientErrors(t *testing.T) {
	_, err := ResolveAll(context.Background(), nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "OANDA client not configured")
}

// TestResolve_ScopedSummaryTargetsAccountID verifies a scoped Resolve hits
// OANDA with its own ID, not some other cached session's.
func TestResolve_ScopedSummaryTargetsAccountID(t *testing.T) {
	var mu sync.Mutex
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		mu.Lock()
		requested = append(requested, parts[2])
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"account":{"id":"x","balance":"1000","NAV":"1000","marginUsed":"0","marginAvailable":"1000","unrealizedPL":"0","currency":"USD"}}`))
	}))
	defer srv.Close()

	client := &oanda.Client{BaseURL: srv.URL, Token: "test"}
	other, err := Resolve(context.Background(), "resolve-scoped-other-acc", client, nil)
	require.NoError(t, err)
	_, err = other.GetAccountSummary(context.Background())
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"resolve-scoped-other-acc"}, requested)
}

// TestResolveFirst_AutoDiscoversSoleAccount confirms ResolveFirst discovers
// the sole account when defaultID is empty.
func TestResolveFirst_AutoDiscoversSoleAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accounts":[{"id":"resolve-first-only-acc"}]}`))
	}))
	defer srv.Close()

	client := &oanda.Client{BaseURL: srv.URL, Token: "test"}
	acc, err := ResolveFirst(context.Background(), "", client, nil)
	require.NoError(t, err)
	assert.Equal(t, "resolve-first-only-acc", acc.ID)
}

func TestResolveFirst_UsesDefaultIDWhenSet(t *testing.T) {
	acc, err := ResolveFirst(context.Background(), "resolve-first-configured-acc", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "resolve-first-configured-acc", acc.ID)
}
