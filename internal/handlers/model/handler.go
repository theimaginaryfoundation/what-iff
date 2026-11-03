package model

import (
	"encoding/json"
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// Handler handles model-related API requests
type Handler struct {
	ds     *datastore.Datastore
	logger *zap.Logger
	// tiersApply reports whether model subscription tiers mean anything here;
	// when they do not, the list omits them. A field so tests can set it.
	tiersApply func() bool
}

// NewHandler creates a new model handler instance
func NewHandler(ds *datastore.Datastore, logger *zap.Logger) *Handler {
	return &Handler{
		ds:         ds,
		logger:     logger,
		tiersApply: metering.Linked,
	}
}

// RegisterRoutes registers all model-related routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	modelRouter := router.PathPrefix("/model").Subrouter()

	modelRouter.HandleFunc("", h.ListModels).Methods("GET")
}

// ListModels lists models. Authenticated users receive a catalog filtered by
// enable_experimental_models; unauthenticated callers see the default catalog
// (experimental providers hidden, same as enable_experimental_models=false).
// Subscription tiers are included only where they apply (metering.Linked).
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var (
		modelList []*models.Model
		err       error
	)
	if userID, ok := middleware.GetUserIDFromContext(ctx); ok {
		modelList, err = h.ds.ListModelsForUser(ctx, userID)
	} else {
		modelList, err = h.ds.ListModelsDefault(ctx)
	}
	if err != nil {
		h.logger.Error("failed to list models", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list models", err)
		return
	}

	if !h.tiersApply() {
		for _, m := range modelList {
			m.SubscriptionTier = ""
		}
	}

	json.NewEncoder(w).Encode(modelList)
}
