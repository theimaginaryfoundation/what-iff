package chat

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// LoadedSkill is one skill the agent loaded in a thread (ADR 0x035 §4), for the Tools panel.
type LoadedSkill struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

// ListChatLoadedSkills handles GET /chat/{chatId}/loaded-skills: the skills the agent loaded in
// the thread with load_skill, oldest load first. Skills a mode or a message preloads are not loaded
// state and are not listed.
func (h *Handler) ListChatLoadedSkills(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	chatID, ok := parseChatIDVar(w, h.logger, r, "chatId")
	if !ok {
		return
	}
	ids, err := h.ds.ListChatLoadedSkillIDs(r.Context(), userID, chatID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	}
	if err != nil {
		h.logger.Error("failed to list the chat's loaded skills", zap.String("chat_id", chatID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list loaded skills", err)
		return
	}
	out := make([]LoadedSkill, 0, len(ids))
	if len(ids) > 0 {
		rituals, err := h.ds.GetRitualsByIDs(r.Context(), userID, ids)
		if err != nil {
			h.logger.Error("failed to load the chat's loaded skills", zap.String("chat_id", chatID.String()), zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list loaded skills", err)
			return
		}
		byID := make(map[uuid.UUID]*models.Ritual, len(rituals))
		for _, rt := range rituals {
			byID[rt.ID] = rt
		}
		// Keep the load order; a row for a deleted skill is skipped (the next unload sweep drops it).
		for _, id := range ids {
			if rt, ok := byID[id]; ok {
				out = append(out, LoadedSkill{ID: rt.ID, Slug: rt.Slug, Name: rt.Name, Description: rt.Description})
			}
		}
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, out)
}

// UnloadChatSkill handles DELETE /chat/{chatId}/loaded-skills/{ritualId}: forgets a skill the
// agent loaded, so its instructions leave the thread's context from the next turn.
func (h *Handler) UnloadChatSkill(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	chatID, ok := parseChatIDVar(w, h.logger, r, "chatId")
	if !ok {
		return
	}
	ritualID, err := uuid.Parse(mux.Vars(r)["ritualId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid skill ID", err)
		return
	}
	_, err = h.ds.RemoveChatLoadedSkills(r.Context(), userID, chatID, []uuid.UUID{ritualID})
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	}
	if err != nil {
		h.logger.Error("failed to unload a skill", zap.String("chat_id", chatID.String()), zap.String("ritual_id", ritualID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to unload skill", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
