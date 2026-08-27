// Package accountexport implements the user-facing account export/import MVP, run in-process in the
// main app (no separate worker). Export is asynchronous and its download link is delivered ONLY by
// email — a deliberate security control: access to the app account alone cannot exfiltrate the full
// account; the exfiltrator would also need mailbox access. Imports are staged and restored by bounded
// background workers.
package accountexport

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/embedding"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/email"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

const (
	// exportCooldown is the minimum spacing between a user's export requests. Exports are heavy and
	// infrequent; one in-flight/recent export per user is plenty.
	exportCooldown = 10 * time.Minute
	// bundlePrefix is the S3 key prefix for generated export archives (never served by other flows).
	bundlePrefix = "exports"
	// presignTTL is how long the emailed download link stays valid.
	presignTTL = 24 * time.Hour
	// exportJobTimeout bounds a single in-process export build.
	exportJobTimeout = 30 * time.Minute
	// importJobTimeout bounds a single in-process account import.
	importJobTimeout = 30 * time.Minute
	// maxConcurrentAccountImports bounds expensive archive expansion and embedding work per API task.
	maxConcurrentAccountImports = 2
	// maxImportBytes caps the uploaded import ZIP.
	maxImportBytes = 100 << 20 // 100 MiB
	// maxImportEntries and maxImportExpandedBytes bound ZIP-bomb expansion during import.
	maxImportEntries       = 10_000
	maxImportExpandedBytes = 250 << 20 // 250 MiB
	// importMultipartMemory keeps only form metadata in memory; the file part spools to a temp file.
	importMultipartMemory = 1 << 20 // 1 MiB
)

// Handler serves the account export/import endpoints.
type Handler struct {
	ds        *datastore.Datastore
	logger    *zap.Logger
	fileStore storage.FileStore
	sender    email.Sender
	oaiClient *openai.Client // for regenerating memory embeddings on import; nil disables memory import
	limiter   *cooldownLimiter
	imports   chan struct{}
}

// NewHandler builds the handler. openAIKey enables memory-embedding regeneration on import; when
// empty, memory import is skipped (export is unaffected).
func NewHandler(ds *datastore.Datastore, logger *zap.Logger, fileStore storage.FileStore, sender email.Sender, openAIKey string) *Handler {
	h := &Handler{
		ds:        ds,
		logger:    logger,
		fileStore: fileStore,
		sender:    sender,
		limiter:   newCooldownLimiter(exportCooldown),
		imports:   make(chan struct{}, maxConcurrentAccountImports),
	}
	if openAIKey != "" {
		client := openai.NewClient(option.WithAPIKey(openAIKey))
		h.oaiClient = &client
	} else {
		logger.Warn("account export: OpenAI key not configured — memory import will be skipped")
	}
	return h
}

// RegisterRoutes wires the account export/import routes onto an authenticated router.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	r := router.PathPrefix("/account").Subrouter()
	r.HandleFunc("/export", h.EnqueueExport).Methods("POST")
	r.HandleFunc("/export/{id}", h.GetExport).Methods("GET")
	r.HandleFunc("/import", h.ImportAccount).Methods("POST")
	r.HandleFunc("/import/{id}", h.GetImport).Methods("GET")
}

// GetImport returns an account-import Job, including its JSON-encoded AccountImportProgress.
func (h *Handler) GetImport(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid import id", err)
		return
	}
	job, err := h.ds.GetJob(r.Context(), userID, id)
	if err != nil {
		if ent.IsNotFound(err) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Import not found", err)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to retrieve import", err)
		return
	}
	if job.JobType != models.JobTypeAccountImport {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Import not found", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, job)
}

// EnqueueExport creates a pending export Job and builds it in a background goroutine. Returns 202
// with the Job. The user is emailed the download link on completion; the link is never returned here.
func (h *Handler) EnqueueExport(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	recent, err := h.ds.HasRecentAccountExport(r.Context(), userID, time.Now().Add(-exportCooldown))
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to check recent export requests", err)
		return
	}
	if recent {
		handlerutils.RespondWithError(w, h.logger, http.StatusTooManyRequests, handlerutils.CodeNotSet, "An export was requested recently; please wait before requesting another.", nil)
		return
	}
	if !h.limiter.allow(userID) {
		handlerutils.RespondWithError(w, h.logger, http.StatusTooManyRequests, handlerutils.CodeNotSet, "An export was requested recently; please wait before requesting another.", nil)
		return
	}

	progress, _ := json.Marshal(models.AccountExportProgress{Phase: models.AccountExportPhaseQueued})
	job, err := h.ds.CreateJob(r.Context(), userID, models.Job{
		JobType:   models.JobTypeAccountExport,
		Reference: userID.String(),
		Status:    models.JobStatusPending,
		Progress:  string(progress),
	})
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to start export", err)
		return
	}
	h.ds.AuditAccountExport(r.Context(), userID, "requested", "user requested account export", map[string]any{
		"job_id": job.ID.String(),
	})

	// Detached from the request: the build must outlive the HTTP response.
	go func(userID, jobID uuid.UUID) {
		ctx, cancel := context.WithTimeout(context.Background(), exportJobTimeout)
		defer cancel()
		h.runAccountExport(ctx, userID, jobID)
	}(userID, job.ID)

	h.logger.Info("account export enqueued", zap.String("user_id", userID.String()), zap.String("job_id", job.ID.String()))
	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, job)
}

// GetExport returns the export Job status. Its progress carries the phase and counts — never the
// download link, which is delivered only by email.
func (h *Handler) GetExport(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid export id", err)
		return
	}
	job, err := h.ds.GetJob(r.Context(), userID, id)
	if err != nil {
		if ent.IsNotFound(err) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Export not found", err)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to retrieve export", err)
		return
	}
	if job.JobType != models.JobTypeAccountExport {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Export not found", nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, job)
}

// createEmbedding regenerates a memory embedding on import. Returns an error when no OpenAI client is
// configured so the caller can skip/annotate the memory section rather than crash.
func (h *Handler) createEmbedding(ctx context.Context, input string) ([]float32, error) {
	if h.oaiClient == nil {
		return nil, errMemoryImportUnavailable
	}
	return embedding.CreateEmbedding(ctx, h.oaiClient, input)
}

// createEmbeddings regenerates a bounded batch of memory embeddings during account import.
func (h *Handler) createEmbeddings(ctx context.Context, inputs []string) ([][]float32, error) {
	if h.oaiClient == nil {
		return nil, errMemoryImportUnavailable
	}
	return embedding.CreateEmbeddings(ctx, h.oaiClient, inputs)
}

// cooldownLimiter enforces a per-user minimum interval between export requests.
type cooldownLimiter struct {
	mu     sync.Mutex
	last   map[uuid.UUID]time.Time
	window time.Duration
}

func newCooldownLimiter(window time.Duration) *cooldownLimiter {
	return &cooldownLimiter{last: make(map[uuid.UUID]time.Time), window: window}
}

func (l *cooldownLimiter) allow(userID uuid.UUID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// Opportunistically drop entries past the window so the map does not grow unbounded over uptime.
	for id, t := range l.last {
		if now.Sub(t) >= l.window {
			delete(l.last, id)
		}
	}
	if prev, ok := l.last[userID]; ok && now.Sub(prev) < l.window {
		return false
	}
	l.last[userID] = now
	return true
}
