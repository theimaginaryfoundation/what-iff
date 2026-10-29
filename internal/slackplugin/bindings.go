package slackplugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
	"github.com/theimaginaryfoundation/what-iff/internal/slackrelay"
	"go.uber.org/zap"
)

func (h *Handler) listBindings(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	bindings, err := h.Store.ListSlackBindings(r.Context(), userID)
	if err != nil {
		h.fail(w, err, "Failed to list Slack channels")
		return
	}
	h.markSandboxed(r.Context(), userID, bindings)
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, bindings)
}

// unrestrictedBindMsg is the 400 for binding a thread that is not sandboxed. A Slack channel
// is shared with other people: whoever the allow list lets mention the persona drives this
// thread, so a thread that can read the account hands them the owner's name, memories, other
// conversations, files and scratchpad.
const unrestrictedBindMsg = "That thread is not sandboxed, so anyone allowed to mention the persona could use your memories, " +
	"other conversations, files and scratchpad. Sandbox the thread first, or confirm with allow_unrestricted."

// bindableChat decides whether chatID may be bound to a Slack channel (or a binding repointed
// to it). A sandboxed thread always may; any other only with the owner's explicit
// acknowledgement. record is the value to store as the binding's allow_unrestricted: true only
// for an acknowledged thread that is not sandboxed. When ok is false the response is written.
func (h *Handler) bindableChat(w http.ResponseWriter, r *http.Request, userID, chatID uuid.UUID, acknowledged bool) (record, ok bool) {
	chat, err := h.Store.GetChat(r.Context(), userID, chatID)
	if err != nil {
		h.fail(w, err, "Failed to load the thread")
		return false, false
	}
	if relaykit.RelayThreadOpenWithoutAcknowledgement(chat) {
		return false, true
	}
	if !acknowledged {
		h.bad(w, unrestrictedBindMsg)
		return false, false
	}
	return true, true
}

// markSandboxed fills each binding's computed ChatSandboxed from its thread's current sandbox
// state (the UI shows it; the relay re-checks it on every mention).
func (h *Handler) markSandboxed(ctx context.Context, userID uuid.UUID, bindings []models.SlackBinding) {
	seen := map[uuid.UUID]*bool{}
	for i := range bindings {
		id := bindings[i].ChatID
		v, ok := seen[id]
		if !ok {
			if chat, err := h.Store.GetChat(ctx, userID, id); err == nil && chat != nil {
				open := relaykit.RelayThreadOpenWithoutAcknowledgement(chat)
				v = &open
			}
			seen[id] = v
		}
		bindings[i].ChatSandboxed = v
	}
}

func (h *Handler) markOne(ctx context.Context, userID uuid.UUID, b *models.SlackBinding) {
	bs := []models.SlackBinding{*b}
	h.markSandboxed(ctx, userID, bs)
	b.ChatSandboxed = bs[0].ChatSandboxed
}

type createBindingRequest struct {
	AppID       uuid.UUID `json:"app_id"`
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	IsPrivate   bool      `json:"is_private"`
	// ChatID binds an existing thread; when nil a new relay thread is created for the app's
	// persona.
	ChatID         *uuid.UUID `json:"chat_id"`
	InboundEnabled *bool      `json:"inbound_enabled"`
	AllowUserIDs   []string   `json:"allow_user_ids"`
	DenyUserIDs    []string   `json:"deny_user_ids"`
	// AnswerAgents lets other apps' bots start turns (within the relay's loop guard).
	AnswerAgents bool `json:"answer_agents"`
	// AllowUnrestricted acknowledges binding an existing thread that is not sandboxed. Without
	// it such a thread is refused with a 400.
	AllowUnrestricted bool `json:"allow_unrestricted"`
}

func (h *Handler) createBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	var req createBindingRequest
	if !h.decode(w, r, &req) {
		return
	}
	channelID := strings.TrimSpace(req.ChannelID)
	if req.AppID == uuid.Nil || !validChannelID(channelID) {
		h.bad(w, "app_id and channel_id are required")
		return
	}
	app, err := h.Store.GetSlackApp(r.Context(), userID, req.AppID)
	if err != nil {
		h.fail(w, err, "Failed to load the Slack app")
		return
	}
	channelName := strings.TrimPrefix(strings.TrimSpace(req.ChannelName), "#")
	chatID := uuid.Nil
	allowUnrestricted := false
	if req.ChatID != nil {
		chatID = *req.ChatID
		if allowUnrestricted, ok = h.bindableChat(w, r, userID, chatID, req.AllowUnrestricted); !ok {
			return
		}
	} else {
		// A relay thread is shared with other people, so it is created sandboxed: it reads only
		// itself and writes only Chat-scoped memories, starts with the sandbox's default-off
		// tools (models.SandboxDefaultDisabledTools, applied by CreateChat), and has no MCP
		// connectors (CreateChat attaches none). The owner can change this in its settings.
		chat, err := h.Store.CreateChat(r.Context(), userID, models.Chat{
			Name:          relayThreadName(channelName),
			PersonalityID: app.PersonalityID,
			ContextScope:  models.ContextScopeSandbox,
		})
		if err != nil {
			h.fail(w, err, "Failed to create the relay thread")
			return
		}
		chatID = chat.ID
	}
	inbound := true
	if req.InboundEnabled != nil {
		inbound = *req.InboundEnabled
	}
	binding, err := h.Store.CreateSlackBinding(r.Context(), userID, req.AppID, models.SlackBindingCreate{
		ChatID:            chatID,
		ChannelID:         channelID,
		ChannelName:       channelName,
		IsPrivate:         req.IsPrivate,
		InboundEnabled:    inbound,
		AllowUserIDs:      cleanIDs(req.AllowUserIDs),
		DenyUserIDs:       cleanIDs(req.DenyUserIDs),
		AnswerAgents:      req.AnswerAgents,
		AllowUnrestricted: allowUnrestricted,
	})
	if err != nil {
		if req.ChatID == nil {
			// Do not leave the relay thread created for this binding behind.
			if derr := h.Store.DeleteChat(r.Context(), userID, chatID); derr != nil {
				h.Logger.Warn("slack: could not remove the unused relay thread", zap.Error(derr))
			}
		}
		h.fail(w, err, "Failed to connect the channel")
		return
	}
	h.markOne(r.Context(), userID, binding)
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusCreated, binding)
}

func relayThreadName(channel string) string {
	if channel == "" {
		return "Slack relay"
	}
	return "Slack · #" + channel
}

type updateBindingRequest struct {
	ChatID         *uuid.UUID `json:"chat_id"`
	InboundEnabled *bool      `json:"inbound_enabled"`
	AllowUserIDs   *[]string  `json:"allow_user_ids"`
	DenyUserIDs    *[]string  `json:"deny_user_ids"`
	ChannelName    *string    `json:"channel_name"`
	AnswerAgents   *bool      `json:"answer_agents"`
	// AllowUnrestricted acknowledges (true) or withdraws (false) the owner's acknowledgement
	// that the thread is not sandboxed. Repointing to such a thread requires it; sending it
	// alone acknowledges the current thread.
	AllowUnrestricted *bool `json:"allow_unrestricted"`
	// Reactivate clears a broken status (after fixing the app's access).
	Reactivate bool `json:"reactivate"`
}

func (h *Handler) updateBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var req updateBindingRequest
	if !h.decode(w, r, &req) {
		return
	}
	p := models.SlackBindingPatch{
		ChatID: req.ChatID, InboundEnabled: req.InboundEnabled, ChannelName: req.ChannelName, AnswerAgents: req.AnswerAgents,
	}
	if req.AllowUserIDs != nil {
		ids := cleanIDs(*req.AllowUserIDs)
		p.AllowUserIDs = &ids
	}
	if req.DenyUserIDs != nil {
		ids := cleanIDs(*req.DenyUserIDs)
		p.DenyUserIDs = &ids
	}
	if req.Reactivate {
		st, clear := models.SlackBindingActive, ""
		p.Status, p.LastError = &st, &clear
	}
	if req.ChatID != nil {
		// Repointing: the acknowledgement belongs to the thread it was given for, so it is
		// re-evaluated (and reset) for the new one.
		ack := req.AllowUnrestricted != nil && *req.AllowUnrestricted
		record, ok := h.bindableChat(w, r, userID, *req.ChatID, ack)
		if !ok {
			return
		}
		p.AllowUnrestricted = &record
	} else if req.AllowUnrestricted != nil {
		current, err := h.Store.GetSlackBinding(r.Context(), userID, id)
		if err != nil {
			h.fail(w, err, "Failed to load the channel")
			return
		}
		record := false
		if *req.AllowUnrestricted {
			// Only recorded while the thread really is not sandboxed.
			chat, err := h.Store.GetChat(r.Context(), userID, current.ChatID)
			if err != nil {
				h.fail(w, err, "Failed to load the thread")
				return
			}
			record = !relaykit.RelayThreadOpenWithoutAcknowledgement(chat)
		}
		p.AllowUnrestricted = &record
	}
	binding, err := h.Store.UpdateSlackBinding(r.Context(), userID, id, p)
	if err != nil {
		h.fail(w, err, "Failed to update the channel")
		return
	}
	h.markOne(r.Context(), userID, binding)
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, binding)
}

func (h *Handler) deleteBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.Store.DeleteSlackBinding(r.Context(), userID, id); err != nil {
		h.fail(w, err, "Failed to disconnect the channel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ChatState is what a thread needs to show its Slack side: its bindings, which of them will
// post the next reply, and the recent crossings.
type ChatState struct {
	Bindings       []models.SlackBinding     `json:"bindings"`
	PendingBinding []uuid.UUID               `json:"pending_binding_ids"`
	Links          []models.SlackMessageLink `json:"links"`
}

func (h *Handler) chatState(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	chatID, ok := h.pathID(w, r, "chatId")
	if !ok {
		return
	}
	bindings, err := h.Store.ListSlackBindingsForChat(r.Context(), userID, chatID)
	if err != nil {
		h.fail(w, err, "Failed to load Slack state")
		return
	}
	h.markSandboxed(r.Context(), userID, bindings)
	state := ChatState{Bindings: bindings, PendingBinding: []uuid.UUID{}, Links: []models.SlackMessageLink{}}
	if len(bindings) > 0 {
		if state.PendingBinding, err = h.Store.ListSlackPendingPosts(r.Context(), userID, chatID); err != nil {
			h.fail(w, err, "Failed to load Slack state")
			return
		}
		if state.Links, err = h.Store.ListSlackLinksForChat(r.Context(), userID, chatID, 200); err != nil {
			h.fail(w, err, "Failed to load Slack state")
			return
		}
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, state)
}

func (h *Handler) setPending(w http.ResponseWriter, r *http.Request)   { h.pending(w, r, true) }
func (h *Handler) clearPending(w http.ResponseWriter, r *http.Request) { h.pending(w, r, false) }

func (h *Handler) pending(w http.ResponseWriter, r *http.Request, set bool) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	chatID, ok := h.pathID(w, r, "chatId")
	if !ok {
		return
	}
	bindingID, ok := h.pathID(w, r, "bindingId")
	if !ok {
		return
	}
	if _, err := h.Store.GetChat(r.Context(), userID, chatID); err != nil {
		h.fail(w, err, "Failed to load the thread")
		return
	}
	var err error
	if set {
		err = h.Store.SetSlackPendingPost(r.Context(), userID, bindingID, chatID, models.SlackPostFromComposer)
	} else {
		err = h.Store.ClearSlackPendingPost(r.Context(), userID, bindingID, chatID)
	}
	if err != nil {
		h.fail(w, err, "Failed to update the post setting")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func (h *Handler) user(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.Logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
	}
	return id, ok
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(mux.Vars(r)[name])
	if err != nil {
		h.bad(w, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) appCreds(w http.ResponseWriter, r *http.Request) (*models.SlackAppCredentials, bool) {
	userID, ok := h.user(w, r)
	if !ok {
		return nil, false
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return nil, false
	}
	creds, err := h.Store.GetSlackAppCredentials(r.Context(), userID, id)
	if err != nil {
		h.fail(w, err, "Failed to load the Slack app")
		return nil, false
	}
	return creds, true
}

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		h.bad(w, "invalid request body")
		return false
	}
	return true
}

func (h *Handler) bad(w http.ResponseWriter, msg string) {
	handlerutils.RespondWithError(w, h.Logger, http.StatusBadRequest, handlerutils.CodeNotSet, msg, nil)
}

// fail maps storage errors to responses: not-found (including another account's rows) is 404,
// conflicts are 409, anything else 500.
func (h *Handler) fail(w http.ResponseWriter, err error, msg string) {
	switch {
	case errors.Is(err, datastore.ErrSlackAppNotFound), errors.Is(err, datastore.ErrSlackBindingNotFound),
		errors.Is(err, datastore.ErrChatNotFound), errors.Is(err, datastore.ErrPersonalityNotFound):
		handlerutils.RespondWithError(w, h.Logger, http.StatusNotFound, handlerutils.CodeNotSet, "Not found", nil)
	case errors.Is(err, datastore.ErrSlackAppExists):
		handlerutils.RespondWithError(w, h.Logger, http.StatusConflict, handlerutils.CodeNotSet,
			"That Slack app is already connected, or the persona already has an app in that workspace.", nil)
	case errors.Is(err, datastore.ErrSlackBindingExists):
		handlerutils.RespondWithError(w, h.Logger, http.StatusConflict, handlerutils.CodeNotSet,
			"That channel is already connected for this app.", nil)
	case errors.Is(err, datastore.ErrSlackBindingPaused):
		handlerutils.RespondWithError(w, h.Logger, http.StatusConflict, handlerutils.CodeNotSet,
			"That channel is paused or needs attention, so nothing is posted there. Turn it back on first.", nil)
	default:
		handlerutils.RespondWithError(w, h.Logger, http.StatusInternalServerError, handlerutils.CodeNotSet, msg, err)
	}
}

// slackFailed answers an error from Slack: a refused token is the caller's to fix (400, with
// authMsg when given), anything else is Slack's side (502).
func (h *Handler) slackFailed(w http.ResponseWriter, err error, authMsg string) {
	switch {
	case slackrelay.IsAuthError(err):
		if authMsg == "" {
			authMsg = "Slack rejected the app's token. Paste a new one."
		}
		h.bad(w, authMsg)
	case slackrelay.IsChannelError(err):
		handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet, "The app cannot see that channel.", nil)
	default:
		var slackErr *slackrelay.Error
		if errors.As(err, &slackErr) {
			if authMsg != "" && slackErr.Code == "not_a_bot_token" {
				h.bad(w, authMsg)
				return
			}
			handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet,
				"Slack refused the request ("+slackErr.Code+").", nil)
			return
		}
		handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet, "Slack could not be reached. Try again.", err)
	}
}

// cleanToken accepts a token pasted with a "Bearer " prefix or surrounding spaces.
func cleanToken(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "Bearer ")
	return strings.TrimSpace(t)
}

// validChannelID reports whether s looks like a Slack conversation id (C… or G…).
func validChannelID(s string) bool {
	return len(s) >= 2 && (s[0] == 'C' || s[0] == 'G') && strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") == ""
}

// cleanIDs trims, upper-cases, drops empties and anything that is not a Slack user id (U… or
// W…), and de-duplicates.
func cleanIDs(ids []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		id = strings.ToUpper(strings.TrimSpace(id))
		if len(id) < 2 || (id[0] != 'U' && id[0] != 'W') || seen[id] || strings.Trim(id, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
