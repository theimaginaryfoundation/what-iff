package discordplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/discordrelay"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// stubStore implements Store; methods a test does not set panic via the nil embed.
type stubStore struct {
	Store
	bots        map[uuid.UUID]*models.DiscordBotCredentials
	created     []models.DiscordBotCreate
	chats       []models.Chat
	bindings    []models.DiscordBindingCreate
	patches     []models.DiscordBotPatch
	pendingSet  []uuid.UUID
	createErr   error
	personaName string
	// chatLimits is each chat's Memory access; a chat not listed is a restricted
	// (public-only) thread, so tests opt IN to unrestricted ones.
	chatLimits map[uuid.UUID]models.MemorySensitivity
	// existing is the stored binding GetDiscordBinding / UpdateDiscordBinding see.
	existing *models.DiscordBinding
	patched  []models.DiscordBindingPatch
	// bindingErr fails CreateDiscordBinding; deletedChats records DeleteChat.
	bindingErr   error
	deletedChats []uuid.UUID
}

func (s *stubStore) CreateDiscordBot(_ context.Context, _ uuid.UUID, in models.DiscordBotCreate) (*models.DiscordBot, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	s.created = append(s.created, in)
	return &models.DiscordBot{ID: uuid.New(), PersonalityID: in.PersonalityID, ApplicationID: in.ApplicationID, BotUserID: in.BotUserID, BotUsername: in.BotUsername}, nil
}

func (s *stubStore) GetDiscordBotCredentials(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.DiscordBotCredentials, error) {
	c, ok := s.bots[id]
	if !ok {
		return nil, datastore.ErrDiscordBotNotFound
	}
	return c, nil
}

func (s *stubStore) UpdateDiscordBot(_ context.Context, _ uuid.UUID, id uuid.UUID, p models.DiscordBotPatch) (*models.DiscordBot, error) {
	s.patches = append(s.patches, p)
	return &models.DiscordBot{ID: id, ApplicationID: "app"}, nil
}

func (s *stubStore) CreateChat(_ context.Context, _ uuid.UUID, c models.Chat) (*models.Chat, error) {
	s.chats = append(s.chats, c)
	c.ID = uuid.New()
	return &c, nil
}

func (s *stubStore) DeleteChat(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	s.deletedChats = append(s.deletedChats, id)
	return nil
}

func (s *stubStore) CreateDiscordBinding(_ context.Context, _ uuid.UUID, botID uuid.UUID, in models.DiscordBindingCreate) (*models.DiscordBinding, error) {
	if s.bindingErr != nil {
		return nil, s.bindingErr
	}
	s.bindings = append(s.bindings, in)
	return &models.DiscordBinding{ID: uuid.New(), BotID: botID, ChatID: in.ChatID, ChannelID: in.ChannelID}, nil
}

func (s *stubStore) GetChat(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.Chat, error) {
	limit, ok := s.chatLimits[id]
	if !ok {
		limit = models.MemorySensitivityPublic
	}
	return &models.Chat{ID: id, MemorySensitivityLimit: limit}, nil
}

func (s *stubStore) GetDiscordBinding(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.DiscordBinding, error) {
	if s.existing == nil || s.existing.ID != id {
		return nil, datastore.ErrDiscordBindingNotFound
	}
	c := *s.existing
	return &c, nil
}

func (s *stubStore) UpdateDiscordBinding(_ context.Context, _ uuid.UUID, id uuid.UUID, p models.DiscordBindingPatch) (*models.DiscordBinding, error) {
	s.patched = append(s.patched, p)
	b := models.DiscordBinding{ID: id}
	if s.existing != nil {
		b = *s.existing
	}
	if p.ChatID != nil {
		b.ChatID = *p.ChatID
	}
	if p.AllowUnrestricted != nil {
		b.AllowUnrestricted = *p.AllowUnrestricted
	}
	return &b, nil
}

func (s *stubStore) ListDiscordBindings(context.Context, uuid.UUID) ([]models.DiscordBinding, error) {
	if s.existing == nil {
		return nil, nil
	}
	return []models.DiscordBinding{*s.existing}, nil
}

func (s *stubStore) SetDiscordPendingPost(_ context.Context, _ uuid.UUID, bindingID, _ uuid.UUID, _ models.DiscordPendingPostSource) error {
	s.pendingSet = append(s.pendingSet, bindingID)
	return nil
}

func (s *stubStore) GetPersonality(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.Personality, error) {
	return &models.Personality{ID: id, Name: s.personaName}, nil
}

type stubDiscord struct {
	discordrelay.Discord
	identity    discordrelay.BotIdentity
	identifyErr error
	tokens      []string
	profile     []string
}

func (d *stubDiscord) Identify(_ context.Context, token string) (discordrelay.BotIdentity, error) {
	d.tokens = append(d.tokens, token)
	return d.identity, d.identifyErr
}

func (d *stubDiscord) SetProfile(_ context.Context, _ string, username, avatar string) error {
	d.profile = append(d.profile, username, avatar)
	return nil
}

func serve(t *testing.T, h *Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := mux.NewRouter()
	h.Register(r)
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req = req.WithContext(middleware.ContextWithUser(req.Context(), uuid.New(), ""))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func newHandler(store *stubStore, d *stubDiscord) *Handler {
	if store.bots == nil {
		store.bots = map[uuid.UUID]*models.DiscordBotCredentials{}
	}
	return &Handler{Store: store, Discord: d, Logger: zap.NewNop()}
}

func TestCreateBotValidatesTheTokenAndStoresWhatDiscordSays(t *testing.T) {
	store := &stubStore{}
	d := &stubDiscord{identity: discordrelay.BotIdentity{ApplicationID: "app1", BotUserID: "111", Username: "Vix", MessageContent: true}}
	persona := uuid.New()

	rec := serve(t, newHandler(store, d), http.MethodPost, "/bots", map[string]any{"personality_id": persona, "token": "  Bot abc.def  "})

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"abc.def"}, d.tokens, "the prefix and spaces are stripped")
	require.Len(t, store.created, 1)
	assert.Equal(t, models.DiscordBotCreate{PersonalityID: persona, ApplicationID: "app1", BotUserID: "111", BotUsername: "Vix", Token: "abc.def", MessageContent: true}, store.created[0])
	var resp BotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp.InviteURL, "client_id=app1")
	assert.NotContains(t, rec.Body.String(), "abc.def", "the token is never returned")
}

func TestCreateBotRejectsABadTokenAndADuplicate(t *testing.T) {
	d := &stubDiscord{identifyErr: discordrelay.ErrInvalidToken}
	rec := serve(t, newHandler(&stubStore{}, d), http.MethodPost, "/bots", map[string]any{"personality_id": uuid.New(), "token": "x"})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "Discord rejected that token")

	store := &stubStore{createErr: datastore.ErrDiscordBotExists}
	rec = serve(t, newHandler(store, &stubDiscord{}), http.MethodPost, "/bots", map[string]any{"personality_id": uuid.New(), "token": "x"})
	assert.Equal(t, http.StatusConflict, rec.Code)

	rec = serve(t, newHandler(&stubStore{}, &stubDiscord{}), http.MethodPost, "/bots", map[string]any{"token": "x"})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestReplacingATokenMustKeepTheSameBot(t *testing.T) {
	botID := uuid.New()
	store := &stubStore{bots: map[uuid.UUID]*models.DiscordBotCredentials{botID: {ID: botID, BotUserID: "111", Token: "old"}}}

	d := &stubDiscord{identity: discordrelay.BotIdentity{BotUserID: "999"}}
	rec := serve(t, newHandler(store, d), http.MethodPatch, "/bots/"+botID.String(), map[string]any{"token": "new"})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, store.patches)

	d.identity = discordrelay.BotIdentity{BotUserID: "111", Username: "Vix"}
	rec = serve(t, newHandler(store, d), http.MethodPatch, "/bots/"+botID.String(), map[string]any{"token": "new"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.patches, 1)
	assert.Equal(t, "new", *store.patches[0].Token)
	assert.Equal(t, models.DiscordBotActive, *store.patches[0].Status, "a working token reactivates the bot")
}

func TestAnotherAccountsBotIsNotFound(t *testing.T) {
	rec := serve(t, newHandler(&stubStore{}, &stubDiscord{}), http.MethodGet, "/bots/"+uuid.NewString()+"/guilds", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateBindingWithoutAChatCreatesARelayThreadForThePersona(t *testing.T) {
	botID, persona := uuid.New(), uuid.New()
	store := &stubStore{bots: map[uuid.UUID]*models.DiscordBotCredentials{botID: {ID: botID, PersonalityID: persona}}}

	rec := serve(t, newHandler(store, &stubDiscord{}), http.MethodPost, "/bindings", map[string]any{
		"bot_id": botID, "guild_id": "g", "guild_name": "Home", "channel_id": "c", "channel_name": "#general",
		"allow_user_ids": []string{" 123 ", "123", "not-an-id", ""},
	})

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Len(t, store.chats, 1)
	assert.Equal(t, "Discord · #general", store.chats[0].Name)
	assert.Equal(t, persona, store.chats[0].PersonalityID)
	assert.Equal(t, models.MemorySensitivityPublic, store.chats[0].MemorySensitivityLimit,
		"a new relay thread is a public surface: public memories only, until the user widens it")
	for _, tool := range []string{"create_agent_job", "run_subagent", "update_scratchpad", "web_search", "fetch_page", "generate_image"} {
		assert.Contains(t, store.chats[0].DisabledTools, tool, "a new relay thread starts with %s off", tool)
	}
	require.Len(t, store.bindings, 1)
	assert.Equal(t, "general", store.bindings[0].ChannelName)
	assert.True(t, store.bindings[0].InboundEnabled, "inbound defaults on")
	assert.Equal(t, []string{"123"}, store.bindings[0].AllowUserIDs)
}

func TestCreateBindingOnAnExistingChatCreatesNoThread(t *testing.T) {
	botID, chatID := uuid.New(), uuid.New()
	store := &stubStore{bots: map[uuid.UUID]*models.DiscordBotCredentials{botID: {ID: botID}}}

	rec := serve(t, newHandler(store, &stubDiscord{}), http.MethodPost, "/bindings", map[string]any{
		"bot_id": botID, "guild_id": "g", "channel_id": "c", "chat_id": chatID, "inbound_enabled": false,
	})

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Empty(t, store.chats, "binding an existing thread creates none and leaves its settings alone")
	assert.Equal(t, chatID, store.bindings[0].ChatID)
	assert.False(t, store.bindings[0].InboundEnabled)
}

func TestSetPendingArmsTheNextReply(t *testing.T) {
	store := &stubStore{}
	bindingID := uuid.New()
	rec := serve(t, newHandler(store, &stubDiscord{}), http.MethodPut, "/chats/"+uuid.NewString()+"/pending/"+bindingID.String(), nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, []uuid.UUID{bindingID}, store.pendingSet)
}

func TestSyncProfileUsesThePersonaName(t *testing.T) {
	botID := uuid.New()
	store := &stubStore{bots: map[uuid.UUID]*models.DiscordBotCredentials{botID: {ID: botID, Token: "t"}}, personaName: "Vix the Foxfire"}
	d := &stubDiscord{}

	rec := serve(t, newHandler(store, d), http.MethodPost, "/bots/"+botID.String()+"/sync-profile", nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"Vix the Foxfire", ""}, d.profile, "no file store: name only")
}

func TestDiscordUsername(t *testing.T) {
	assert.Equal(t, "Vix", discordUsername("  Vix "))
	assert.Equal(t, "Persona", discordUsername("V"))
	assert.Len(t, []rune(discordUsername(string(make([]rune, 40)))), 32)
}

func TestAFailedBindingRemovesTheRelayThreadItCreated(t *testing.T) {
	botID := uuid.New()
	store := &stubStore{bots: map[uuid.UUID]*models.DiscordBotCredentials{botID: {ID: botID}}, bindingErr: datastore.ErrDiscordBindingExists}
	rec := serve(t, newHandler(store, &stubDiscord{}), http.MethodPost, "/bindings", map[string]any{"bot_id": botID, "guild_id": "g", "channel_id": "c"})
	assert.Equal(t, http.StatusConflict, rec.Code)
	require.Len(t, store.chats, 1)
	assert.Len(t, store.deletedChats, 1, "the thread made for the binding is not left behind")

	// An existing thread is never deleted.
	chatID := uuid.New()
	store = &stubStore{bots: map[uuid.UUID]*models.DiscordBotCredentials{botID: {ID: botID}}, bindingErr: datastore.ErrDiscordBindingExists}
	serve(t, newHandler(store, &stubDiscord{}), http.MethodPost, "/bindings", map[string]any{"bot_id": botID, "guild_id": "g", "channel_id": "c", "chat_id": chatID})
	assert.Empty(t, store.deletedChats)
}

func TestCredentialsNeverSerializeTheToken(t *testing.T) {
	b, err := json.Marshal(models.DiscordBotCredentials{Token: "secret-token"})
	require.NoError(t, err)
	assert.NotContains(t, string(b), "secret-token")
}
