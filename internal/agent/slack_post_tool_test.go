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

type fakeSlackToolStore struct {
	bindings []models.SlackBinding
	pending  []uuid.UUID
}

func (f *fakeSlackToolStore) ListSlackBindingsForPersonality(context.Context, uuid.UUID, uuid.UUID) ([]models.SlackBinding, error) {
	return f.bindings, nil
}

func (f *fakeSlackToolStore) SetSlackPendingPost(_ context.Context, _, bindingID, _ uuid.UUID, src models.SlackPendingPostSource) error {
	if src != models.SlackPostFromTool {
		panic("wrong source")
	}
	f.pending = append(f.pending, bindingID)
	return nil
}

func withSlackToolStore(t *testing.T, s slackToolStore) {
	t.Helper()
	orig := slackToolStoreFor
	slackToolStoreFor = func(*Agent) slackToolStore { return s }
	t.Cleanup(func() { slackToolStoreFor = orig })
	allowSlack(t, true)
}

// allowSlack decides whether every owner may use the (experimental) relay for the test.
func allowSlack(t *testing.T, ok bool) {
	t.Helper()
	orig := slackMayUse
	slackMayUse = func(context.Context, uuid.UUID) bool { return ok }
	t.Cleanup(func() { slackMayUse = orig })
}

func slackBinding(team, channel string) models.SlackBinding {
	return models.SlackBinding{ID: uuid.New(), ChatID: uuid.New(), TeamName: team, ChannelName: channel, ChannelID: "id-" + channel, Status: models.SlackBindingActive}
}

func TestSlackToolIsInTheCatalogAndNeedsChannels(t *testing.T) {
	names := map[string]bool{}
	for _, d := range tools.AdditionalFunctionToolCatalog() {
		names[d.Spec.Name] = true
	}
	assert.True(t, names[slackPostToolName])
	assert.True(t, names[discordPostToolName], "both relays' tools are registered")
	assert.Contains(t, extraToolHandlersForChat(&Agent{}, testChat()), slackPostToolName)

	store := &fakeSlackToolStore{}
	withSlackToolStore(t, store)
	chat := testChat()
	assert.True(t, additionalDisabledToolsForChat(&Agent{}, chat)[slackPostToolName])
	store.bindings = []models.SlackBinding{slackBinding("Acme", "general")}
	assert.False(t, additionalDisabledToolsForChat(&Agent{}, chat)[slackPostToolName])
	store.bindings[0].Status = models.SlackBindingBroken
	assert.True(t, additionalDisabledToolsForChat(&Agent{}, chat)[slackPostToolName], "broken bindings are not offered")
}

func TestSlackToolIsOffWhereTheRelayIsOff(t *testing.T) {
	store := &fakeSlackToolStore{bindings: []models.SlackBinding{slackBinding("Acme", "general")}}
	withSlackToolStore(t, store)
	chat := testChat()
	assert.False(t, additionalDisabledToolsForChat(&Agent{}, chat)[slackPostToolName])

	allowSlack(t, false)
	assert.True(t, additionalDisabledToolsForChat(&Agent{}, chat)[slackPostToolName], "an owner without the experimental feature gets no Slack tool")
}

func TestSlackPostMarksTheChosenChannel(t *testing.T) {
	allowSlack(t, true)
	gen, ops := slackBinding("Acme", "general"), slackBinding("Acme", "ops")
	store := &fakeSlackToolStore{bindings: []models.SlackBinding{gen, ops}}
	chat := testChat()
	for _, arg := range []string{`{"channel":"#ops"}`, `{"channel":"ops"}`, `{"channel":"Acme/#ops"}`, `{"channel":"id-ops"}`} {
		store.pending = nil
		out, err := slackPost(context.Background(), store, chat, []byte(arg))
		require.NoError(t, err, arg)
		assert.Equal(t, []uuid.UUID{ops.ID}, store.pending, arg)
		assert.Contains(t, out, "#ops")
	}
	_, err := slackPost(context.Background(), store, chat, []byte(`{"channel":"#nope"}`))
	assert.ErrorContains(t, err, "use one of: #general, #ops")
	_, err = slackPost(context.Background(), store, chat, []byte(`{}`))
	assert.Error(t, err)
}

func TestSlackSameNamedChannelsAreQualifiedByWorkspace(t *testing.T) {
	allowSlack(t, true)
	a, b := slackBinding("Acme", "general"), slackBinding("Globex", "general")
	store := &fakeSlackToolStore{bindings: []models.SlackBinding{a, b}}
	chat := testChat()
	_, err := slackPost(context.Background(), store, chat, []byte(`{"channel":"#general"}`))
	assert.ErrorContains(t, err, "Acme/#general, Globex/#general")
	_, err = slackPost(context.Background(), store, chat, []byte(`{"channel":"Globex/#general"}`))
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{b.ID}, store.pending)
}

func TestSlackSandboxedOrRelayThreadsReachOnlyTheirOwnChannel(t *testing.T) {
	allowSlack(t, true)
	chat := testChat()
	own, other := slackBinding("Acme", "general"), slackBinding("Acme", "secret-plans")
	own.ChatID = chat.ID
	store := &fakeSlackToolStore{bindings: []models.SlackBinding{own, other}}
	got := slackChannelsFor(context.Background(), store, chat)
	require.Len(t, got, 1, "a relay thread reaches only its own channel")
	assert.Equal(t, own.ID, got[0].ID)

	ctx := slackDeveloperContext(got, chat, true)
	assert.NotContains(t, ctx, "secret-plans", "the owner's other channels are never named")
	assert.Contains(t, ctx, `"name (Slack, #channel): text"`)

	sandboxed := testChat()
	sandboxed.ContextScope = models.ContextScopeSandbox
	assert.Empty(t, slackChannelsFor(context.Background(), store, sandboxed), "a sandboxed chat that is not a relay thread reaches none")

	_, err := slackPost(context.Background(), store, chat, []byte(`{"channel":"#secret-plans"}`))
	assert.Error(t, err)
	assert.Empty(t, store.pending)
}
