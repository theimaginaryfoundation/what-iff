package discordplugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/discordrelay"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// Store is the storage the routes need (the datastore satisfies it).
type Store interface {
	CreateDiscordBot(ctx context.Context, userID uuid.UUID, in models.DiscordBotCreate) (*models.DiscordBot, error)
	ListDiscordBots(ctx context.Context, userID uuid.UUID) ([]models.DiscordBot, error)
	GetDiscordBotCredentials(ctx context.Context, userID, id uuid.UUID) (*models.DiscordBotCredentials, error)
	UpdateDiscordBot(ctx context.Context, userID, id uuid.UUID, p models.DiscordBotPatch) (*models.DiscordBot, error)
	DeleteDiscordBot(ctx context.Context, userID, id uuid.UUID) error

	CreateDiscordBinding(ctx context.Context, userID, botID uuid.UUID, in models.DiscordBindingCreate) (*models.DiscordBinding, error)
	ListDiscordBindings(ctx context.Context, userID uuid.UUID) ([]models.DiscordBinding, error)
	ListDiscordBindingsForChat(ctx context.Context, userID, chatID uuid.UUID) ([]models.DiscordBinding, error)
	GetDiscordBinding(ctx context.Context, userID, id uuid.UUID) (*models.DiscordBinding, error)
	UpdateDiscordBinding(ctx context.Context, userID, id uuid.UUID, p models.DiscordBindingPatch) (*models.DiscordBinding, error)
	DeleteDiscordBinding(ctx context.Context, userID, id uuid.UUID) error

	ListDiscordLinksForChat(ctx context.Context, userID, chatID uuid.UUID, limit int) ([]models.DiscordMessageLink, error)
	SetDiscordPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID, source models.DiscordPendingPostSource) error
	ClearDiscordPendingPost(ctx context.Context, userID, bindingID, chatID uuid.UUID) error
	ListDiscordPendingPosts(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error)

	GetPersonality(ctx context.Context, userID, id uuid.UUID) (*models.Personality, error)
	GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error)
	CreateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error)
	DeleteChat(ctx context.Context, userID, id uuid.UUID) error
	GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error)
}

// Handler serves /api/discord/... (behind auth).
type Handler struct {
	Store   Store
	Discord discordrelay.Discord
	// Files is the object store, for reading a persona's portrait when syncing the
	// bot's avatar. Nil disables avatar sync (the name is still synced).
	Files  storage.FileStore
	Logger *zap.Logger
}

// Register mounts the routes on r (already prefixed with /discord).
func (h *Handler) Register(r *mux.Router) {
	r.HandleFunc("/bots", h.listBots).Methods(http.MethodGet)
	r.HandleFunc("/bots", h.createBot).Methods(http.MethodPost)
	r.HandleFunc("/bots/{id}", h.updateBot).Methods(http.MethodPatch)
	r.HandleFunc("/bots/{id}", h.deleteBot).Methods(http.MethodDelete)
	r.HandleFunc("/bots/{id}/sync-profile", h.syncProfile).Methods(http.MethodPost)
	r.HandleFunc("/bots/{id}/guilds", h.listGuilds).Methods(http.MethodGet)
	r.HandleFunc("/bots/{id}/guilds/{guildId}/channels", h.listChannels).Methods(http.MethodGet)

	r.HandleFunc("/bindings", h.listBindings).Methods(http.MethodGet)
	r.HandleFunc("/bindings", h.createBinding).Methods(http.MethodPost)
	r.HandleFunc("/bindings/{id}", h.updateBinding).Methods(http.MethodPatch)
	r.HandleFunc("/bindings/{id}", h.deleteBinding).Methods(http.MethodDelete)

	r.HandleFunc("/chats/{chatId}", h.chatState).Methods(http.MethodGet)
	r.HandleFunc("/chats/{chatId}/pending/{bindingId}", h.setPending).Methods(http.MethodPut)
	r.HandleFunc("/chats/{chatId}/pending/{bindingId}", h.clearPending).Methods(http.MethodDelete)
}

// BotResponse is a bot plus the link that adds it to a server.
type BotResponse struct {
	models.DiscordBot
	InviteURL string `json:"invite_url"`
}

func botResponse(b models.DiscordBot) BotResponse {
	return BotResponse{DiscordBot: b, InviteURL: discordrelay.InviteURL(b.ApplicationID)}
}

func (h *Handler) listBots(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	bots, err := h.Store.ListDiscordBots(r.Context(), userID)
	if err != nil {
		h.fail(w, err, "Failed to list Discord bots")
		return
	}
	out := make([]BotResponse, 0, len(bots))
	for _, b := range bots {
		out = append(out, botResponse(b))
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, out)
}

type createBotRequest struct {
	PersonalityID uuid.UUID `json:"personality_id"`
	Token         string    `json:"token"`
}

func (h *Handler) createBot(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	var req createBotRequest
	if !h.decode(w, r, &req) {
		return
	}
	token := cleanToken(req.Token)
	if req.PersonalityID == uuid.Nil || token == "" {
		h.bad(w, "personality_id and token are required")
		return
	}
	id, err := h.Discord.Identify(r.Context(), token)
	if err != nil {
		h.identifyFailed(w, err)
		return
	}
	bot, err := h.Store.CreateDiscordBot(r.Context(), userID, models.DiscordBotCreate{
		PersonalityID:  req.PersonalityID,
		ApplicationID:  id.ApplicationID,
		BotUserID:      id.BotUserID,
		BotUsername:    id.Username,
		Token:          token,
		MessageContent: id.MessageContent,
	})
	if err != nil {
		h.fail(w, err, "Failed to save the Discord bot")
		return
	}
	h.Logger.Info("discord bot added", zap.String("bot_id", bot.ID.String()), zap.String("user_id", userID.String()))
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusCreated, botResponse(*bot))
}

type updateBotRequest struct {
	// Token replaces the bot's token (it must belong to the same bot).
	Token *string `json:"token"`
	// Refresh re-reads the bot's name and Message Content setting from Discord.
	Refresh bool `json:"refresh"`
	// Enabled pauses (false) or resumes (true) the bot.
	Enabled *bool `json:"enabled"`
}

func (h *Handler) updateBot(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	botID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var req updateBotRequest
	if !h.decode(w, r, &req) {
		return
	}
	creds, err := h.Store.GetDiscordBotCredentials(r.Context(), userID, botID)
	if err != nil {
		h.fail(w, err, "Failed to load the Discord bot")
		return
	}
	patch := models.DiscordBotPatch{}
	token := creds.Token
	if req.Token != nil {
		token = cleanToken(*req.Token)
		if token == "" {
			h.bad(w, "token must not be empty")
			return
		}
	}
	if req.Token != nil {
		// The old token's cached REST session is no longer needed (or valid).
		defer h.forgetToken(creds.Token)
	}
	if req.Token != nil || req.Refresh {
		id, err := h.Discord.Identify(r.Context(), token)
		if err != nil {
			h.identifyFailed(w, err)
			return
		}
		if id.BotUserID != creds.BotUserID {
			h.bad(w, "That token belongs to a different bot. Add it as a new bot instead.")
			return
		}
		clear := ""
		patch.BotUsername, patch.MessageContent = &id.Username, &id.MessageContent
		patch.LastError = &clear
		if h.botStatus(r.Context(), userID, botID) != models.DiscordBotDisabled {
			// A working token clears invalid_token; a bot the owner paused stays paused until
			// they resume it (enabled: true below).
			active := models.DiscordBotActive
			patch.Status = &active
		}
		if req.Token != nil {
			patch.Token = &token
		}
	}
	if req.Enabled != nil {
		st := models.DiscordBotDisabled
		if *req.Enabled {
			st = models.DiscordBotActive
		}
		patch.Status = &st
	}
	bot, err := h.Store.UpdateDiscordBot(r.Context(), userID, botID, patch)
	if err != nil {
		h.fail(w, err, "Failed to update the Discord bot")
		return
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, botResponse(*bot))
}

func (h *Handler) deleteBot(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	botID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var token string
	if creds, err := h.Store.GetDiscordBotCredentials(r.Context(), userID, botID); err == nil {
		token = creds.Token
	}
	if err := h.Store.DeleteDiscordBot(r.Context(), userID, botID); err != nil {
		h.fail(w, err, "Failed to remove the Discord bot")
		return
	}
	h.forgetToken(token)
	w.WriteHeader(http.StatusNoContent)
}

// forgetToken drops the REST client's cached session for a token that was replaced
// or whose bot was removed.
func (h *Handler) forgetToken(token string) {
	if token == "" {
		return
	}
	if f, ok := h.Discord.(interface{ Forget(string) }); ok {
		f.Forget(token)
	}
}

// maxAvatarBytes is Discord's avatar upload limit.
const maxAvatarBytes = 10 << 20

// syncProfile sets the bot's username and avatar from its persona.
func (h *Handler) syncProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	botID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	creds, err := h.Store.GetDiscordBotCredentials(r.Context(), userID, botID)
	if err != nil {
		h.fail(w, err, "Failed to load the Discord bot")
		return
	}
	persona, err := h.Store.GetPersonality(r.Context(), userID, creds.PersonalityID)
	if err != nil {
		h.fail(w, err, "Failed to load the persona")
		return
	}
	avatar := h.avatarDataURI(r.Context(), userID, persona)
	username := discordUsername(persona.Name)
	if err := h.Discord.SetProfile(r.Context(), creds.Token, username, avatar); err != nil {
		h.Logger.Warn("discord profile sync failed", zap.Error(err))
		handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet,
			"Discord did not accept the change. Usernames can only be changed about twice an hour; try again later.", nil)
		return
	}
	bot, err := h.Store.UpdateDiscordBot(r.Context(), userID, botID, models.DiscordBotPatch{BotUsername: &username})
	if err != nil {
		h.fail(w, err, "Failed to update the Discord bot")
		return
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, map[string]any{
		"bot":           botResponse(*bot),
		"avatar_synced": avatar != "",
	})
}

// avatarDataURI reads the persona's portrait as a data URI, or "" when there is
// none or it cannot be read (the name is synced regardless).
func (h *Handler) avatarDataURI(ctx context.Context, userID uuid.UUID, p *models.Personality) string {
	if h.Files == nil || p.CoverImageID == nil {
		return ""
	}
	att, err := h.Store.GetFileAttachment(ctx, userID, *p.CoverImageID)
	if err != nil || att == nil || att.S3Key == "" {
		return ""
	}
	data, err := h.Files.DownloadFile(ctx, att.S3Key)
	if err != nil || len(data) == 0 || len(data) > maxAvatarBytes {
		return ""
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return ""
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// discordUsername fits a persona name to Discord's 2-32 character username rule.
func discordUsername(name string) string {
	name = strings.TrimSpace(name)
	r := []rune(name)
	if len(r) > 32 {
		r = r[:32]
	}
	if len(r) < 2 {
		return "Persona"
	}
	return string(r)
}

func (h *Handler) listGuilds(w http.ResponseWriter, r *http.Request) {
	creds, ok := h.botCreds(w, r)
	if !ok {
		return
	}
	guilds, err := h.Discord.Guilds(r.Context(), creds.Token)
	if err != nil {
		h.discordFailed(w, err)
		return
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, guilds)
}

func (h *Handler) listChannels(w http.ResponseWriter, r *http.Request) {
	creds, ok := h.botCreds(w, r)
	if !ok {
		return
	}
	channels, err := h.Discord.Channels(r.Context(), creds.Token, mux.Vars(r)["guildId"])
	if err != nil {
		h.discordFailed(w, err)
		return
	}
	if channels == nil {
		channels = []discordrelay.Channel{}
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, channels)
}

func (h *Handler) listBindings(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	bindings, err := h.Store.ListDiscordBindings(r.Context(), userID)
	if err != nil {
		h.fail(w, err, "Failed to list Discord channels")
		return
	}
	h.markSandboxed(r.Context(), userID, bindings)
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, bindings)
}

// unrestrictedBindMsg is the 400 for binding a thread that is not sandboxed. A
// Discord channel is a public surface: whoever the allow list lets tag the bot
// drives this thread, so a thread that can read the account hands them the owner's
// name, memories, other conversations, files and scratchpad.
const unrestrictedBindMsg = "That thread is not sandboxed, so anyone allowed to tag the bot could use your memories, " +
	"other conversations, files and scratchpad. Sandbox the thread first, or confirm with allow_unrestricted."

// bindableChat decides whether chatID may be bound to a Discord channel (or a
// binding repointed to it). A sandboxed thread always may; any other only with the
// owner's explicit acknowledgement. record is the value to store as the binding's
// allow_unrestricted: true only for an acknowledged thread that is not sandboxed. When ok is false the response has been written.
func (h *Handler) bindableChat(w http.ResponseWriter, r *http.Request, userID, chatID uuid.UUID, acknowledged bool) (record, ok bool) {
	chat, err := h.Store.GetChat(r.Context(), userID, chatID)
	if err != nil {
		h.fail(w, err, "Failed to load the thread")
		return false, false
	}
	if discordrelay.RelayThreadOpenWithoutAcknowledgement(chat) {
		return false, true
	}
	if !acknowledged {
		h.bad(w, unrestrictedBindMsg)
		return false, false
	}
	return true, true
}

// markSandboxed fills each binding's computed ChatSandboxed from its thread's current
// sandbox state (the UI shows it; the relay re-checks it on every tag).
func (h *Handler) markSandboxed(ctx context.Context, userID uuid.UUID, bindings []models.DiscordBinding) {
	seen := map[uuid.UUID]*bool{}
	for i := range bindings {
		id := bindings[i].ChatID
		v, ok := seen[id]
		if !ok {
			if chat, err := h.Store.GetChat(ctx, userID, id); err == nil && chat != nil {
				r := discordrelay.RelayThreadOpenWithoutAcknowledgement(chat)
				v = &r
			}
			seen[id] = v
		}
		bindings[i].ChatSandboxed = v
	}
}

type createBindingRequest struct {
	BotID       uuid.UUID `json:"bot_id"`
	GuildID     string    `json:"guild_id"`
	GuildName   string    `json:"guild_name"`
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	// ChatID binds an existing thread; when nil a new relay thread is created for
	// the bot's persona.
	ChatID         *uuid.UUID `json:"chat_id"`
	InboundEnabled *bool      `json:"inbound_enabled"`
	AllowUserIDs   []string   `json:"allow_user_ids"`
	DenyUserIDs    []string   `json:"deny_user_ids"`
	// AllowUnrestricted acknowledges binding an existing thread that is not sandboxed.
	// Without it such a thread is refused with a 400.
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
	if req.BotID == uuid.Nil || strings.TrimSpace(req.GuildID) == "" || strings.TrimSpace(req.ChannelID) == "" {
		h.bad(w, "bot_id, guild_id and channel_id are required")
		return
	}
	creds, err := h.Store.GetDiscordBotCredentials(r.Context(), userID, req.BotID)
	if err != nil {
		h.fail(w, err, "Failed to load the Discord bot")
		return
	}
	chatID := uuid.Nil
	allowUnrestricted := false
	if req.ChatID != nil {
		chatID = *req.ChatID
		if allowUnrestricted, ok = h.bindableChat(w, r, userID, chatID, req.AllowUnrestricted); !ok {
			return
		}
	} else {
		personaID := creds.PersonalityID
		// A relay thread is a public surface, so it is created sandboxed: it reads only itself
		// (never the account's memories, the personality's scratchpad or other conversations)
		// and writes only Chat-scoped memories. The sandbox brings its own defaults: the tools
		// that spend credits or act beyond the conversation start switched off
		// (models.SandboxDefaultDisabledTools, applied by CreateChat) and no MCP connector is
		// attached (CreateChat attaches none; only the app's create route adds the default
		// ones). The user can switch any of this on in the thread's settings.
		chat, err := h.Store.CreateChat(r.Context(), userID, models.Chat{
			Name:          relayThreadName(req.ChannelName),
			PersonalityID: personaID,
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
	binding, err := h.Store.CreateDiscordBinding(r.Context(), userID, req.BotID, models.DiscordBindingCreate{
		ChatID:         chatID,
		GuildID:        strings.TrimSpace(req.GuildID),
		ChannelID:      strings.TrimSpace(req.ChannelID),
		GuildName:      strings.TrimSpace(req.GuildName),
		ChannelName:    strings.TrimPrefix(strings.TrimSpace(req.ChannelName), "#"),
		InboundEnabled: inbound,
		AllowUserIDs:   cleanIDs(req.AllowUserIDs),
		DenyUserIDs:    cleanIDs(req.DenyUserIDs),

		AllowUnrestricted: allowUnrestricted,
	})
	if err != nil {
		if req.ChatID == nil {
			// Do not leave the relay thread created for this binding behind.
			if derr := h.Store.DeleteChat(r.Context(), userID, chatID); derr != nil {
				h.Logger.Warn("discord: could not remove the unused relay thread", zap.Error(derr))
			}
		}
		h.fail(w, err, "Failed to connect the channel")
		return
	}
	h.markOne(r.Context(), userID, binding)
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusCreated, binding)
}

func (h *Handler) markOne(ctx context.Context, userID uuid.UUID, b *models.DiscordBinding) {
	bs := []models.DiscordBinding{*b}
	h.markSandboxed(ctx, userID, bs)
	b.ChatSandboxed = bs[0].ChatSandboxed
}

func relayThreadName(channel string) string {
	channel = strings.TrimPrefix(strings.TrimSpace(channel), "#")
	if channel == "" {
		return "Discord relay"
	}
	return "Discord · #" + channel
}

type updateBindingRequest struct {
	ChatID         *uuid.UUID `json:"chat_id"`
	InboundEnabled *bool      `json:"inbound_enabled"`
	AllowUserIDs   *[]string  `json:"allow_user_ids"`
	DenyUserIDs    *[]string  `json:"deny_user_ids"`
	ChannelName    *string    `json:"channel_name"`
	GuildName      *string    `json:"guild_name"`
	// AllowUnrestricted acknowledges (true) or withdraws (false) the owner's
	// acknowledgement that the thread is not sandboxed. Repointing to
	// such a thread requires it; sending it alone acknowledges the current thread.
	AllowUnrestricted *bool `json:"allow_unrestricted"`
	// Reactivate clears a broken status (after fixing the bot's access).
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
	p := models.DiscordBindingPatch{
		ChatID: req.ChatID, InboundEnabled: req.InboundEnabled,
		ChannelName: req.ChannelName, GuildName: req.GuildName,
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
		st, clear := models.DiscordBindingActive, ""
		p.Status, p.LastError = &st, &clear
	}
	if req.ChatID != nil {
		// Repointing: the acknowledgement belongs to the thread it was given for, so it
		// is re-evaluated (and reset) for the new one.
		ack := req.AllowUnrestricted != nil && *req.AllowUnrestricted
		record, ok := h.bindableChat(w, r, userID, *req.ChatID, ack)
		if !ok {
			return
		}
		p.AllowUnrestricted = &record
	} else if req.AllowUnrestricted != nil {
		current, err := h.Store.GetDiscordBinding(r.Context(), userID, id)
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
			record = !discordrelay.RelayThreadOpenWithoutAcknowledgement(chat)
		}
		p.AllowUnrestricted = &record
	}
	binding, err := h.Store.UpdateDiscordBinding(r.Context(), userID, id, p)
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
	if err := h.Store.DeleteDiscordBinding(r.Context(), userID, id); err != nil {
		h.fail(w, err, "Failed to disconnect the channel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ChatState is what a thread needs to show its Discord side: its bindings, which
// of them will post the next reply, and the recent crossings.
type ChatState struct {
	Bindings       []models.DiscordBinding     `json:"bindings"`
	PendingBinding []uuid.UUID                 `json:"pending_binding_ids"`
	Links          []models.DiscordMessageLink `json:"links"`
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
	bindings, err := h.Store.ListDiscordBindingsForChat(r.Context(), userID, chatID)
	if err != nil {
		h.fail(w, err, "Failed to load Discord state")
		return
	}
	h.markSandboxed(r.Context(), userID, bindings)
	state := ChatState{Bindings: bindings, PendingBinding: []uuid.UUID{}, Links: []models.DiscordMessageLink{}}
	if len(bindings) > 0 {
		if state.PendingBinding, err = h.Store.ListDiscordPendingPosts(r.Context(), userID, chatID); err != nil {
			h.fail(w, err, "Failed to load Discord state")
			return
		}
		if state.Links, err = h.Store.ListDiscordLinksForChat(r.Context(), userID, chatID, 200); err != nil {
			h.fail(w, err, "Failed to load Discord state")
			return
		}
	}
	handlerutils.RespondWithJSON(w, h.Logger, http.StatusOK, state)
}

func (h *Handler) setPending(w http.ResponseWriter, r *http.Request) {
	h.pending(w, r, true)
}

func (h *Handler) clearPending(w http.ResponseWriter, r *http.Request) {
	h.pending(w, r, false)
}

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
		err = h.Store.SetDiscordPendingPost(r.Context(), userID, bindingID, chatID, models.DiscordPostFromComposer)
	} else {
		err = h.Store.ClearDiscordPendingPost(r.Context(), userID, bindingID, chatID)
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

func (h *Handler) botCreds(w http.ResponseWriter, r *http.Request) (*models.DiscordBotCredentials, bool) {
	userID, ok := h.user(w, r)
	if !ok {
		return nil, false
	}
	botID, ok := h.pathID(w, r, "id")
	if !ok {
		return nil, false
	}
	creds, err := h.Store.GetDiscordBotCredentials(r.Context(), userID, botID)
	if err != nil {
		h.fail(w, err, "Failed to load the Discord bot")
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

// fail maps storage errors to responses: not-found (including another account's
// rows) is 404, conflicts are 409, anything else 500.
func (h *Handler) fail(w http.ResponseWriter, err error, msg string) {
	switch {
	case errors.Is(err, datastore.ErrDiscordBotNotFound), errors.Is(err, datastore.ErrDiscordBindingNotFound),
		errors.Is(err, datastore.ErrChatNotFound), errors.Is(err, datastore.ErrPersonalityNotFound):
		handlerutils.RespondWithError(w, h.Logger, http.StatusNotFound, handlerutils.CodeNotSet, "Not found", nil)
	case errors.Is(err, datastore.ErrDiscordBotExists):
		handlerutils.RespondWithError(w, h.Logger, http.StatusConflict, handlerutils.CodeNotSet,
			"That persona already has a bot, or that bot is already connected.", nil)
	case errors.Is(err, datastore.ErrDiscordBindingExists):
		handlerutils.RespondWithError(w, h.Logger, http.StatusConflict, handlerutils.CodeNotSet,
			"That channel is already connected for this bot.", nil)
	default:
		handlerutils.RespondWithError(w, h.Logger, http.StatusInternalServerError, handlerutils.CodeNotSet, msg, err)
	}
}

func (h *Handler) identifyFailed(w http.ResponseWriter, err error) {
	if errors.Is(err, discordrelay.ErrInvalidToken) {
		h.bad(w, "Discord rejected that token. Copy the bot token from the Bot page of your application (not the client secret).")
		return
	}
	h.discordFailed(w, err)
}

func (h *Handler) discordFailed(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, discordrelay.ErrInvalidToken):
		handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet, "Discord rejected the bot token. Paste a new one.", nil)
	case errors.Is(err, discordrelay.ErrNoAccess):
		handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet, "The bot cannot see that server or channel.", nil)
	default:
		handlerutils.RespondWithError(w, h.Logger, http.StatusBadGateway, handlerutils.CodeNotSet, "Discord could not be reached. Try again.", err)
	}
}

// cleanToken accepts a token pasted with a "Bot " prefix or surrounding spaces.
func cleanToken(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "Bot ")
	return strings.TrimSpace(t)
}

// cleanIDs trims, drops empties and non-numeric entries (Discord ids are
// snowflakes), and de-duplicates.
func cleanIDs(ids []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || strings.Trim(id, "0123456789") != "" {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// botStatus is the stored status of one of the user's bots, or "" when it cannot be read.
func (h *Handler) botStatus(ctx context.Context, userID, botID uuid.UUID) models.DiscordBotStatus {
	bots, err := h.Store.ListDiscordBots(ctx, userID)
	if err != nil {
		return ""
	}
	for _, b := range bots {
		if b.ID == botID {
			return b.Status
		}
	}
	return ""
}
