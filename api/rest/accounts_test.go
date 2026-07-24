package rest

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rustyeddy/trader/brokers/oanda"
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

	client, err := oanda.NewClient("practice", "test-token")
	require.NoError(t, err)
	client.BaseURL = srv.URL
	return New(client, slog.Default(), "", nil, ":0"), &summaryHits
}

// handleListAccounts/handleDefaultAccount now call accountsvc.List, which
// builds its own broker from OANDA_TOKEN/~/.config/oanda/pat.txt — there is
// no seam left to point it at a fake server (see newAccountsTestServer's
// remaining uses below for routes that still resolve through s.oanda). So
// these two only exercise the deterministic auth-failure path; success-path
// coverage for a real token is opt-in/manual.

func TestListAccounts_MissingToken(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("HOME", t.TempDir()) // block the ~/.config/oanda/pat.txt fallback
	srv := newMinimalServer()
	rr := do(t, srv.Handler(), "GET", "/api/v1/accounts")
	assert.Equal(t, http.StatusBadGateway, rr.Code)
	assert.Contains(t, rr.Body.String(), "no token")
}

func TestDefaultAccount_MissingToken(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	srv := newMinimalServer()
	rr := do(t, srv.Handler(), "GET", "/api/v1/accounts/default")
	assert.Equal(t, http.StatusBadGateway, rr.Code)
	assert.Contains(t, rr.Body.String(), "no token")
}

func TestAccountSummary_MissingToken(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	srv := newMinimalServer()
	rr := do(t, srv.Handler(), "GET", "/api/v1/accounts/summary")
	assert.Equal(t, http.StatusBadGateway, rr.Code)
	assert.Contains(t, rr.Body.String(), "no token")
}

func TestAccountOrders_MissingToken(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	srv := newMinimalServer()
	rr := do(t, srv.Handler(), "GET", "/api/v1/accounts/orders")
	assert.Equal(t, http.StatusBadGateway, rr.Code)
	assert.Contains(t, rr.Body.String(), "no token")
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
