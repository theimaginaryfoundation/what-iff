package imagegallery

import (
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// Handler serves the gallery API: listing a user's images and other files, filing them in folders,
// and proxying full-resolution or thumbnail image bytes from object storage.
type Handler struct {
	ds        Store
	logger    *zap.Logger
	fileStore storage.FileStore
	agent     *agent.Agent
}

// NewHandler creates a new Handler instance.
func NewHandler(ds Store, logger *zap.Logger, fileStore storage.FileStore, agents ...*agent.Agent) *Handler {
	var a *agent.Agent
	if len(agents) > 0 {
		a = agents[0]
	}
	return &Handler{ds: ds, logger: logger, fileStore: fileStore, agent: a}
}

// RegisterRoutes wires the gallery endpoints onto the authenticated subrouter.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	gallery := router.PathPrefix("/image-gallery").Subrouter()
	gallery.HandleFunc("", h.ListImages).Methods("GET")
	gallery.HandleFunc("/import", h.ImportImage).Methods("POST")
	// Registered before /{id}, which would otherwise take "folders" for an image id.
	gallery.HandleFunc("/folders", h.ListFolders).Methods("GET")
	gallery.HandleFunc("/folders/move", h.MoveFolder).Methods("POST")
	gallery.HandleFunc("/move", h.MoveImages).Methods("POST")
	gallery.HandleFunc("/{id}", h.GetImageContent).Methods("GET")
	gallery.HandleFunc("/{id}", h.DeleteImage).Methods("DELETE")
	gallery.HandleFunc("/{id}", h.RenameImage).Methods("PATCH")
	gallery.HandleFunc("/{id}/info", h.GetFileInfo).Methods("GET")
	gallery.HandleFunc("/{id}/reference", h.ReferenceImage).Methods("POST")
}
