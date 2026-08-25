package memory

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/embedding"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"

	"github.com/gorilla/mux"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"go.uber.org/zap"
)

// exportRateLimit is the default per-user sliding-window limit for the export endpoint.
const (
	exportRateLimit  = 5
	exportRateWindow = 15 * time.Minute
)

// Handler handles memory-related API requests
type Handler struct {
	ds            *datastore.Datastore
	logger        *zap.Logger
	exportLimiter *exportRateLimiter
	oaiClient     *openai.Client
}

var errMemoryImportUnavailable = errors.New("memory import unavailable: OpenAI API key is not configured")

// NewHandler creates a new memory handler instance. httpClient overrides the
// SDK's default HTTP client when non-nil; under MOCK_LLM the server passes the
// deny-network client so import embeddings cannot reach the provider.
func NewHandler(ds *datastore.Datastore, logger *zap.Logger, openAIKey string, httpClient *http.Client) *Handler {
	h := &Handler{
		ds:            ds,
		logger:        logger,
		exportLimiter: newExportRateLimiter(exportRateLimit, exportRateWindow),
	}
	if strings.TrimSpace(openAIKey) == "" {
		logger.Warn("memory import disabled: OpenAI API key is not configured")
		return h
	}

	opts := []option.RequestOption{option.WithAPIKey(openAIKey)}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	client := openai.NewClient(opts...)
	h.oaiClient = &client
	return h
}

// RegisterRoutes registers all memory-related routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	memoryRouter := router.PathPrefix("/memory").Subrouter()

	memoryRouter.HandleFunc("", h.ListMemories).Methods("GET")
	memoryRouter.HandleFunc("", h.CreateMemory).Methods("POST")
	memoryRouter.HandleFunc("/batch", h.CreateMemoriesBatch).Methods("POST")
	memoryRouter.HandleFunc("/export", h.ExportMemories).Methods("GET")
	memoryRouter.HandleFunc("/import", h.ImportMemories).Methods("POST")
	memoryRouter.HandleFunc("/merge-events", h.ListMemoryMergeEvents).Methods("GET")
	memoryRouter.HandleFunc("/merge-events/{id}/undo", h.UndoMemoryMergeEvent).Methods("POST")
	memoryRouter.HandleFunc("/compaction-events", h.ListCompactionEvents).Methods("GET")
	memoryRouter.HandleFunc("/compaction-events/{id}", h.GetCompactionEvent).Methods("GET")
	memoryRouter.HandleFunc("/snapshots/{id}/revert", h.RevertCheckpointSnapshot).Methods("POST")
	memoryRouter.HandleFunc("/{id}", h.GetMemory).Methods("GET")
	memoryRouter.HandleFunc("/{id}", h.PatchMemory).Methods("PATCH")
	memoryRouter.HandleFunc("/{id}", h.DeleteMemory).Methods("DELETE")
	memoryRouter.HandleFunc("/{id}/pin", h.UpdateMemoryPin).Methods("PUT")
}

func (h *Handler) createEmbedding(ctx context.Context, input string) ([]float32, error) {
	if h.oaiClient == nil {
		return nil, errMemoryImportUnavailable
	}
	return embedding.CreateEmbedding(ctx, h.oaiClient, input)
}
