package fileattachment

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/filechunker"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/filedelete"
	"github.com/theimaginaryfoundation/what-iff/internal/filesave"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type attachmentStore interface {
	ListFileAttachments(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error)
	// Get, delete and the reference checks a delete needs (filedelete.Service.DeleteFile).
	filedelete.Store
	// The swap and re-index a content save needs (filesave.Service.Replace).
	filesave.Store
	// Whether a save's chat is the caller's, and sandboxed.
	GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error)
	// Comments on text, code and image files (filecomments.Service).
	CommentFileID(ctx context.Context, userID, id uuid.UUID) (uuid.UUID, error)
	GetFileCommentSet(ctx context.Context, userID, fileID uuid.UUID) (*models.FileCommentSet, error)
	PutFileCommentSet(ctx context.Context, userID, fileID uuid.UUID, set models.FileCommentSet, expectRevision int) (int, error)
}

type attachmentAgent interface {
	FileStore() storage.FileStore
	ChunkPipeline() *filechunker.FileChunkPipeline
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
	fileAttachmentRouter.HandleFunc("/{id}/content", h.ReplaceFileAttachmentContent).Methods("PUT")
	fileAttachmentRouter.HandleFunc("/{id}/comments", h.GetFileComments).Methods("GET")
	fileAttachmentRouter.HandleFunc("/{id}/comments", h.PutFileComments).Methods("PUT")
}
