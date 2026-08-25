package personality

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type PersonalityAgent interface {
	EnqueueExpressionGridJob(ctx context.Context, userID, personalityID uuid.UUID) (*models.Job, error)
	EnqueuePersonalityPortraitJob(ctx context.Context, userID, flowID uuid.UUID, systemPrompt, imageStyle string) (*models.Job, error)
	EnqueuePersonalityGenerationJob(ctx context.Context, userID, flowID uuid.UUID) (*models.Job, error)
}

// Handler handles personality-related API requests
type Handler struct {
	ds               Store
	logger           *zap.Logger
	agent            *agent.Agent
	personalityAgent PersonalityAgent
}

// NewHandler creates a new Handler instance
func NewHandler(ds Store, logger *zap.Logger, agent *agent.Agent) *Handler {
	return &Handler{
		ds:               ds,
		logger:           logger,
		agent:            agent,
		personalityAgent: agent,
	}
}

// RegisterRoutes registers all personality-related routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	personalityRouter := router.PathPrefix("/personality").Subrouter()

	personalityRouter.HandleFunc("", h.ListPersonalities).Methods("GET")
	personalityRouter.HandleFunc("", h.CreatePersonality).Methods("POST")
	personalityRouter.HandleFunc("/prompt-defaults", h.GetPromptDefaults).Methods("GET")

	// Personality generation flow — registered before /{id} to avoid catch-all collision.
	personalityRouter.HandleFunc("/generate", h.GetOrCreateFlow).Methods("GET")
	personalityRouter.HandleFunc("/generate/{id}/active-job", h.GetActiveGenerationJob).Methods("GET")
	personalityRouter.HandleFunc("/generate/{id}", h.GetFlow).Methods("GET")
	personalityRouter.HandleFunc("/generate/{id}", h.UpdateFlow).Methods("PUT")
	personalityRouter.HandleFunc("/generate/{id}/reset", h.ResetFlow).Methods("POST")
	personalityRouter.HandleFunc("/generate/{id}/complete", h.CompleteFlow).Methods("POST")
	personalityRouter.HandleFunc("/generate/{id}/regenerate", h.RegenerateFlow).Methods("POST")
	personalityRouter.HandleFunc("/generate/{id}/accept", h.AcceptFlow).Methods("POST")
	personalityRouter.HandleFunc("/generate/{id}/portrait", h.GenerateFlowPortrait).Methods("POST")

	personalityRouter.HandleFunc("/active-media-job", h.GetActiveMediaJob).Methods("GET")

	personalityRouter.HandleFunc("/{id}/file-attachment", h.CreateFileAttachment).Methods("POST")
	personalityRouter.HandleFunc("/{id}/expressions", h.ListExpressions).Methods("GET")
	personalityRouter.HandleFunc("/{id}/expressions/generate-default-grid", h.GenerateDefaultExpressionGrid).Methods("POST")
	personalityRouter.HandleFunc("/{id}/expressions/{expression_key}", h.UpsertExpression).Methods("PUT")
	personalityRouter.HandleFunc("/{id}/expressions/{expression_key}", h.DeleteExpression).Methods("DELETE")
	personalityRouter.HandleFunc("/{id}", h.GetPersonality).Methods("GET")
	personalityRouter.HandleFunc("/{id}", h.UpdatePersonality).Methods("PUT")
	personalityRouter.HandleFunc("/{id}", h.DeletePersonality).Methods("DELETE")
}
