package fileattachment

import (
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// Handler handles file attachment-related API requests
type Handler struct {
	ds     *datastore.Datastore
	logger *zap.Logger
	agent  *agent.Agent
}

// NewHandler creates a new Handler instance
func NewHandler(ds *datastore.Datastore, logger *zap.Logger, agent *agent.Agent) *Handler {
	return &Handler{ds: ds, logger: logger, agent: agent}
}

// RegisterRoutes registers all file attachment-related routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	fileAttachmentRouter := router.PathPrefix("/file-attachment").Subrouter()

	fileAttachmentRouter.HandleFunc("", h.ListFileAttachments).Methods("GET")
	fileAttachmentRouter.HandleFunc("/{id}", h.DeleteFileAttachment).Methods("DELETE")
}
