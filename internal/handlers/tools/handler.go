package tools

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"go.uber.org/zap"
)

// Handler serves tool metadata to the frontend.
type Handler struct {
	logger *zap.Logger
	// firstPartyWebSearch selects the web_search description (ADR 0x021).
	firstPartyWebSearch bool
}

func NewHandler(logger *zap.Logger, firstPartyWebSearch bool) *Handler {
	return &Handler{logger: logger, firstPartyWebSearch: firstPartyWebSearch}
}

func (h *Handler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/tools", h.ListTools).Methods("GET")
}

// ListTools returns agent tool metadata for the current user.
func (h *Handler) ListTools(w http.ResponseWriter, r *http.Request) {
	tools := agent.GetAvailableTools(r.Context(), h.firstPartyWebSearch)
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, tools)
}
