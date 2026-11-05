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
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

func (f *connectionFixture) choose(t *testing.T, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole, modelID uuid.UUID) {
	t.Helper()
	require.NoError(t, f.a.ds.SetUtilityModel(context.Background(), scope, userID, role, modelID))
}

// A role runs on the account's choice, else the deployment's, else its built-in model; an
// account's choice never reaches another account.
func TestUtilityTargetFor_AccountThenDeploymentThenBuiltIn(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	f.serve(f.aliceModel, ConnectionEndpoint{Preset: "ollama", Flavor: ConnectionFlavorOpenAI, BaseURL: "http://local"})
	alice := apicontext.WithUserID(context.Background(), f.alice)
	bob := apicontext.WithUserID(context.Background(), f.bob)

	got := f.a.utilityTargetFor(alice, provider.UtilityRoleQuick)
	require.Equal(t, utilityTarget{name: models.UtilityModelName}, got, "nothing chosen: the built-in model")

	f.choose(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleQuick, f.catalog.ID)
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleQuick, f.aliceModel.ID)

	got = f.a.utilityTargetFor(alice, provider.UtilityRoleQuick)
	require.Equal(t, "qwen3:0.6b", got.name)
	require.Equal(t, "ollama", got.provider)
	require.NotNil(t, got.connection, "a connection model is reached through its connection")

	got = f.a.utilityTargetFor(bob, provider.UtilityRoleQuick)
	require.Equal(t, "gpt-5.1", got.name, "bob gets the deployment's choice, not alice's")
	require.Nil(t, got.connection)

	got = f.a.utilityTargetFor(alice, provider.UtilityRoleMemory)
	require.Equal(t, archivalOpenAIModel, got.name, "a choice applies to its role only")
}

// A choice that can't be used falls back to the next one, and past the last to the built-in
// model, so background work keeps running; each fallback is counted.
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

	require.Equal(t, "gpt-5.1", f.a.utilityTargetFor(alice, provider.UtilityRoleMemory).name, "the deployment's choice")
	require.EqualValues(t, 1, tm.CounterValue(t, telemetry.UtilityModelFallbacks.Name,
		telemetry.AttrUtilityRole.String("memory"), telemetry.AttrUtilityScope.String("account"), telemetry.AttrReason.String("unavailable")))

	// The admin deletes the catalog model: a soft delete, so its setting stays and is passed over.
	_, err = f.ent.Model.UpdateOneID(f.catalog.ID).SetDeleted(true).Save(ctx)
	require.NoError(t, err)
	require.Equal(t, utilityTarget{name: archivalOpenAIModel}, f.a.utilityTargetFor(alice, provider.UtilityRoleMemory), "the built-in model")
}

// A call on a role whose chosen model is a connection's goes to that connection.
func TestCallUtility_ChosenConnectionModel(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	srv, path, req := recordingServer(t, `{"id":"c","object":"chat.completion","created":1,"model":"qwen3:0.6b","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"named"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	f.serve(f.aliceModel, ConnectionEndpoint{Preset: "ollama", Flavor: ConnectionFlavorOpenAI, BaseURL: srv.URL + "/v1"})
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleQuick, f.aliceModel.ID)

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

	require.True(t, f.a.checkpointChainsResponses(alice, openAIChat))
	f.choose(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleMemory, f.catalog.ID)
	require.True(t, f.a.checkpointChainsResponses(alice, openAIChat), "an OpenAI memory model still chains")
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleMemory, f.aliceModel.ID)
	require.False(t, f.a.checkpointChainsResponses(alice, openAIChat))
	require.False(t, f.a.checkpointChainsResponses(alice, &chatContext{model: "gemini-3.5", modelProvider: string(models.ModelProviderGoogle)}))

	got := f.a.rebuiltCheckpointTarget(alice, openAIChat)
	require.Equal(t, "qwen3:0.6b", got.name, "the rebuilt path uses the chosen memory model")
}
