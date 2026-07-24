package rest

import (
	"fmt"
	"net/http"

	"github.com/rustyeddy/trader/brokers/oanda"
	accountsvc "github.com/rustyeddy/trader/service/account"
)

// accountInfo is the public view of an account in the listing.
type accountInfo struct {
	ID        string `json:"id"`
	IsDefault bool   `json:"is_default"`
}

// ── GET /api/v1/accounts ──────────────────────────────────────────────────

// handleListAccounts returns every account the configured token can access,
// flagging the server's default. The UI account dropdown is built from this.
func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	result, err := accountsvc.List(r.Context(), accountsvc.AccountCfg{Broker: "oanda"})
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("list accounts: %v", err))
		return
	}
	defaultID := accountsvc.DefaultAccountID(result.Accounts, s.accountID)
	out := make([]accountInfo, 0, len(result.Accounts))
	for _, ref := range result.Accounts {
		out = append(out, accountInfo{
			ID:        ref.ID,
			IsDefault: ref.ID == defaultID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

// ── GET /api/v1/accounts/default ──────────────────────────────────────────

// handleDefaultAccount returns the server's default account ID — the one the
// UI selects on load and legacy (un-scoped) routes operate on.
func (s *Server) handleDefaultAccount(w http.ResponseWriter, r *http.Request) {
	result, err := accountsvc.List(r.Context(), accountsvc.AccountCfg{Broker: "oanda"})
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("default account: %v", err))
		return
	}
	defaultID := accountsvc.DefaultAccountID(result.Accounts, s.accountID)
	if defaultID == "" {
		writeErr(w, http.StatusBadGateway, "default account: no accounts found for this token")
		return
	}
	writeJSON(w, http.StatusOK, accountInfo{ID: defaultID, IsDefault: true})
}

// ── GET /api/v1/accounts/summary ──────────────────────────────────────────

// accountSummaryEntry is the public view of one account's summary in
// handleAccountSummary's response — mirrors `trader account summary`'s
// per-row output, including per-account fetch errors.
type accountSummaryEntry struct {
	ID      string                `json:"id"`
	Summary *oanda.AccountSummary `json:"summary,omitempty"`
	Error   string                `json:"error,omitempty"`
}

// handleAccountSummary returns balance/NAV/margin/P&L for one account (via
// ?account_id=) or every account the configured token can see if omitted —
// mirrors `trader account summary`.
func (s *Server) handleAccountSummary(w http.ResponseWriter, r *http.Request) {
	results, err := accountsvc.Summary(r.Context(), accountCfgFromQuery(r))
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("account summary: %v", err))
		return
	}
	out := make([]accountSummaryEntry, 0, len(results))
	for _, result := range results {
		entry := accountSummaryEntry{ID: result.ID}
		if result.Err != nil {
			entry.Error = result.Err.Error()
		} else {
			entry.Summary = result.Summary
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

// ── GET /api/v1/accounts/orders ───────────────────────────────────────────

// handleAccountOrders returns open trades on the resolved account —
// mirrors `trader account orders`.
func (s *Server) handleAccountOrders(w http.ResponseWriter, r *http.Request) {
	trades, err := accountsvc.Orders(r.Context(), accountCfgFromQuery(r))
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("account orders: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, trades)
}

// accountCfgFromQuery builds an accountsvc.AccountCfg from the optional
// ?account_id= query param, mirroring cmd/account's --account-id flag.
func accountCfgFromQuery(r *http.Request) accountsvc.AccountCfg {
	accountID := r.URL.Query().Get("account_id")
	return accountsvc.AccountCfg{
		Broker:           "oanda",
		AccountID:        accountID,
		AccountIDChanged: accountID != "",
	}
}
