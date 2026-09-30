package webhook

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type createWebhookTokenRequest struct {
	Name string `json:"name"`
	// Scopes says what the token may do. Omitted means messages:write only, which is what tokens
	// have always done; reading threads has to be asked for with chat:read.
	Scopes []string `json:"scopes,omitempty"`
}

type createWebhookTokenResponse struct {
	Token    *models.WebhookToken `json:"token"`
	APIToken string               `json:"api_token"`
}

func (h *Handler) CreateWebhookToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	var req createWebhookTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	scopes, err := models.NormalizeWebhookScopes(req.Scopes)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
		return
	}

	tokenName := strings.TrimSpace(req.Name)
	token, plainToken, err := h.provider.CreateWebhookToken(r.Context(), userID, tokenName, scopes)
	if err != nil {
		if err == datastore.ErrInvalidRequestBody {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Token name is required", nil)
			return
		}
		h.logger.Error("failed to create webhook token",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to create webhook token", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, createWebhookTokenResponse{
		Token:    token,
		APIToken: plainToken,
	})
}

func (h *Handler) ListWebhookTokens(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	tokens, err := h.provider.ListWebhookTokens(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to list webhook tokens",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list webhook tokens", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, tokens)
}

func (h *Handler) RevokeWebhookToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	tokenID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid token ID", err)
		return
	}

	if err := h.provider.RevokeWebhookToken(r.Context(), userID, tokenID); err != nil {
		if err == datastore.ErrWebhookTokenNotFound {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Webhook token not found", nil)
			return
		}
		h.logger.Error("failed to revoke webhook token",
			zap.String("user_id", userID.String()),
			zap.String("token_id", tokenID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to revoke webhook token", err)
		return
	}

	handlerutils.RespondWithNoContent(w)
}
