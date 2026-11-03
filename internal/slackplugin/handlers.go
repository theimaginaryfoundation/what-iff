package slackplugin

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/slackrelay"
	"go.uber.org/zap"
)

// Store is the storage the routes need (the datastore satisfies it).
type Store interface {
	CreateSlackApp(ctx context.Context, userID uuid.UUID, in models.SlackAppCreate) (*models.SlackApp, error)
	ListSlackApps(ctx context.Context, userID uuid.UUID) ([]models.SlackApp, error)
	GetSlackApp(ctx context.Context, userID, id uuid.UUID) (*models.SlackApp, error)
	GetSlackAppCredentials(ctx context.Context, userID, id uuid.UUID) (*models.SlackAppCredentials, error)
	UpdateSlackApp(ctx context.Context, userID, id uuid.UUID, p models.SlackAppPatch) (*models.SlackApp, error)
	DeleteSlackApp(ctx context.Context, userID, id uuid.UUID) error

	CreateSlackBinding(ctx context.Context, userID, appID uuid.UUID, in models.SlackBindingCreate) (*models.SlackBinding, error)
	ListSlackBindings(ctx context.Context, userID uuid.UUID) ([]models.SlackBinding, error)
	ListSlackBindingsForChat(ctx context.Context, userID, chatID uuid.UUID) ([]models.SlackBinding, error)
	GetSlackBinding(ctx context.Context, userID, id uuid.UUID) (*models.SlackBinding, error)
	UpdateSlackBinding(ctx context.Context, userID, id uuid.UUID, p models.SlackBindingPatch) (*models.SlackBinding, error)
	DeleteSlackBinding(ctx context.Context, userID, id uuid.UUID) error

	ListSlackLinksForChat(ctx context.Context, userID, chatID uuid.UUID, limit int) ([]models.SlackMessageLink, error)
	SetSlackPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID, source models.SlackPendingPostSource) error
	ClearSlackPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID) error
	ListSlackPendingPosts(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error)

	GetPersonality(ctx context.Context, userID, id uuid.UUID) (*models.Personality, error)
	GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error)
	CreateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error)
	DeleteChat(ctx context.Context, userID, id uuid.UUID) error
}

// Slack is what the routes need from Slack's Web API (*slackrelay.API satisfies it).
type Slack interface {
	AuthTest(ctx context.Context, botToken string) (*slackrelay.Identity, error)
	Bot(ctx context.Context, botToken, botID string) (*slackrelay.BotInfo, error)
	Channels(ctx context.Context, botToken string) ([]slackrelay.Channel, error)
	JoinChannel(ctx context.Context, botToken, channel string) (*slackrelay.Channel, error)
}

// Handler serves /api/slack/... (behind auth).
type Handler struct {
	Store  Store
	Slack  Slack
	Logger *zap.Logger
	// CheckAppToken opens a Socket Mode connection with an app-level token and returns the app
	// it belongs to ("" when Slack does not say), so a pasted pair of tokens is known to be one
	// app's.
	CheckAppToken func(ctx context.Context, appToken string) (string, error)
	// Changed is told when an app changed, so the gateway reconciles now instead of at its next
	// interval. Nil does nothing.
	Changed func()
}

// Register mounts the routes on r (already prefixed with /slack).
func (h *Handler) Register(r *mux.Router) {
	r.HandleFunc("/manifest", h.manifest).Methods(http.MethodGet)
	r.HandleFunc("/apps", h.listApps).Methods(http.MethodGet)
	r.HandleFunc("/apps", h.createApp).Methods(http.MethodPost)
	r.HandleFunc("/apps/{id}", h.updateApp).Methods(http.MethodPatch)
	r.HandleFunc("/apps/{id}", h.deleteApp).Methods(http.MethodDelete)
	r.HandleFunc("/apps/{id}/channels", h.listChannels).Methods(http.MethodGet)
	r.HandleFunc("/apps/{id}/channels/{channelId}/join", h.joinChannel).Methods(http.MethodPost)

	r.HandleFunc("/bindings", h.listBindings).Methods(http.MethodGet)
	r.HandleFunc("/bindings", h.createBinding).Methods(http.MethodPost)
	r.HandleFunc("/bindings/{id}", h.updateBinding).Methods(http.MethodPatch)
	r.HandleFunc("/bindings/{id}", h.deleteBinding).Methods(http.MethodDelete)

	r.HandleFunc("/chats/{chatId}", h.chatState).Methods(http.MethodGet)
	r.HandleFunc("/chats/{chatId}/pending/{bindingId}", h.setPending).Methods(http.MethodPut)
	r.HandleFunc("/chats/{chatId}/pending/{bindingId}", h.clearPending).Methods(http.MethodDelete)
}

func (h *Handler) changed() {
	if h.Changed != nil {
		h.Changed()
	}
}

// ManifestResponse is the app manifest for a persona and the link that creates the app from it.
type ManifestResponse struct {
	Manifest  map[string]any `json:"manifest"`
	CreateURL string         `json:"create_url"`
	Scopes    []string       `json:"scopes"`
	Events    []string       `json:"events"`
}

func (h *Handler) manifest(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	personaID, err := uuid.Parse(r.URL.Query().Get("personality_id"))
	if err != nil {
		h.bad(w, "personality_id is required")
		return
	}
	persona, err := h.Store.GetPersonality(r.Context(), userID, personaID)
	if err != nil {
		h.fail(w, err, "Failed to load the persona")
		return
	}
	m := slackrelay.Manifest(persona.Name)
	link, err := slackrelay.CreateAppURL(m)
	if err != nil {
		h.fail(w, err, "Failed to build the manifest")
		return
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, ManifestResponse{
		Manifest: m, CreateURL: link, Scopes: slackrelay.BotScopes, Events: slackrelay.BotEvents,
	})
}

// AppResponse is an app plus its settings page in Slack (where its icon is set).
type AppResponse struct {
	models.SlackApp
	SettingsURL string `json:"settings_url"`
}

func appResponse(a models.SlackApp) AppResponse {
	return AppResponse{SlackApp: a, SettingsURL: slackrelay.AppSettingsURL(a.AppID)}
}

func (h *Handler) listApps(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	apps, err := h.Store.ListSlackApps(r.Context(), userID)
	if err != nil {
		h.fail(w, err, "Failed to list Slack apps")
		return
	}
	out := make([]AppResponse, 0, len(apps))
	for _, a := range apps {
		out = append(out, appResponse(a))
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, out)
}

type createAppRequest struct {
	PersonalityID uuid.UUID `json:"personality_id"`
	BotToken      string    `json:"bot_token"`
	AppToken      string    `json:"app_token"`
}

// checked is a pair of tokens Slack confirmed belong to one app.
type checked struct {
	id    *slackrelay.Identity
	appID string
	name  string
}

// checkTokens asks Slack whose the tokens are: the bot token's workspace, bot user and app,
// and that the app-level token is the same app's. When ok is false the response is written.
func (h *Handler) checkTokens(w http.ResponseWriter, r *http.Request, botToken, appToken string) (*checked, bool) {
	if !strings.HasPrefix(botToken, "xoxb-") {
		h.bad(w, "The bot token starts with xoxb-. Copy it from OAuth & Permissions after installing the app.")
		return nil, false
	}
	if !strings.HasPrefix(appToken, "xapp-") {
		h.bad(w, "The app-level token starts with xapp-. Create one with the connections:write scope under Basic Information.")
		return nil, false
	}
	id, err := h.Slack.AuthTest(r.Context(), botToken)
	if err != nil {
		h.slackFailed(w, err, "Slack rejected the bot token. Copy the Bot User OAuth Token (xoxb-) from OAuth & Permissions.")
		return nil, false
	}
	bot, err := h.Slack.Bot(r.Context(), botToken, id.BotID)
	if err != nil {
		h.slackFailed(w, err, "Slack would not say which app the bot belongs to. Check the app has the users:read scope, then reinstall it.")
		return nil, false
	}
	if h.CheckAppToken != nil {
		appID, err := h.CheckAppToken(r.Context(), appToken)
		if err != nil {
			h.slackFailed(w, err, "Slack rejected the app-level token. Create one with the connections:write scope, and check Socket Mode is on.")
			return nil, false
		}
		if appID != "" && appID != bot.AppID {
			h.bad(w, "The app-level token belongs to a different Slack app than the bot token.")
			return nil, false
		}
	}
	name := bot.Name
	if name == "" {
		name = id.User
	}
	return &checked{id: id, appID: bot.AppID, name: name}, true
}

func (h *Handler) createApp(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	var req createAppRequest
	if !h.decode(w, r, &req) {
		return
	}
	botToken, appToken := cleanToken(req.BotToken), cleanToken(req.AppToken)
	if req.PersonalityID == uuid.Nil || botToken == "" || appToken == "" {
		h.bad(w, "personality_id, bot_token and app_token are required")
		return
	}
	if _, err := h.Store.GetPersonality(r.Context(), userID, req.PersonalityID); err != nil {
		h.fail(w, err, "Failed to load the persona")
		return
	}
	c, ok := h.checkTokens(w, r, botToken, appToken)
	if !ok {
		return
	}
	app, err := h.Store.CreateSlackApp(r.Context(), userID, models.SlackAppCreate{
		PersonalityID: req.PersonalityID,
		TeamID:        c.id.TeamID,
		TeamName:      c.id.Team,
		TeamURL:       c.id.URL,
		AppID:         c.appID,
		BotUserID:     c.id.UserID,
		BotID:         c.id.BotID,
		BotName:       c.name,
		BotToken:      botToken,
		AppToken:      appToken,
	})
	if err != nil {
		h.fail(w, err, "Failed to save the Slack app")
		return
	}
	h.Logger.Info("slack app added", zap.String("slack_app_id", app.ID.String()), zap.String("user_id", userID.String()))
	h.changed()
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusCreated, appResponse(*app))
}

type updateAppRequest struct {
	// BotToken and AppToken replace the app's tokens (they must be the same app's).
	BotToken *string `json:"bot_token"`
	AppToken *string `json:"app_token"`
	// Refresh re-reads the workspace's and the bot's names from Slack.
	Refresh bool `json:"refresh"`
	// Enabled pauses (false) or resumes (true) the app.
	Enabled *bool `json:"enabled"`
}

func (h *Handler) updateApp(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var req updateAppRequest
	if !h.decode(w, r, &req) {
		return
	}
	creds, err := h.Store.GetSlackAppCredentials(r.Context(), userID, id)
	if err != nil {
		h.fail(w, err, "Failed to load the Slack app")
		return
	}
	current, err := h.Store.GetSlackApp(r.Context(), userID, id)
	if err != nil {
		h.fail(w, err, "Failed to load the Slack app")
		return
	}
	patch := models.SlackAppPatch{}
	botToken, appToken := creds.BotToken, creds.AppToken
	if req.BotToken != nil {
		botToken = cleanToken(*req.BotToken)
	}
	if req.AppToken != nil {
		appToken = cleanToken(*req.AppToken)
	}
	if req.BotToken != nil || req.AppToken != nil || req.Refresh {
		c, ok := h.checkTokens(w, r, botToken, appToken)
		if !ok {
			return
		}
		if c.appID != creds.AppID || c.id.TeamID != creds.TeamID {
			h.bad(w, "Those tokens belong to a different Slack app or workspace. Add it as a new app instead.")
			return
		}
		clear := ""
		patch.TeamName, patch.TeamURL, patch.BotName, patch.LastError = &c.id.Team, &c.id.URL, &c.name, &clear
		if req.BotToken != nil {
			patch.BotToken = &botToken
		}
		if req.AppToken != nil {
			patch.AppToken = &appToken
		}
		if current.Status != models.SlackAppDisabled {
			// Working tokens clear invalid_token; an app the owner paused stays paused until
			// they resume it (enabled: true below).
			st := models.SlackAppActive
			patch.Status = &st
		}
	}
	if req.Enabled != nil {
		st := models.SlackAppDisabled
		if *req.Enabled {
			st = models.SlackAppActive
		}
		// Resuming only undoes a pause: an app whose tokens Slack refused stays invalid_token
		// until new tokens or a refresh (above) clear it, so the card does not show "Active"
		// over tokens that still do not work.
		if !*req.Enabled || current.Status == models.SlackAppDisabled {
			patch.Status = &st
		}
	}
	app, err := h.Store.UpdateSlackApp(r.Context(), userID, id, patch)
	if err != nil {
		h.fail(w, err, "Failed to update the Slack app")
		return
	}
	h.changed()
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, appResponse(*app))
}

func (h *Handler) deleteApp(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.Store.DeleteSlackApp(r.Context(), userID, id); err != nil {
		h.fail(w, err, "Failed to remove the Slack app")
		return
	}
	h.changed()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listChannels(w http.ResponseWriter, r *http.Request) {
	creds, ok := h.appCreds(w, r)
	if !ok {
		return
	}
	channels, err := h.Slack.Channels(r.Context(), creds.BotToken)
	if err != nil {
		h.slackFailed(w, err, "")
		return
	}
	if channels == nil {
		channels = []slackrelay.Channel{}
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, channels)
}

// joinChannel adds the app to a public channel, so it can be bound without inviting it in
// Slack. Private channels need an invite there.
func (h *Handler) joinChannel(w http.ResponseWriter, r *http.Request) {
	creds, ok := h.appCreds(w, r)
	if !ok {
		return
	}
	ch, err := h.Slack.JoinChannel(r.Context(), creds.BotToken, mux.Vars(r)["channelId"])
	if err != nil {
		var slackErr *slackrelay.Error
		if errors.As(err, &slackErr) && slackErr.Code == "method_not_supported_for_channel_type" {
			h.bad(w, "That channel is private: invite the persona in Slack (/invite) instead.")
			return
		}
		h.slackFailed(w, err, "")
		return
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, ch)
}
