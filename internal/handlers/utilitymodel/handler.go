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
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

// Store is what the handler reads and writes.
type Store interface {
	ListUtilityModelSettings(ctx context.Context, userID uuid.UUID) ([]models.UtilityModelSetting, error)
	SetUtilityModel(ctx context.Context, userID uuid.UUID, s models.UtilityModelSetting) error
	GetModel(ctx context.Context, id uuid.UUID) (*models.Model, error)
}

// Handler serves the utility-model routes.
type Handler struct {
	store Store
	// keys answers which providers the account holds a key for.
	keys providerkeys.Resolver
	// accountsChoose is whether accounts may choose: only where they bring their own keys, and so
	// pay for their background work. Elsewhere the deployment chooses, and writes are refused.
	accountsChoose bool
	logger         *zap.Logger
}

// NewHandler builds the handler.
func NewHandler(store Store, keys providerkeys.Resolver, logger *zap.Logger) *Handler {
	return &Handler{store: store, keys: keys, accountsChoose: keys.AccountsSupplyKeys(), logger: logger}
}

// RegisterRoutes mounts the routes on an authenticated router.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/utility-models", h.List).Methods(http.MethodGet)
	router.HandleFunc("/utility-models/{role}", h.Set).Methods(http.MethodPut)
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

// Set chooses the account's model for a role, from the models it can use, or switches the role
// off. There is no clearing: where accounts choose, nothing stands behind their choice, so a
// cleared role would quietly stop running.
func (h *Handler) Set(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := h.target(w, r)
	if !ok {
		return
	}
	var req models.SetUtilityModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	if req.Off == (req.ModelID != nil) || (req.ModelID != nil && *req.ModelID == uuid.Nil) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Send either model_id or off", nil)
		return
	}
	setting := models.UtilityModelSetting{Scope: models.UtilityScopeAccount, Role: role, ModelID: req.ModelID}
	if req.ReasoningEffort != nil {
		setting.ReasoningEffort = *req.ReasoningEffort
	}
	if req.ServiceTier != nil {
		setting.ServiceTier = *req.ServiceTier
	}
	switch {
	case req.Off && (setting.ReasoningEffort != "" || setting.ServiceTier != ""):
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "A role that is off takes no reasoning effort or service tier", nil)
		return
	case setting.ReasoningEffort != "" && !setting.ReasoningEffort.Valid():
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Unknown reasoning effort", nil)
		return
	case !setting.ServiceTier.Valid():
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Unknown service tier", nil)
		return
	}
	if req.ModelID != nil && !h.checkModel(w, r, *req.ModelID, setting.ServiceTier) {
		return
	}
	err := h.store.SetUtilityModel(r.Context(), userID, setting)
	if errors.Is(err, datastore.ErrUserNotFound) {
		// The account was deleted while the request ran.
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
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
	if !h.accountsChoose {
		handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, handlerutils.CodeNotSet, "This deployment chooses the background models", nil)
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
	out := models.UtilityModels{AccountsChoose: h.accountsChoose, Roles: make([]models.UtilityRoleModel, 0, len(models.UtilityRoles))}
	for _, role := range models.UtilityRoles {
		entry := models.UtilityRoleModel{Role: role}
		for _, s := range settings {
			if s.Role != role {
				continue
			}
			choice := &models.UtilityChoice{Off: s.Off(), ModelID: s.ModelID}
			if s.ReasoningEffort != "" {
				effort := s.ReasoningEffort
				choice.ReasoningEffort = &effort
			}
			if s.ServiceTier != "" {
				tier := s.ServiceTier
				choice.ServiceTier = &tier
			}
			switch s.Scope {
			case models.UtilityScopeAccount:
				if !h.accountsChoose {
					continue // stored before, but not this deployment's to honour
				}
				entry.Account = choice
			case models.UtilityScopeDeployment:
				entry.Deployment = choice
			}
		}
		out.Roles = append(out.Roles, entry)
	}
	return out, nil
}

// checkModel answers the request and reports false when the chosen model can't take the
// choice: a service tier on a model whose API offers none (only OpenAI's Responses API does),
// or a catalog model whose provider the account holds no key for, which would only fail at
// every call; or when the model can't be read. A connection model carries its connection's
// key, and a model that doesn't exist is the store's to refuse.
func (h *Handler) checkModel(w http.ResponseWriter, r *http.Request, modelID uuid.UUID, tier models.UtilityServiceTier) bool {
	m, err := h.store.GetModel(r.Context(), modelID)
	if errors.Is(err, datastore.ErrModelNotFound) {
		return true
	}
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Error saving the utility model", err)
		return false
	}
	if tier != models.UtilityServiceTierDefault && !offersServiceTier(m) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "This model's API offers no service tier", nil)
		return false
	}
	if m.ConnectionID != nil {
		return true
	}
	p := models.ProviderForModel(m.Provider, m.Name)
	if h.keys.Key(r.Context(), p) != "" {
		return true
	}
	handlerutils.RespondIfProviderNotConfigured(w, h.logger, &providerkeys.NotConfiguredError{Provider: p})
	return false
}

// offersServiceTier reports whether m is on OpenAI's own Responses API, the only API that takes a
// service tier.
func offersServiceTier(m *models.Model) bool {
	return m.ConnectionID == nil && models.UsesOpenAIResponsesAPI(m.Provider, m.Name)
}
