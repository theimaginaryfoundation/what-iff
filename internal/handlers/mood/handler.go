package mood

import (
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// Handler serves the mood API.
type Handler struct {
	ds        Store
	logger    *zap.Logger
	fileStore storage.FileStore
}

// NewHandler creates a new Handler.
func NewHandler(ds Store, logger *zap.Logger, fileStore storage.FileStore) *Handler {
	return &Handler{ds: ds, logger: logger, fileStore: fileStore}
}

// RegisterRoutes wires mood endpoints onto the authenticated subrouter.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	r := router.PathPrefix("/mood").Subrouter()
	r.HandleFunc("", h.ListMoods).Methods("GET")
	r.HandleFunc("", h.CreateMood).Methods("POST")
	r.HandleFunc("/{id}", h.GetMood).Methods("GET")
	r.HandleFunc("/{id}", h.UpdateMood).Methods("PUT")
	r.HandleFunc("/{id}", h.DeleteMood).Methods("DELETE")
	r.HandleFunc("/{id}/personalities", h.AttachToPersonalities).Methods("POST")
}
