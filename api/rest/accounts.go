package rest

import (
	"fmt"
	"net/http"

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
	if !s.requireOANDA(w) {
		return
	}
	refs, err := accountsvc.ListAccounts(r.Context(), s.svc.OANDA)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("list accounts: %v", err))
		return
	}
	defaultID := accountsvc.DefaultAccountID(refs, s.svc.AccountID)
	out := make([]accountInfo, 0, len(refs))
	for _, ref := range refs {
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
	if !s.requireOANDA(w) {
		return
	}
	refs, err := accountsvc.ListAccounts(r.Context(), s.svc.OANDA)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("default account: %v", err))
		return
	}
	defaultID := accountsvc.DefaultAccountID(refs, s.svc.AccountID)
	if defaultID == "" {
		writeErr(w, http.StatusBadGateway, "default account: no accounts found for this token")
		return
	}
	writeJSON(w, http.StatusOK, accountInfo{ID: defaultID, IsDefault: true})
}
