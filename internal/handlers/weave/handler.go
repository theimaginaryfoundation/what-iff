// Package weave serves the weave API (docs/adr/0x030-thread-weaves.md): owner-only routes that
// join sandboxed threads into a weave whose members may read each other.
//
// Membership is deliberately an owner action with no agent tool: a stranger talking to a persona
// in one sandboxed room must not be able to have it woven with another.
package weave

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Store is the storage the routes need (the datastore satisfies it).
type Store interface {
	CreateWeave(ctx context.Context, userID uuid.UUID, name string, chatIDs []uuid.UUID) (*models.Weave, error)
	GetWeave(ctx context.Context, userID, id uuid.UUID) (*models.Weave, error)
	ListWeaves(ctx context.Context, userID uuid.UUID) ([]models.Weave, error)
	AddChatToWeave(ctx context.Context, userID, weaveID, chatID uuid.UUID) (*models.Weave, error)
	RemoveChatFromWeave(ctx context.Context, userID, weaveID, chatID uuid.UUID) (*models.Weave, error)
	DeleteWeave(ctx context.Context, userID, id uuid.UUID) error
}

// Handler serves /api/weaves.
type Handler struct {
	ds     Store
	logger *zap.Logger
}

// NewHandler creates a new Handler.
func NewHandler(ds Store, logger *zap.Logger) *Handler {
	return &Handler{ds: ds, logger: logger}
}

// RegisterRoutes wires the weave endpoints onto the authenticated subrouter.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	r := router.PathPrefix("/weaves").Subrouter()
	r.HandleFunc("", h.list).Methods(http.MethodGet)
	r.HandleFunc("", h.create).Methods(http.MethodPost)
	r.HandleFunc("/{id}", h.get).Methods(http.MethodGet)
	r.HandleFunc("/{id}", h.delete).Methods(http.MethodDelete)
	r.HandleFunc("/{id}/chats/{chatId}", h.addChat).Methods(http.MethodPut)
	r.HandleFunc("/{id}/chats/{chatId}", h.removeChat).Methods(http.MethodDelete)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	weaves, err := h.ds.ListWeaves(r.Context(), userID)
	if err != nil {
		h.fail(w, err, "Failed to list weaves")
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, weaves)
}

type createRequest struct {
	Name    string      `json:"name"`
	ChatIDs []uuid.UUID `json:"chat_ids"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	var req createRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "invalid request body", nil)
		return
	}
	weave, err := h.ds.CreateWeave(r.Context(), userID, req.Name, req.ChatIDs)
	if err != nil {
		h.fail(w, err, "Failed to create the weave")
		return
	}
	h.logger.Info("weave created", zap.String("weave_id", weave.ID.String()), zap.Int("chats", len(weave.Chats)))
	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, weave)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := h.userAndID(w, r)
	if !ok {
		return
	}
	weave, err := h.ds.GetWeave(r.Context(), userID, id)
	if err != nil {
		h.fail(w, err, "Failed to load the weave")
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, weave)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := h.userAndID(w, r)
	if !ok {
		return
	}
	if err := h.ds.DeleteWeave(r.Context(), userID, id); err != nil {
		h.fail(w, err, "Failed to delete the weave")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) addChat(w http.ResponseWriter, r *http.Request) {
	h.member(w, r, h.ds.AddChatToWeave, "Failed to add the thread")
}

func (h *Handler) removeChat(w http.ResponseWriter, r *http.Request) {
	h.member(w, r, h.ds.RemoveChatFromWeave, "Failed to remove the thread")
}

func (h *Handler) member(w http.ResponseWriter, r *http.Request, op func(ctx context.Context, userID, weaveID, chatID uuid.UUID) (*models.Weave, error), msg string) {
	userID, id, ok := h.userAndID(w, r)
	if !ok {
		return
	}
	chatID, err := uuid.Parse(mux.Vars(r)["chatId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "invalid chatId", nil)
		return
	}
	weave, err := op(r.Context(), userID, id, chatID)
	if err != nil {
		h.fail(w, err, msg)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, weave)
}

func (h *Handler) user(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
	}
	return id, ok
}

func (h *Handler) userAndID(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := h.user(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "invalid id", nil)
		return uuid.Nil, uuid.Nil, false
	}
	return userID, id, true
}

// fail maps storage errors: another account's weave or thread is a 404, a thread that cannot join
// is a 400 with the reason, anything else a 500.
func (h *Handler) fail(w http.ResponseWriter, err error, msg string) {
	switch {
	case errors.Is(err, datastore.ErrWeaveNotFound), errors.Is(err, datastore.ErrChatNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Not found", nil)
	case errors.Is(err, datastore.ErrWeaveChat):
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, weaveReason(err), nil)
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, msg, err)
	}
}

// weaveReason is the user-facing part of an ErrWeaveChat ("weave: chat cannot join: <reason>").
func weaveReason(err error) string {
	msg := err.Error()
	prefix := datastore.ErrWeaveChat.Error() + ": "
	if len(msg) > len(prefix) && msg[:len(prefix)] == prefix {
		return msg[len(prefix):]
	}
	return "That thread cannot be woven."
}
