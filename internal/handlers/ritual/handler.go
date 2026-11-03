package ritual

import (
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

type Handler struct {
	ds *datastore.Datastore
	// files holds skill bundle files (ADR 0x035 §3); nil when the deployment has no object store,
	// in which case bundle files cannot be stored and import keeps the text only.
	files  storage.FileStore
	logger *zap.Logger
}

func NewHandler(ds *datastore.Datastore, files storage.FileStore, logger *zap.Logger) *Handler {
	return &Handler{ds: ds, files: files, logger: logger}
}

func (h *Handler) RegisterRoutes(router *mux.Router) {
	ritualRouter := router.PathPrefix("/ritual").Subrouter()

	ritualRouter.HandleFunc("", h.ListRituals).Methods("GET")
	ritualRouter.HandleFunc("", h.CreateRitual).Methods("POST")
	ritualRouter.HandleFunc("/import", h.ImportRitual).Methods("POST")
	ritualRouter.HandleFunc("/{id}/export", h.ExportRitual).Methods("GET")
	ritualRouter.HandleFunc("/{id}/files", h.ListSkillFiles).Methods("GET")
	ritualRouter.HandleFunc("/{id}/files", h.UploadSkillFile).Methods("POST")
	ritualRouter.HandleFunc("/{id}/files/{fileId}", h.DeleteSkillFile).Methods("DELETE")
	ritualRouter.HandleFunc("/{id}", h.GetRitual).Methods("GET")
	ritualRouter.HandleFunc("/{id}", h.UpdateRitual).Methods("PUT")
	ritualRouter.HandleFunc("/{id}", h.DeleteRitual).Methods("DELETE")
}
