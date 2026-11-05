// Package utilitymodel serves an account's choice of model for each utility
// role: the background work, such as memory extraction or chat naming, that
// isn't the assistant talking (ADR 0x027 section 6).
package utilitymodel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Store is what the handler reads and writes.
type Store interface {
	ListUtilityModelSettings(ctx context.Context, userID uuid.UUID) ([]models.UtilityModelSetting, error)
	SetUtilityModel(ctx context.Context, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole, modelID uuid.UUID) error
	ClearUtilityModel(ctx context.Context, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole) error
}

// Handler serves the utility-model routes.
type Handler struct {
	store Store
	// builtIn names the model a role runs on when nothing is chosen.
	builtIn func(models.UtilityRole) string
	logger  *zap.Logger
}

// NewHandler builds the handler.
func NewHandler(store Store, builtIn func(models.UtilityRole) string, logger *zap.Logger) *Handler {
	return &Handler{store: store, builtIn: builtIn, logger: logger}
}

// RegisterRoutes mounts the routes on an authenticated router.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/utility-models", h.List).Methods(http.MethodGet)
	router.HandleFunc("/utility-models/{role}", h.Set).Methods(http.MethodPut)
	router.HandleFunc("/utility-models/{role}", h.Clear).Methods(http.MethodDelete)
}

// List reports each role's models for the requesting account.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	out, err := h.utilityModels(r.Context(), userID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error fetching utility models", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, out)
}

// Set chooses the account's model for a role, from the models it can use.
func (h *Handler) Set(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := h.target(w, r)
	if !ok {
		return
	}
	var req models.SetUtilityModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ModelID == uuid.Nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "model_id is required", err)
		return
	}
	err := h.store.SetUtilityModel(r.Context(), models.UtilityScopeAccount, userID, role, req.ModelID)
	if errors.Is(err, datastore.ErrExperimentalModelNotAllowed) {
		handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, handlerutils.CodeNotSet, "Experimental models are not enabled for this account", nil)
		return
	}
	if errors.Is(err, datastore.ErrModelNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "That model is not available", err)
		return
	}
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error saving the utility model", err)
		return
	}
	h.respondWith(w, r, userID)
}

// Clear removes the account's choice for a role, so it runs on the deployment's
// choice or its built-in model.
func (h *Handler) Clear(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := h.target(w, r)
	if !ok {
		return
	}
	if err := h.store.ClearUtilityModel(r.Context(), models.UtilityScopeAccount, userID, role); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error clearing the utility model", err)
		return
	}
	h.respondWith(w, r, userID)
}

// target reads the account and the role a write is for, answering for itself
// when either is missing.
func (h *Handler) target(w http.ResponseWriter, r *http.Request) (uuid.UUID, models.UtilityRole, bool) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return uuid.Nil, "", false
	}
	role := models.UtilityRole(mux.Vars(r)["role"])
	if !role.Valid() {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Unknown utility role", nil)
		return uuid.Nil, "", false
	}
	return userID, role, true
}

// respondWith answers a write with every role's models after it.
func (h *Handler) respondWith(w http.ResponseWriter, r *http.Request, userID uuid.UUID) {
	out, err := h.utilityModels(r.Context(), userID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error fetching utility models", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, out)
}

func (h *Handler) utilityModels(ctx context.Context, userID uuid.UUID) (models.UtilityModels, error) {
	settings, err := h.store.ListUtilityModelSettings(ctx, userID)
	if err != nil {
		return models.UtilityModels{}, err
	}
	out := models.UtilityModels{Roles: make([]models.UtilityRoleModel, 0, len(models.UtilityRoles))}
	for _, role := range models.UtilityRoles {
		entry := models.UtilityRoleModel{Role: role, BuiltInModel: h.builtIn(role)}
		for _, s := range settings {
			if s.Role != role {
				continue
			}
			id := s.ModelID
			switch s.Scope {
			case models.UtilityScopeAccount:
				entry.AccountModelID = &id
			case models.UtilityScopeDeployment:
				entry.DeploymentModelID = &id
			}
		}
		out.Roles = append(out.Roles, entry)
	}
	return out, nil
}
