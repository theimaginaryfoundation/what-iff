package version

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/buildinfo"
)

// Handler handles version requests
type Handler struct {
	info   buildinfo.Info
	logger *zap.Logger
}

// NewHandler creates a new version handler reporting the given build info
func NewHandler(info buildinfo.Info, logger *zap.Logger) *Handler {
	return &Handler{
		info:   info,
		logger: logger,
	}
}

// Response represents the version response
type Response struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
	// OverlayCommit is only present in builds that compose additional
	// source on top of the open-source tree; absent means "no overlay".
	OverlayCommit string `json:"overlay_commit,omitempty"`
}

// Get handles GET /api/version requests
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := Response{
		Version:       h.info.Version,
		Commit:        h.info.Commit,
		BuiltAt:       h.info.BuiltAt,
		OverlayCommit: h.info.OverlayCommit,
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode version response", zap.Error(err))
	}
}
