package rest

import (
	"fmt"
	"net/http"
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
	accts, err := s.svc.Accounts(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("list accounts: %v", err))
		return
	}
	def, _ := s.svc.FirstAccount(r.Context())
	out := make([]accountInfo, 0, len(accts))
	for _, a := range accts {
		out = append(out, accountInfo{
			ID:        a.ID,
			IsDefault: def != nil && a.ID == def.ID,
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
	acc, err := s.svc.FirstAccount(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("default account: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, accountInfo{ID: acc.ID, IsDefault: true})
}
