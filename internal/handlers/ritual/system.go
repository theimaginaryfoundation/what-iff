package ritual

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// ListSystemRituals returns the system rituals available to all users.
//
// These rituals are not stored in the database and are intended to be UI-visible helpers that
// enable special backend behavior (e.g. custom image generation).
func (h *Handler) ListSystemRituals(w http.ResponseWriter, r *http.Request) {
	// Keep auth consistent with other ritual endpoints.
	_, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	now := time.Now().UTC()
	resp := agent.ListSystemRituals(now)

	userID, _ := middleware.GetUserIDFromContext(r.Context())
	h.overlayUserBindings(r.Context(), userID, resp)

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, resp)
}

// Overlay user bindings onto the given rituals in place.
func (h *Handler) overlayUserBindings(ctx context.Context, userID uuid.UUID, rituals []models.Ritual) {
	if userID == uuid.Nil {
		return
	}
	bindings, err := h.getBindingStore().GetSystemBindingsForUser(ctx, userID)
	if err != nil {
		h.logger.Warn("failed to load system ritual bindings", zap.Error(err))
		return
	}
	if len(bindings) == 0 {
		return
	}
	bindingMap := make(map[uuid.UUID]string, len(bindings))
	for _, b := range bindings {
		bindingMap[b.RitualID] = b.Hotkeys
	}
	for i := range rituals {
		if hk, ok := bindingMap[rituals[i].ID]; ok {
			rituals[i].Hotkeys = hk
		}
	}
}
