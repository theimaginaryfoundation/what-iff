package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

func (f *connectionFixture) choose(t *testing.T, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole, modelID uuid.UUID) {
	t.Helper()
	require.NoError(t, f.a.ds.SetUtilityModel(context.Background(), userID, models.UtilityModelSetting{Scope: scope, Role: role, ModelID: &modelID}))
}

// switchOff switches role off at scope.
func (f *connectionFixture) switchOff(t *testing.T, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole) {
	t.Helper()
	require.NoError(t, f.a.ds.SetUtilityModel(context.Background(), userID, models.UtilityModelSetting{Scope: scope, Role: role}))
}

// target is the model role resolves to under ctx, which must resolve.
func (f *connectionFixture) target(t *testing.T, ctx context.Context, role provider.UtilityRole) utilityTarget {
	t.Helper()
	got, err := f.a.utilityTargetFor(ctx, role)
	require.NoError(t, err)
	return got
}

// A role runs on the account's choice, else the deployment's, else nothing; an account's
// choice never reaches another account.
func TestUtilityTargetFor_AccountThenDeploymentThenNotSet(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	f.serve(f.aliceModel, ConnectionEndpoint{Preset: "ollama", Flavor: ConnectionFlavorOpenAI, BaseURL: "http://local"})
	alice := apicontext.WithUserID(context.Background(), f.alice)
	bob := apicontext.WithUserID(context.Background(), f.bob)

	_, err := f.a.utilityTargetFor(alice, provider.UtilityRoleSimple)
	require.ErrorIs(t, err, ErrUtilityRoleNotSet, "nothing chosen: no model to run on")

	f.choose(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleSimple, f.catalog.ID)
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleSimple, f.aliceModel.ID)

	got := f.target(t, alice, provider.UtilityRoleSimple)
	require.Equal(t, "qwen3:0.6b", got.name)
	require.Equal(t, "ollama", got.provider)
	require.NotNil(t, got.connection, "a connection model is reached through its connection")

	got = f.target(t, bob, provider.UtilityRoleSimple)
	require.Equal(t, "gpt-5.1", got.name, "bob gets the deployment's choice, not alice's")
	require.Nil(t, got.connection)

	_, err = f.a.utilityTargetFor(alice, provider.UtilityRoleMemory)
	require.ErrorIs(t, err, ErrUtilityRoleNotSet, "a choice applies to its role only")
}

// Off is a choice like a model: an account's Off stands over the deployment's model, and an
// account's model over the deployment's Off.
func TestUtilityTargetFor_Off(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	alice := apicontext.WithUserID(context.Background(), f.alice)
	bob := apicontext.WithUserID(context.Background(), f.bob)
	f.choose(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleSimple, f.catalog.ID)
	f.switchOff(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleSimple)

	_, err := f.a.utilityTargetFor(alice, provider.UtilityRoleSimple)
	require.ErrorIs(t, err, ErrUtilityRoleOff)
	require.Equal(t, "gpt-5.1", f.target(t, bob, provider.UtilityRoleSimple).name, "bob still gets the deployment's")

	f.switchOff(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleSimple)
	_, err = f.a.utilityTargetFor(bob, provider.UtilityRoleSimple)
	require.ErrorIs(t, err, ErrUtilityRoleOff)
	f.choose(t, models.UtilityScopeAccount, f.bob, models.UtilityRoleSimple, f.catalog.ID)
	require.Equal(t, "gpt-5.1", f.target(t, bob, provider.UtilityRoleSimple).name)
}

// A choice that can't be used falls back to the next one, and past the last fails with why it
// couldn't be used; each fallback is counted.
func TestUtilityTargetFor_UnusableChoiceFallsBack(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	tm := telemetrytest.New(t)
	f.a.telemetry = &telemetry.Telemetry{Logger: zap.NewNop(), Metrics: tm.Metrics}
	ctx := context.Background()
	alice := apicontext.WithUserID(ctx, f.alice)
	f.choose(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleMemory, f.catalog.ID)
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleMemory, f.aliceModel.ID)
	// Alice turns her model off.
	_, err := f.ent.Model.UpdateOneID(f.aliceModel.ID).SetDeleted(true).Save(ctx)
	require.NoError(t, err)

	require.Equal(t, "gpt-5.1", f.target(t, alice, provider.UtilityRoleMemory).name, "the deployment's choice")
	require.EqualValues(t, 1, tm.CounterValue(t, telemetry.UtilityModelFallbacks.Name,
		telemetry.AttrUtilityRole.String("memory"), telemetry.AttrUtilityScope.String("account"), telemetry.AttrReason.String("unavailable")))

	// The admin deletes the catalog model: a soft delete, so its setting stays and is passed over.
	_, err = f.ent.Model.UpdateOneID(f.catalog.ID).SetDeleted(true).Save(ctx)
	require.NoError(t, err)
	_, err = f.a.utilityTargetFor(alice, provider.UtilityRoleMemory)
	require.ErrorIs(t, err, ErrModelDisabled, "why the last choice couldn't be used")
}

// A call on a role whose chosen model is a connection's goes to that connection.
func TestCallUtility_ChosenConnectionModel(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	srv, path, req := recordingServer(t, `{"id":"c","object":"chat.completion","created":1,"model":"qwen3:0.6b","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"named"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	f.serve(f.aliceModel, ConnectionEndpoint{Preset: "ollama", Flavor: ConnectionFlavorOpenAI, BaseURL: srv.URL + "/v1"})
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleSimple, f.aliceModel.ID)

	out, err := f.a.CallUtility(apicontext.WithUserID(context.Background(), f.alice), utilityTestRequest)
	require.NoError(t, err)
	require.Equal(t, "named", out)
	require.Contains(t, *path, "/chat/completions")
	require.Equal(t, "qwen3:0.6b", (*req)["model"])
}

// A checkpoint chains off the stored OpenAI response only while the memory role's model can
// continue it; a memory model chosen elsewhere makes the chat rebuild its context instead.
func TestCheckpointChainsResponses_FollowsMemoryModel(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	f.serve(f.aliceModel, ConnectionEndpoint{Preset: "ollama", Flavor: ConnectionFlavorOpenAI, BaseURL: "http://local"})
	openAIChat := &chatContext{model: "gpt-5.4", modelProvider: string(models.ModelProviderOpenAI)}
	alice := apicontext.WithUserID(context.Background(), f.alice)

	chains := func(chatCtx *chatContext) bool {
		return checkpointChainsResponses(chatCtx, f.target(t, alice, provider.UtilityRoleMemory))
	}
	f.choose(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleMemory, f.catalog.ID)
	require.True(t, chains(openAIChat), "an OpenAI memory model chains")
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleMemory, f.aliceModel.ID)
	require.False(t, chains(openAIChat))
	require.False(t, chains(&chatContext{model: "gemini-3.5", modelProvider: string(models.ModelProviderGoogle)}))

	require.Equal(t, "qwen3:0.6b", f.target(t, alice, provider.UtilityRoleMemory).name, "the rebuilt path runs on the chosen memory model")
}

// A checkpoint pins the memory model it decided to chain with, so a choice saved while it runs
// can't send a later step, still continuing the stored response, to a model that can't
// continue it.
func TestCheckpoint_PinnedMemoryModelSurvivesAChoiceMidRun(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	f.serve(f.aliceModel, ConnectionEndpoint{Preset: "ollama", Flavor: ConnectionFlavorOpenAI, BaseURL: "http://local"})
	srv, reqs := capturingResponsesServer(t, responseTextJSONBody("resp_2", "updated"))
	f.a.OpenAIProvider = newHTTPTestOpenAIProvider(srv.URL)
	alice := apicontext.WithUserID(context.Background(), f.alice)
	openAIChat := &chatContext{model: "gpt-5.4", modelProvider: string(models.ModelProviderOpenAI)}

	f.choose(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleMemory, f.catalog.ID)
	memory := f.target(t, alice, provider.UtilityRoleMemory)
	require.True(t, checkpointChainsResponses(openAIChat, memory))
	checkpoint := withUtilityTarget(alice, provider.UtilityRoleMemory, memory)

	// Alice chooses a local model for memory while the checkpoint runs.
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleMemory, f.aliceModel.ID)
	require.Equal(t, "qwen3:0.6b", f.target(t, alice, provider.UtilityRoleMemory).name, "outside the checkpoint, the new choice counts")
	require.Equal(t, memory, f.target(t, checkpoint, provider.UtilityRoleMemory), "inside it, the pinned model stays")

	_, err := f.a.callUtility(checkpoint, provider.UtilityRequest{
		Role: provider.UtilityRoleMemory, PreviousResponseID: "resp_1", Input: "update the scratchpad",
	})
	require.NoError(t, err, "the step continues the stored response on the model that chained it")
	got := reqs()
	require.Len(t, got, 1)
	require.Equal(t, "resp_1", got[0]["previous_response_id"])
	require.Equal(t, "gpt-5.1", got[0]["model"])
}

// A catalog model whose provider has lost its key since it was chosen can't be used: the role
// falls back past it, and with nothing after it, the call fails naming the missing key.
func TestUtilityTargetFor_ChoiceWithoutKeyFallsBack(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	alice := apicontext.WithUserID(context.Background(), f.alice)
	f.choose(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleSimple, f.catalog.ID)

	f.a.keyResolver = providerkeys.NewDeploymentResolver(map[models.ModelProvider]string{models.ModelProviderOpenAI: "key"})
	require.Equal(t, "gpt-5.1", f.target(t, alice, provider.UtilityRoleSimple).name, "with its key, the choice runs")

	f.a.keyResolver = providerkeys.NewDeploymentResolver(map[models.ModelProvider]string{})
	_, err := f.a.utilityTargetFor(alice, provider.UtilityRoleSimple)
	var missing *providerkeys.NotConfiguredError
	require.ErrorAs(t, err, &missing, "without it, the missing key")
	require.Equal(t, models.ModelProviderOpenAI, missing.Provider)
}

// Where the deployment supplies the keys, an account's stored choice doesn't count: the
// deployment pays for background work, so only its choice does.
func TestUtilityTargetFor_DeploymentKeysIgnoreAccountChoices(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	f.a.keyResolver = providerkeys.NewDeploymentResolver(map[models.ModelProvider]string{models.ModelProviderOpenAI: "k"})
	f.choose(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleSimple, f.catalog.ID)
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleSimple, f.aliceModel.ID)

	require.Equal(t, "gpt-5.1", f.target(t, apicontext.WithUserID(context.Background(), f.alice), provider.UtilityRoleSimple).name)
}

// A role switched off or not set doesn't run: the call fails before anything is sent.
func TestCallUtility_OffOrNotSetSendsNothing(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	srv, reqs := capturingResponsesServer(t, responseTextJSONBody("resp_1", "named"))
	f.a.OpenAIProvider = newHTTPTestOpenAIProvider(srv.URL)
	alice := apicontext.WithUserID(context.Background(), f.alice)

	_, err := f.a.CallUtility(alice, utilityTestRequest)
	require.ErrorIs(t, err, ErrUtilityRoleNotSet)
	f.switchOff(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleSimple)
	_, err = f.a.CallUtility(alice, utilityTestRequest)
	require.ErrorIs(t, err, ErrUtilityRoleOff)
	require.Empty(t, reqs(), "nothing reaches a provider")
}

// Merging runs on its own role: switched off, it groups only exact duplicates and sends
// nothing, while the rest of memory work keeps its model.
func TestInferMemoryMergeGroups_RunsOnTheMergeRole(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	srv, reqs := capturingResponsesServer(t, responseTextJSONBody("resp_1", `{\"groups\":[]}`))
	f.a.OpenAIProvider = newHTTPTestOpenAIProvider(srv.URL)
	alice := apicontext.WithUserID(context.Background(), f.alice)
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleMemory, f.catalog.ID)
	f.switchOff(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleMemoryMerge)
	candidates := []memoryMergeCandidate{{Content: "likes tea", IsNew: true}, {Content: "likes tea", IsNew: true}}

	require.Equal(t, fallbackExactMergeGroups(candidates), f.a.inferMemoryMergeGroups(alice, f.alice, "", candidates))
	require.Empty(t, reqs())
	require.Equal(t, "gpt-5.1", f.target(t, alice, provider.UtilityRoleMemory).name)
}

// A choice's effort and tier travel with its model, and the model row's reasoning support with
// them, so the call can limit the effort to what the model takes.
func TestUtilityTargetFor_CarriesTheChoicesEffortAndTier(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	alice := apicontext.WithUserID(context.Background(), f.alice)
	id := f.catalog.ID
	require.NoError(t, f.a.ds.SetUtilityModel(context.Background(), uuid.Nil, models.UtilityModelSetting{
		Scope: models.UtilityScopeDeployment, Role: models.UtilityRoleMemoryMerge, ModelID: &id,
		ReasoningEffort: models.ReasoningEffortLow, ServiceTier: models.UtilityServiceTierFlex}))

	got := f.target(t, alice, provider.UtilityRoleMemoryMerge)
	require.Equal(t, models.ReasoningEffortLow, got.reasoningEffort)
	require.Equal(t, models.UtilityServiceTierFlex, got.serviceTier)
	require.Equal(t, models.ReasoningEffortLow, got.chatContext().reasoningEffort, "gpt-5.1 takes an optional effort")
}
