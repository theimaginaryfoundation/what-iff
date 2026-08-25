package role

import (
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// Handler handles role-related HTTP requests
type Handler struct {
	ds     *datastore.Datastore
	logger *zap.Logger
}

// NewHandler creates a new role handler
func NewHandler(ds *datastore.Datastore, logger *zap.Logger) *Handler {
	return &Handler{
		ds:     ds,
		logger: logger,
	}
}

// RegisterRoutes registers all role routes
// All routes require admin or super_admin role
func (h *Handler) RegisterRoutes(router *mux.Router) {
	// Role routes - require admin or super_admin role
	roleRouter := router.PathPrefix("/roles").Subrouter()
	roleRouter.Use(middleware.RequireRole(h.logger, "admin", "super_admin"))

	// CRUD routes
	roleRouter.HandleFunc("", h.ListRoles).Methods("GET")
	roleRouter.HandleFunc("", h.CreateRole).Methods("POST")
	roleRouter.HandleFunc("/{id}", h.GetRole).Methods("GET")
	roleRouter.HandleFunc("/{id}", h.UpdateRole).Methods("PUT")
	roleRouter.HandleFunc("/{id}", h.DeleteRole).Methods("DELETE")
}
