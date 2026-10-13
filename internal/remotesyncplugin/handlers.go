package remotesyncplugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/remotesync"
	"go.uber.org/zap"
)

// Store is the storage the routes need; *datastore.Datastore implements it. Owner-scoped.
type Store interface {
	GetRemoteSyncSettings(ctx context.Context, userID uuid.UUID) (*models.RemoteSyncSettings, error)
	UpsertRemoteSync(ctx context.Context, userID uuid.UUID, in models.RemoteSyncUpsert) error
	DeleteRemoteSync(ctx context.Context, userID uuid.UUID) error
	GetRemoteSyncAccount(ctx context.Context, userID uuid.UUID) (*models.RemoteSyncAccount, error)
}

// Starter starts a run in the background (remotesync.Syncer).
type Starter interface {
	Start(ctx context.Context, userID uuid.UUID) error
	Interval() time.Duration
}

// Handler serves /api/remote-sync (behind auth).
type Handler struct {
	Store  Store
	Syncer Starter
	// Check proves a token can read from a remote before it is saved.
	Check func(ctx context.Context, baseURL, token string) error
	// Lifecycle is the server's context: runs started from a request outlive the request.
	Lifecycle         context.Context
	AllowInsecureHTTP bool
	Logger            *zap.Logger
}

// Register mounts the routes on r (the authenticated router, prefixed /remote-sync).
func (h *Handler) Register(r *mux.Router) {
	r.HandleFunc("", h.get).Methods(http.MethodGet)
	r.HandleFunc("", h.put).Methods(http.MethodPut)
	r.HandleFunc("", h.delete).Methods(http.MethodDelete)
	r.HandleFunc("/run", h.run).Methods(http.MethodPost)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	h.respondSettings(w, r, userID, http.StatusOK)
}

type putRequest struct {
	BaseURL string  `json:"base_url"`
	Token   *string `json:"token,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

// put saves the settings. Unless it switches sync off, it first proves the token can read from
// the remote, so a typo or a token without chat:read is reported here, not by a failed run later.
func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	var req putRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		h.badRequest(w, "The request body is not valid JSON.")
		return
	}
	raw := req.BaseURL
	if strings.TrimSpace(raw) == "" {
		raw = remotesync.DefaultBaseURL
	}
	baseURL, err := remotesync.NormalizeBaseURL(raw, h.AllowInsecureHTTP)
	if err != nil {
		h.badRequest(w, upperFirst(err.Error())+".")
		return
	}
	if req.Token != nil {
		t := cleanToken(*req.Token)
		if t == "" {
			req.Token = nil
		} else {
			req.Token = &t
		}
	}

	token := ""
	if req.Token != nil {
		token = *req.Token
	} else if acct, err := h.Store.GetRemoteSyncAccount(r.Context(), userID); err == nil {
		token = acct.Token
	} else if !errors.Is(err, datastore.ErrRemoteSyncNotFound) {
		h.failed(w, "Could not read the sync settings.", err)
		return
	}
	if token == "" {
		h.badRequest(w, "Paste a webhook token from the remote server.")
		return
	}

	switchingOff := req.Enabled != nil && !*req.Enabled
	if !switchingOff {
		if err := h.Check(r.Context(), baseURL, token); err != nil {
			handlerutils.RespondWithError(w, h.Logger, http.StatusBadRequest, handlerutils.CodeNotSet, remotesync.UserMessage(err), nil)
			return
		}
	}

	if err := h.Store.UpsertRemoteSync(r.Context(), userID, models.RemoteSyncUpsert{BaseURL: baseURL, Token: req.Token, Enabled: req.Enabled}); err != nil {
		if errors.Is(err, datastore.ErrRemoteSyncTokenRequired) {
			h.badRequest(w, "Paste a webhook token from the remote server.")
			return
		}
		h.failed(w, "Could not save the sync settings.", err)
		return
	}
	if !switchingOff {
		if err := h.Syncer.Start(h.Lifecycle, userID); err != nil && !errors.Is(err, remotesync.ErrBusy) && !errors.Is(err, remotesync.ErrDisabled) {
			h.Logger.Warn("remote sync: start after save", zap.Error(err))
		}
	}
	h.respondSettings(w, r, userID, http.StatusOK)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	if err := h.Store.DeleteRemoteSync(r.Context(), userID); err != nil && !errors.Is(err, datastore.ErrRemoteSyncNotFound) {
		h.failed(w, "Could not remove the sync settings.", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// run starts a sync now. The run continues after the response; poll GET for its outcome.
func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	err := h.Syncer.Start(h.Lifecycle, userID)
	switch {
	case err == nil:
		h.respondSettings(w, r, userID, http.StatusAccepted)
	case errors.Is(err, datastore.ErrRemoteSyncNotFound):
		handlerutils.RespondWithError(w, h.Logger, http.StatusNotFound, handlerutils.CodeNotSet, "Remote sync is not set up.", nil)
	case errors.Is(err, remotesync.ErrBusy):
		handlerutils.RespondWithError(w, h.Logger, http.StatusConflict, handlerutils.CodeNotSet, "A sync is already running.", nil)
	case errors.Is(err, remotesync.ErrDisabled):
		handlerutils.RespondWithError(w, h.Logger, http.StatusConflict, handlerutils.CodeNotSet, "Sync is switched off. Switch it on first.", nil)
	default:
		h.failed(w, "Could not start a sync.", err)
	}
}

func (h *Handler) respondSettings(w http.ResponseWriter, r *http.Request, userID uuid.UUID, status int) {
	s, err := h.Store.GetRemoteSyncSettings(r.Context(), userID)
	if err != nil {
		h.failed(w, "Could not read the sync settings.", err)
		return
	}
	s.IntervalSeconds = int(h.Syncer.Interval() / time.Second)
	s.DefaultBaseURL = remotesync.DefaultBaseURL
	s.AllowInsecureHTTP = h.AllowInsecureHTTP
	w.Header().Set("Cache-Control", "no-store")
	handlerutils.RespondWithJSON(w, h.Logger, status, s)
}

func (h *Handler) userID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.Logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
	}
	return id, ok
}

func (h *Handler) badRequest(w http.ResponseWriter, msg string) {
	handlerutils.RespondWithError(w, h.Logger, http.StatusBadRequest, handlerutils.CodeNotSet, msg, nil)
}

func (h *Handler) failed(w http.ResponseWriter, msg string, err error) {
	handlerutils.RespondWithError(w, h.Logger, http.StatusInternalServerError, handlerutils.CodeNotSet, msg, err)
}

// cleanToken trims a pasted token, including a pasted "Bearer " prefix.
func cleanToken(t string) string {
	t = strings.TrimSpace(t)
	if len(t) > 7 && strings.EqualFold(t[:7], "bearer ") {
		t = strings.TrimSpace(t[7:])
	}
	return t
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
