package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type fakeDiscordToolStore struct {
	bindings []models.DiscordBinding
	pending  []uuid.UUID
}

func (f *fakeDiscordToolStore) ListDiscordBindingsForPersonality(context.Context, uuid.UUID, uuid.UUID) ([]models.DiscordBinding, error) {
	return f.bindings, nil
}

func (f *fakeDiscordToolStore) SetDiscordPendingPost(_ context.Context, _, bindingID, _ uuid.UUID, src models.DiscordPendingPostSource) error {
	if src != models.DiscordPostFromTool {
		panic("wrong source")
	}
	f.pending = append(f.pending, bindingID)
	return nil
}

func withDiscordToolStore(t *testing.T, s discordToolStore) {
	t.Helper()
	orig := discordToolStoreFor
	discordToolStoreFor = func(*Agent) discordToolStore { return s }
	t.Cleanup(func() { discordToolStoreFor = orig })
}

func binding(guild, channel string) models.DiscordBinding {
	return models.DiscordBinding{ID: uuid.New(), ChatID: uuid.New(), GuildName: guild, ChannelName: channel, ChannelID: "id-" + channel, Status: models.DiscordBindingActive}
}

func testChat() *models.Chat {
	return &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}
}

func TestDiscordToolIsInTheCatalog(t *testing.T) {
	names := map[string]bool{}
	for _, d := range tools.AdditionalFunctionToolCatalog() {
		names[d.Spec.Name] = true
	}
	assert.True(t, names[discordPostToolName])

	handlers := extraToolHandlersForChat(&Agent{}, testChat())
	assert.Contains(t, handlers, discordPostToolName)
}

func TestDiscordToolIsDisabledWithoutChannels(t *testing.T) {
	store := &fakeDiscordToolStore{}
	withDiscordToolStore(t, store)
	chat := testChat()

	assert.True(t, additionalDisabledToolsForChat(&Agent{}, chat)[discordPostToolName])

	store.bindings = []models.DiscordBinding{binding("Home", "general")}
	assert.False(t, additionalDisabledToolsForChat(&Agent{}, chat)[discordPostToolName])

	store.bindings[0].Status = models.DiscordBindingBroken
	assert.True(t, additionalDisabledToolsForChat(&Agent{}, chat)[discordPostToolName], "broken bindings are not offered")
}

func TestDiscordPostMarksTheChosenChannel(t *testing.T) {
	gen, ops := binding("Home", "general"), binding("Home", "ops")
	store := &fakeDiscordToolStore{bindings: []models.DiscordBinding{gen, ops}}
	chat := testChat()

	for _, arg := range []string{`{"channel":"#ops"}`, `{"channel":"ops"}`, `{"channel":"Home/#ops"}`, `{"channel":" #OPS "}`} {
		store.pending = nil
		out, err := discordPost(context.Background(), store, chat, []byte(arg))
		require.NoError(t, err, arg)
		assert.Equal(t, []uuid.UUID{ops.ID}, store.pending, arg)
		assert.Contains(t, out, "#ops")
	}
}

func TestDiscordPostRejectsUnknownAndAmbiguousChannels(t *testing.T) {
	a, b := binding("Home", "general"), binding("Work", "general")
	store := &fakeDiscordToolStore{bindings: []models.DiscordBinding{a, b}}
	chat := testChat()

	_, err := discordPost(context.Background(), store, chat, []byte(`{"channel":"#random"}`))
	assert.ErrorContains(t, err, "Home/#general, Work/#general")

	_, err = discordPost(context.Background(), store, chat, []byte(`{"channel":"#general"}`))
	assert.ErrorContains(t, err, "more than one")

	_, err = discordPost(context.Background(), store, chat, []byte(`{"channel":"Work/#general"}`))
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{b.ID}, store.pending)

	_, err = discordPost(context.Background(), store, chat, []byte(`{}`))
	assert.Error(t, err)
}

func TestDiscordDeveloperContextListsChannelsAndExplainsTheRelayThread(t *testing.T) {
	chat := testChat()
	gen := binding("Home", "general")
	gen.ChatID = chat.ID

	ctx := discordDeveloperContext([]models.DiscordBinding{gen, binding("Home", "ops")}, chat, true)
	assert.Contains(t, ctx, "#general (in Home), #ops (in Home)")
	assert.Contains(t, ctx, "This thread is the relay for #general")

	assert.Empty(t, discordDeveloperContext(nil, chat, true))
	other := discordDeveloperContext([]models.DiscordBinding{binding("Home", "ops")}, chat, true)
	assert.NotContains(t, other, "This thread is the relay")
}

// --- sandboxed threads reach only their own bound channel ---

func sandboxedChat() *models.Chat {
	c := testChat()
	c.Sandboxed = true
	return c
}

func TestASandboxedThreadCanPostOnlyToItsOwnChannel(t *testing.T) {
	chat := sandboxedChat()
	own, other := binding("Home", "general"), binding("Work", "secret-ops")
	own.ChatID = chat.ID
	store := &fakeDiscordToolStore{bindings: []models.DiscordBinding{own, other}}

	out, err := discordPost(context.Background(), store, chat, []byte(`{"channel":"#general"}`))
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{own.ID}, store.pending)
	assert.Contains(t, out, "#general")

	for _, arg := range []string{`{"channel":"#secret-ops"}`, `{"channel":"Work/#secret-ops"}`, `{"channel":"id-secret-ops"}`, `{"channel":"#nope"}`} {
		store.pending = nil
		_, err := discordPost(context.Background(), store, chat, []byte(arg))
		require.Error(t, err, arg)
		assert.Contains(t, err.Error(), "restricted thread", arg)
		assert.Contains(t, err.Error(), "own Discord channel", arg)
		assert.NotContains(t, err.Error(), "secret-ops)", "the other channel is not listed back")
		assert.Empty(t, store.pending, arg)
	}
}

func TestASandboxedThreadDoesNotLearnOtherChannelNames(t *testing.T) {
	chat := sandboxedChat()
	own, other := binding("Home", "general"), binding("Work", "secret-ops")
	own.ChatID = chat.ID
	store := &fakeDiscordToolStore{bindings: []models.DiscordBinding{own, other}}
	withDiscordToolStore(t, store)

	got := discordChannelsFor(context.Background(), store, chat)
	require.Len(t, got, 1)
	assert.Equal(t, own.ID, got[0].ID)

	dev := discordDeveloperContext(got, chat, true)
	assert.NotContains(t, dev, "secret-ops")
	assert.NotContains(t, dev, "Work")
}

func TestASandboxedThreadWithNoChannelOfItsOwnHasNoPostTool(t *testing.T) {
	chat := sandboxedChat()
	store := &fakeDiscordToolStore{bindings: []models.DiscordBinding{binding("Home", "general")}}
	withDiscordToolStore(t, store)

	assert.True(t, additionalDisabledToolsForChat(&Agent{}, chat)[discordPostToolName])
	_, err := discordPost(context.Background(), store, chat, []byte(`{"channel":"#general"}`))
	assert.Error(t, err)
	assert.Empty(t, store.pending)
}

// An unrestricted chat (the owner's own) keeps the persona-wide reach: that is the
// feature. Pinned so a change to it is deliberate.
func TestAnUnrestrictedChatStillReachesEveryChannelOfThePersona(t *testing.T) {
	chat := testChat()
	a, b := binding("Home", "general"), binding("Work", "ops")
	store := &fakeDiscordToolStore{bindings: []models.DiscordBinding{a, b}}
	_, err := discordPost(context.Background(), store, chat, []byte(`{"channel":"Work/#ops"}`))
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{b.ID}, store.pending)
}

func TestDeveloperContextOmitsChannelsWhenTheToolIsOff(t *testing.T) {
	chat := testChat()
	gen := binding("Home", "general")
	gen.ChatID = chat.ID
	ctx := discordDeveloperContext([]models.DiscordBinding{gen}, chat, false)
	assert.NotContains(t, ctx, "post_to_discord")
	assert.Contains(t, ctx, "This thread is the relay for #general")
	assert.False(t, discordToolAvailable(&models.Chat{ToolsEnabled: true, DisabledTools: []string{discordPostToolName}}))
	assert.False(t, discordToolAvailable(&models.Chat{}))
	assert.True(t, discordToolAvailable(&models.Chat{ToolsEnabled: true}))
}

// A relay thread is driven by people outside the account even when the owner widened it, so it
// reaches only its own channel, like a restricted chat.
func TestABoundThreadReachesOnlyItsOwnChannelWhateverItsLimit(t *testing.T) {
	chat := testChat() // unrestricted
	own, other := binding("Home", "general"), binding("Work", "secret-ops")
	own.ChatID = chat.ID
	store := &fakeDiscordToolStore{bindings: []models.DiscordBinding{own, other}}
	got := discordChannelsFor(context.Background(), store, chat)
	require.Len(t, got, 1)
	assert.Equal(t, own.ID, got[0].ID)
}
