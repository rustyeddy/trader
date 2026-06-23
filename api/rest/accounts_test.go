package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rustyeddy/trader/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAccountsTestServer builds a Server backed by a fake OANDA endpoint that
// serves the account-list and per-account summary calls, recording every
// summary path so tests can assert which account was targeted. The Service
// has no preset default, so the single listed account becomes the default.
func newAccountsTestServer(t *testing.T, accountIDs ...string) (*Server, *[]string) {
	t.Helper()
	if len(accountIDs) == 0 {
		accountIDs = []string{"acc-1"}
	}
	var mu sync.Mutex
	var summaryHits []string

	mux := http.NewServeMux()
	mux.HandleFunc("/v3/accounts", func(w http.ResponseWriter, r *http.Request) {
		ids := make([]string, len(accountIDs))
		for i, id := range accountIDs {
			ids[i] = `{"id":"` + id + `"}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accounts":[` + strings.Join(ids, ",") + `]}`))
	})
	mux.HandleFunc("/v3/accounts/{id}/summary", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		summaryHits = append(summaryHits, r.PathValue("id"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"account":{"id":"x","balance":"1000","NAV":"1000","marginUsed":"0","marginAvailable":"1000","unrealizedPL":"0","currency":"USD"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	svc, err := service.New(service.Config{Env: "practice", Token: "test-token"})
	require.NoError(t, err)
	svc.OANDA.BaseURL = srv.URL
	return New(svc, ":0"), &summaryHits
}

func TestListAccounts(t *testing.T) {
	srv, _ := newAccountsTestServer(t, "acc-1", "acc-2")
	rr := do(t, srv.Handler(), "GET", "/api/v1/accounts")
	require.Equal(t, http.StatusOK, rr.Code)

	var body struct {
		Accounts []struct {
			ID        string `json:"id"`
			IsDefault bool   `json:"is_default"`
		} `json:"accounts"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(t, body.Accounts, 2)
	assert.Equal(t, "acc-1", body.Accounts[0].ID)
	assert.Equal(t, "acc-2", body.Accounts[1].ID)
	// With no preset default, the first account is the read/UI default.
	assert.True(t, body.Accounts[0].IsDefault)
	assert.False(t, body.Accounts[1].IsDefault)
}

func TestListAccounts_NoOANDA(t *testing.T) {
	srv := newMinimalServer()
	rr := do(t, srv.Handler(), "GET", "/api/v1/accounts")
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

func TestDefaultAccount_SingleAccount(t *testing.T) {
	srv, _ := newAccountsTestServer(t, "only-acc")
	rr := do(t, srv.Handler(), "GET", "/api/v1/accounts/default")
	require.Equal(t, http.StatusOK, rr.Code)

	var body struct {
		ID        string `json:"id"`
		IsDefault bool   `json:"is_default"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "only-acc", body.ID)
	assert.True(t, body.IsDefault)
}

// TestScopedAccountSummary_TargetsPathAccount verifies the scoped route hits
// the account named in the path, not the default.
func TestScopedAccountSummary_TargetsPathAccount(t *testing.T) {
	srv, hits := newAccountsTestServer(t, "default-acc")

	rr := do(t, srv.Handler(), "GET", "/api/v1/accounts/scoped-acc/account")
	require.Equal(t, http.StatusOK, rr.Code)
	require.NotEmpty(t, *hits)
	assert.Equal(t, "scoped-acc", (*hits)[len(*hits)-1],
		"scoped route must query the account from the path")
}

// TestLegacyAccountRouteRemoved verifies the un-scoped account route no longer
// exists — account operations are scoped-only.
func TestLegacyAccountRouteRemoved(t *testing.T) {
	srv, _ := newAccountsTestServer(t, "only-acc")

	rr := do(t, srv.Handler(), "GET", "/api/v1/account")
	assert.Equal(t, http.StatusNotFound, rr.Code,
		"legacy un-scoped /api/v1/account must be gone")
}
