package ritual

import (
	"context"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// bindingStore defines the datastore operations for system ritual bindings.
// Used for testability; *datastore.Datastore implements this interface.
type bindingStore interface {
	GetSystemBindingsForUser(ctx context.Context, userID uuid.UUID) ([]*models.SystemRitualBinding, error)
	UpsertSystemBinding(ctx context.Context, userID, ritualID uuid.UUID, hotkeys string) (*models.SystemRitualBinding, error)
	DeleteSystemBinding(ctx context.Context, userID, ritualID uuid.UUID) error
}

type Handler struct {
	ds      *datastore.Datastore
	binding bindingStore // if non-nil, used for binding ops; else ds
	logger  *zap.Logger
}

func NewHandler(ds *datastore.Datastore, logger *zap.Logger) *Handler {
	return &Handler{ds: ds, binding: ds, logger: logger}
}

// getBindingStore returns the store to use for binding operations.
func (h *Handler) getBindingStore() bindingStore {
	if h.binding != nil {
		return h.binding
	}
	return h.ds
}

func (h *Handler) RegisterRoutes(router *mux.Router) {
	ritualRouter := router.PathPrefix("/ritual").Subrouter()

	// Specific routes first (before routes with path variables)
	ritualRouter.HandleFunc("/system", h.ListSystemRituals).Methods("GET")

	ritualRouter.HandleFunc("", h.ListRituals).Methods("GET")
	ritualRouter.HandleFunc("", h.CreateRitual).Methods("POST")
	ritualRouter.HandleFunc("/{id}", h.GetRitual).Methods("GET")
	// System ritual binding endpoints (per-user)
	ritualRouter.HandleFunc("/{id}/binding", h.GetBinding).Methods("GET")
	ritualRouter.HandleFunc("/{id}/binding", h.UpsertBinding).Methods("PUT")
	ritualRouter.HandleFunc("/{id}/binding", h.DeleteBinding).Methods("DELETE")
	ritualRouter.HandleFunc("/{id}", h.UpdateRitual).Methods("PUT")
	ritualRouter.HandleFunc("/{id}", h.DeleteRitual).Methods("DELETE")
}
