package job

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/internal/providers"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type JobCanceller interface {
	CancelJob(ctx context.Context, userID, jobID uuid.UUID) error
}

// Handler handles job-related API requests
type Handler struct {
	provider  providers.JobProvider
	canceller JobCanceller
	logger    *zap.Logger
}

// NewHandler creates a new Handler
func NewHandler(provider providers.JobProvider, logger *zap.Logger) *Handler {
	return &Handler{
		provider: provider,
		logger:   logger,
	}
}

// NewHandlerWithCanceller creates a new Handler with runtime cancellation support.
func NewHandlerWithCanceller(provider providers.JobProvider, canceller JobCanceller, logger *zap.Logger) *Handler {
	return &Handler{
		provider:  provider,
		canceller: canceller,
		logger:    logger,
	}
}

// RegisterRoutes registers the job routes on the given router
func (h *Handler) RegisterRoutes(router *mux.Router) {
	jobRouter := router.PathPrefix("/job").Subrouter()

	jobRouter.HandleFunc("", h.ListJobs).Methods("GET")
	jobRouter.HandleFunc("/{id}", h.GetJob).Methods("GET")
	jobRouter.HandleFunc("/{id}/status", h.UpdateJobStatus).Methods("PUT")
	jobRouter.HandleFunc("/{id}/result", h.SetJobResult).Methods("PUT")
	jobRouter.HandleFunc("/{id}/cancel", h.CancelJob).Methods("POST")
}
