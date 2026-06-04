package rest

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rustyeddy/trader/service"
)

// ── POST /api/v1/bots ─────────────────────────────────────────────────────────

func (s *Server) handleStartBot(w http.ResponseWriter, r *http.Request) {
	if !s.requireOANDA(w) {
		return
	}
	var cfg service.BotConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	status, err := s.svc.StartBot(r.Context(), cfg)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("start bot: %v", err))
		return
	}
	writeJSON(w, http.StatusCreated, status)
}

// ── GET /api/v1/bots ──────────────────────────────────────────────────────────

func (s *Server) handleListBots(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.ListBots())
}

// ── GET /api/v1/bots/{id} ─────────────────────────────────────────────────────

func (s *Server) handleGetBot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	status, err := s.svc.GetBot(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// ── DELETE /api/v1/bots/{id} ──────────────────────────────────────────────────

func (s *Server) handleStopBot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.svc.StopBot(id); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "stopped"})
}
