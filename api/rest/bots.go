package rest

import (
	"encoding/json"
	"fmt"
	"net/http"

	botsvc "github.com/rustyeddy/trader/service/bots"
)

// ── POST /api/v1/bots ─────────────────────────────────────────────────────────

func (s *Server) handleStartBot(w http.ResponseWriter, r *http.Request) {
	acc, ok := s.resolveAccount(w, r)
	if !ok {
		return
	}
	var cfg botsvc.BotConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	status, err := botsvc.StartBotOnAccount(r.Context(), acc, cfg, s.oanda, s.log)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("start bot: %v", err))
		return
	}
	writeJSON(w, http.StatusCreated, status)
}

// ── GET /api/v1/bots ──────────────────────────────────────────────────────────

func (s *Server) handleListBots(w http.ResponseWriter, r *http.Request) {
	acc, ok := s.resolveAccount(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, botsvc.ListBotsForAccount(acc.ID))
}

// ── GET /api/v1/bots/{id} ─────────────────────────────────────────────────────

func (s *Server) handleGetBot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	status, err := botsvc.GetBot(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// ── DELETE /api/v1/bots/{id} ──────────────────────────────────────────────────

func (s *Server) handleStopBot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := botsvc.StopBot(id); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "stopped"})
}
