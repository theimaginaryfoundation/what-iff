package fileattachment

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type attachmentStore interface {
	ListFileAttachments(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error)
	GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error)
	DeleteFileAttachment(ctx context.Context, userID, id uuid.UUID) error
}

type attachmentAgent interface {
	FileStore() storage.FileStore
	DeleteProviderFileAttachment(ctx context.Context, fileID string) error
}

// Handler handles file attachment-related API requests
type Handler struct {
	ds     attachmentStore
	logger *zap.Logger
	agent  attachmentAgent
}

// NewHandler creates a new Handler instance
func NewHandler(ds *datastore.Datastore, logger *zap.Logger, agent *agent.Agent) *Handler {
	return &Handler{ds: ds, logger: logger, agent: agent}
}

// RegisterRoutes registers all file attachment-related routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	fileAttachmentRouter := router.PathPrefix("/file-attachment").Subrouter()

	fileAttachmentRouter.HandleFunc("", h.ListFileAttachments).Methods("GET")
	fileAttachmentRouter.HandleFunc("/{id}", h.GetFileAttachmentContent).Methods("GET")
	fileAttachmentRouter.HandleFunc("/{id}", h.DeleteFileAttachment).Methods("DELETE")
}
