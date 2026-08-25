package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"
)

const (
	// Database ping timeout - must be under ALB's 5s health check timeout
	dbPingTimeout = 3 * time.Second
)

// Handler handles health check requests
type Handler struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewHandler creates a new health check handler
func NewHandler(db *sql.DB, logger *zap.Logger) *Handler {
	return &Handler{
		db:     db,
		logger: logger,
	}
}

// Response represents the health check response
type Response struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// Check handles GET /api/health requests
func (h *Handler) Check(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	response := Response{
		Status: "healthy",
		Checks: make(map[string]string),
	}

	// Check database connectivity
	if err := h.checkDatabase(r.Context()); err != nil {
		h.logger.Warn("Health check: database unhealthy", zap.Error(err))
		response.Status = "unhealthy"
		response.Checks["database"] = "error"
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			h.logger.Error("Failed to encode health response", zap.Error(err))
		}
		return
	}

	response.Checks["database"] = "ok"
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode health response", zap.Error(err))
	}
}

// checkDatabase pings the database with timeout
func (h *Handler) checkDatabase(ctx context.Context) error {
	if h.db == nil {
		return sql.ErrConnDone
	}

	ctx, cancel := context.WithTimeout(ctx, dbPingTimeout)
	defer cancel()

	return h.db.PingContext(ctx)
}
