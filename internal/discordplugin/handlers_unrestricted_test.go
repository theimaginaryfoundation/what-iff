package discordplugin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// A Discord channel is a public surface: whoever the allow list lets tag the bot
// drives the bound thread. These pin that a thread that is not sandboxed is never
// bound, or repointed to, without an explicit acknowledgement.

func bindBody(botID, chatID uuid.UUID, extra map[string]any) map[string]any {
	body := map[string]any{"bot_id": botID, "guild_id": "g", "channel_id": "c", "chat_id": chatID}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func TestBindingAnUnsandboxedThreadIsRefusedWithoutAcknowledgement(t *testing.T) {
	botID, chatID := uuid.New(), uuid.New()
	store := &stubStore{
		bots:        map[uuid.UUID]*models.DiscordBotCredentials{botID: {ID: botID}},
		unsandboxed: map[uuid.UUID]bool{chatID: true},
	}
	rec := serve(t, newHandler(store, &stubDiscord{}), http.MethodPost, "/bindings", bindBody(botID, chatID, nil))
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "not sandboxed")
	assert.Contains(t, rec.Body.String(), "allow_unrestricted")
	assert.Empty(t, store.bindings, "nothing is stored")

	// An explicit false is no acknowledgement either.
	rec = serve(t, newHandler(store, &stubDiscord{}), http.MethodPost, "/bindings", bindBody(botID, chatID, map[string]any{"allow_unrestricted": false}))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, store.bindings)
}

func TestBindingAnUnsandboxedThreadWithAcknowledgementStoresIt(t *testing.T) {
	botID, chatID := uuid.New(), uuid.New()
	store := &stubStore{
		bots:        map[uuid.UUID]*models.DiscordBotCredentials{botID: {ID: botID}},
		unsandboxed: map[uuid.UUID]bool{chatID: true},
	}
	rec := serve(t, newHandler(store, &stubDiscord{}), http.MethodPost, "/bindings", bindBody(botID, chatID, map[string]any{"allow_unrestricted": true}))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Len(t, store.bindings, 1)
	assert.True(t, store.bindings[0].AllowUnrestricted)
	var got models.DiscordBinding
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotNil(t, got.ChatSandboxed)
	assert.False(t, *got.ChatSandboxed, "the response says the thread is not sandboxed")
}

func TestBindingASandboxedThreadNeedsNoAcknowledgementAndStoresNone(t *testing.T) {
	botID, chatID := uuid.New(), uuid.New()
	store := &stubStore{
		bots:        map[uuid.UUID]*models.DiscordBotCredentials{botID: {ID: botID}},
		unsandboxed: map[uuid.UUID]bool{},
	}
	// A stray acknowledgement is not recorded against a sandboxed thread: it would
	// otherwise silently cover the thread if the sandbox were switched off later.
	rec := serve(t, newHandler(store, &stubDiscord{}), http.MethodPost, "/bindings", bindBody(botID, chatID, map[string]any{"allow_unrestricted": true}))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.False(t, store.bindings[0].AllowUnrestricted)
}

func TestARelayThreadCreatedByTheServerIsNeverAcknowledged(t *testing.T) {
	botID := uuid.New()
	store := &stubStore{bots: map[uuid.UUID]*models.DiscordBotCredentials{botID: {ID: botID}}}
	rec := serve(t, newHandler(store, &stubDiscord{}), http.MethodPost, "/bindings", map[string]any{
		"bot_id": botID, "guild_id": "g", "channel_id": "c", "allow_unrestricted": true,
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.False(t, store.bindings[0].AllowUnrestricted)
}

func TestRepointingABindingToAnUnsandboxedThreadIsRefusedWithoutAcknowledgement(t *testing.T) {
	bindingID, target := uuid.New(), uuid.New()
	store := &stubStore{
		existing:    &models.DiscordBinding{ID: bindingID, ChatID: uuid.New()},
		unsandboxed: map[uuid.UUID]bool{target: true},
	}
	rec := serve(t, newHandler(store, &stubDiscord{}), http.MethodPatch, "/bindings/"+bindingID.String(), map[string]any{"chat_id": target})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "allow_unrestricted")
	assert.Empty(t, store.patched, "the binding is not repointed")

	rec = serve(t, newHandler(store, &stubDiscord{}), http.MethodPatch, "/bindings/"+bindingID.String(), map[string]any{"chat_id": target, "allow_unrestricted": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.patched, 1)
	assert.Equal(t, target, *store.patched[0].ChatID)
	assert.True(t, *store.patched[0].AllowUnrestricted)
}

func TestRepointingToASandboxedThreadResetsAnEarlierAcknowledgement(t *testing.T) {
	bindingID, target := uuid.New(), uuid.New()
	store := &stubStore{
		existing:    &models.DiscordBinding{ID: bindingID, ChatID: uuid.New(), AllowUnrestricted: true},
		unsandboxed: map[uuid.UUID]bool{},
	}
	rec := serve(t, newHandler(store, &stubDiscord{}), http.MethodPatch, "/bindings/"+bindingID.String(), map[string]any{"chat_id": target})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.patched, 1)
	require.NotNil(t, store.patched[0].AllowUnrestricted)
	assert.False(t, *store.patched[0].AllowUnrestricted)
}

func TestAcknowledgingTheCurrentThreadAloneWorksOnlyWhileItIsNotSandboxed(t *testing.T) {
	bindingID, chatID := uuid.New(), uuid.New()
	store := &stubStore{
		existing:    &models.DiscordBinding{ID: bindingID, ChatID: chatID},
		unsandboxed: map[uuid.UUID]bool{chatID: true},
	}
	path := "/bindings/" + bindingID.String()
	rec := serve(t, newHandler(store, &stubDiscord{}), http.MethodPatch, path, map[string]any{"allow_unrestricted": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, *store.patched[0].AllowUnrestricted)

	delete(store.unsandboxed, chatID)
	serve(t, newHandler(store, &stubDiscord{}), http.MethodPatch, path, map[string]any{"allow_unrestricted": true})
	assert.False(t, *store.patched[1].AllowUnrestricted, "not recorded against a sandboxed thread")

	serve(t, newHandler(store, &stubDiscord{}), http.MethodPatch, path, map[string]any{"allow_unrestricted": false})
	assert.False(t, *store.patched[2].AllowUnrestricted, "can be withdrawn")
}

func TestListedBindingsSayWhetherTheirThreadIsSandboxedNow(t *testing.T) {
	chatID := uuid.New()
	store := &stubStore{
		existing:    &models.DiscordBinding{ID: uuid.New(), ChatID: chatID},
		unsandboxed: map[uuid.UUID]bool{chatID: true},
	}
	rec := serve(t, newHandler(store, &stubDiscord{}), http.MethodGet, "/bindings", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var got []models.DiscordBinding
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 1)
	require.NotNil(t, got[0].ChatSandboxed)
	assert.False(t, *got[0].ChatSandboxed)

	delete(store.unsandboxed, chatID)
	rec = serve(t, newHandler(store, &stubDiscord{}), http.MethodGet, "/bindings", nil)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.True(t, *got[0].ChatSandboxed)
}
