package ritual

import (
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"go.uber.org/zap"
)

type Handler struct {
	ds     *datastore.Datastore
	logger *zap.Logger
}

func NewHandler(ds *datastore.Datastore, logger *zap.Logger) *Handler {
	return &Handler{ds: ds, logger: logger}
}

func (h *Handler) RegisterRoutes(router *mux.Router) {
	ritualRouter := router.PathPrefix("/ritual").Subrouter()

	ritualRouter.HandleFunc("", h.ListRituals).Methods("GET")
	ritualRouter.HandleFunc("", h.CreateRitual).Methods("POST")
	ritualRouter.HandleFunc("/import", h.ImportRitual).Methods("POST")
	ritualRouter.HandleFunc("/{id}/export", h.ExportRitual).Methods("GET")
	ritualRouter.HandleFunc("/{id}", h.GetRitual).Methods("GET")
	ritualRouter.HandleFunc("/{id}", h.UpdateRitual).Methods("PUT")
	ritualRouter.HandleFunc("/{id}", h.DeleteRitual).Methods("DELETE")
}
