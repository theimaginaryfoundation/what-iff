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

type fakeBuzzToolStore struct {
	bindings []models.BuzzBinding
	pending  []uuid.UUID
}

func (f *fakeBuzzToolStore) ListBuzzBindingsForPersonality(context.Context, uuid.UUID, uuid.UUID) ([]models.BuzzBinding, error) {
	return f.bindings, nil
}

func (f *fakeBuzzToolStore) SetBuzzPendingPost(_ context.Context, _, bindingID, _ uuid.UUID, src models.BuzzPendingPostSource) error {
	if src != models.BuzzPostFromTool {
		panic("wrong source")
	}
	f.pending = append(f.pending, bindingID)
	return nil
}

func withBuzzToolStore(t *testing.T, s buzzToolStore) {
	t.Helper()
	orig := buzzToolStoreFor
	buzzToolStoreFor = func(*Agent) buzzToolStore { return s }
	t.Cleanup(func() { buzzToolStoreFor = orig })
}

func buzzBinding(community, channel string) models.BuzzBinding {
	return models.BuzzBinding{ID: uuid.New(), ChatID: uuid.New(), CommunityName: community, ChannelName: channel, ChannelID: "id-" + channel, Status: models.BuzzBindingActive}
}

func TestBuzzToolIsInTheCatalogAndNeedsChannels(t *testing.T) {
	names := map[string]bool{}
	for _, d := range tools.AdditionalFunctionToolCatalog() {
		names[d.Spec.Name] = true
	}
	assert.True(t, names[buzzPostToolName])
	assert.True(t, names[discordPostToolName], "both relays' tools are registered")
	assert.Contains(t, extraToolHandlersForChat(&Agent{}, testChat()), buzzPostToolName)

	store := &fakeBuzzToolStore{}
	withBuzzToolStore(t, store)
	chat := testChat()
	assert.True(t, additionalDisabledToolsForChat(&Agent{}, chat)[buzzPostToolName])
	store.bindings = []models.BuzzBinding{buzzBinding("Acme", "general")}
	assert.False(t, additionalDisabledToolsForChat(&Agent{}, chat)[buzzPostToolName])
	store.bindings[0].Status = models.BuzzBindingBroken
	assert.True(t, additionalDisabledToolsForChat(&Agent{}, chat)[buzzPostToolName], "broken bindings are not offered")
}

func TestBuzzPostMarksTheChosenChannel(t *testing.T) {
	gen, ops := buzzBinding("Acme", "general"), buzzBinding("Acme", "ops")
	store := &fakeBuzzToolStore{bindings: []models.BuzzBinding{gen, ops}}
	chat := testChat()
	for _, arg := range []string{`{"channel":"#ops"}`, `{"channel":"ops"}`, `{"channel":"Acme/#ops"}`, `{"channel":"id-ops"}`} {
		store.pending = nil
		out, err := buzzPost(context.Background(), store, chat, []byte(arg))
		require.NoError(t, err, arg)
		assert.Equal(t, []uuid.UUID{ops.ID}, store.pending, arg)
		assert.Contains(t, out, "#ops")
	}
	_, err := buzzPost(context.Background(), store, chat, []byte(`{"channel":"#nope"}`))
	assert.ErrorContains(t, err, "use one of: #general, #ops")
	_, err = buzzPost(context.Background(), store, chat, []byte(`{}`))
	assert.Error(t, err)
}

func TestBuzzSameNamedChannelsAreQualifiedByCommunity(t *testing.T) {
	a, b := buzzBinding("Acme", "general"), buzzBinding("Globex", "general")
	store := &fakeBuzzToolStore{bindings: []models.BuzzBinding{a, b}}
	chat := testChat()
	_, err := buzzPost(context.Background(), store, chat, []byte(`{"channel":"#general"}`))
	assert.ErrorContains(t, err, "Acme/#general, Globex/#general")
	_, err = buzzPost(context.Background(), store, chat, []byte(`{"channel":"Globex/#general"}`))
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{b.ID}, store.pending)
}

func TestBuzzSandboxedOrRelayThreadsReachOnlyTheirOwnChannel(t *testing.T) {
	chat := testChat()
	own, other := buzzBinding("Acme", "general"), buzzBinding("Acme", "secret-plans")
	own.ChatID = chat.ID
	store := &fakeBuzzToolStore{bindings: []models.BuzzBinding{own, other}}
	got := buzzChannelsFor(context.Background(), store, chat)
	require.Len(t, got, 1, "a relay thread reaches only its own channel")
	assert.Equal(t, own.ID, got[0].ID)

	ctx := buzzDeveloperContext(got, chat, true)
	assert.NotContains(t, ctx, "secret-plans", "the owner's other channels are never named")
	assert.Contains(t, ctx, `"name (Buzz, #channel): text"`)

	sandboxed := testChat()
	sandboxed.ContextScope = models.ContextScopeSandbox
	assert.Empty(t, buzzChannelsFor(context.Background(), store, sandboxed), "a sandboxed chat that is not a relay thread reaches none")

	_, err := buzzPost(context.Background(), store, chat, []byte(`{"channel":"#secret-plans"}`))
	assert.Error(t, err)
	assert.Empty(t, store.pending)
}
