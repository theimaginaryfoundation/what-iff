package model

import (
	"encoding/json"
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// Handler handles model-related API requests
type Handler struct {
	ds        *datastore.Datastore
	logger    *zap.Logger
	providers models.ProviderAvailability
}

// NewHandler creates a new model handler instance. providers determines which
// models are offered: a model whose provider has no configured key fails at
// send time, so listing it is a choice the server cannot honour.
func NewHandler(ds *datastore.Datastore, logger *zap.Logger, providers models.ProviderAvailability) *Handler {
	return &Handler{
		ds:        ds,
		logger:    logger,
		providers: providers,
	}
}

// RegisterRoutes registers all model-related routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	modelRouter := router.PathPrefix("/model").Subrouter()

	modelRouter.HandleFunc("", h.ListModels).Methods("GET")
}

// ListModels lists models the caller can actually use. Two filters apply:
// enable_experimental_models for authenticated users (unauthenticated callers
// get the default catalog, experimental hidden), and provider availability —
// models whose provider has no configured credential are omitted rather than
// offered and then failed at send time.
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var (
		modelList []*models.Model
		err       error
	)
	// include_hidden serves the management screen, which has to name the models
	// it is offering to unhide. Everything else — the picker included — gets the
	// filtered list.
	includeHidden := r.URL.Query().Get("include_hidden") == "true"
	if userID, ok := middleware.GetUserIDFromContext(ctx); ok {
		if includeHidden {
			modelList, err = h.ds.ListAllModelsForUser(ctx, userID)
		} else {
			modelList, err = h.ds.ListModelsForUser(ctx, userID)
		}
	} else {
		modelList, err = h.ds.ListModelsDefault(ctx)
	}
	if err != nil {
		h.logger.Error("failed to list models", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list models", err)
		return
	}

	json.NewEncoder(w).Encode(h.providers.FilterUsable(ctx, modelList))
}
