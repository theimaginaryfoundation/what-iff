package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
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

	got, err := f.a.utilityTargetFor(alice, provider.UtilityRoleSimple)
	require.NoError(t, err)
	require.Equal(t, utilityTarget{name: models.UtilityModelName, provider: string(models.ModelProviderOpenAI)}, got, "nothing chosen: the built-in model, named with its provider")

	f.choose(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleSimple, f.catalog.ID)
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleSimple, f.aliceModel.ID)

	got, err = f.a.utilityTargetFor(alice, provider.UtilityRoleSimple)
	require.NoError(t, err)
	require.Equal(t, "qwen3:0.6b", got.name)
	require.Equal(t, "ollama", got.provider)
	require.NotNil(t, got.connection, "a connection model is reached through its connection")

	got, err = f.a.utilityTargetFor(bob, provider.UtilityRoleSimple)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.1", got.name, "bob gets the deployment's choice, not alice's")
	require.Nil(t, got.connection)

	got, err = f.a.utilityTargetFor(alice, provider.UtilityRoleMemory)
	require.NoError(t, err)
	require.Equal(t, archivalOpenAIModel, got.name, "a choice applies to its role only")
}

// A chosen model turned off since fails the role's calls rather than quietly running them on
// another model, which could send the conversation somewhere the account didn't choose.
func TestUtilityTargetFor_TurnedOffChoiceFails(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleMemory, f.aliceModel.ID)
	_, err := f.ent.Model.UpdateOneID(f.aliceModel.ID).SetDeleted(true).Save(context.Background())
	require.NoError(t, err)

	_, err = f.a.utilityTargetFor(apicontext.WithUserID(context.Background(), f.alice), provider.UtilityRoleMemory)
	require.ErrorIs(t, err, ErrModelDisabled)
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

	require.True(t, f.a.checkpointChainsResponses(alice, openAIChat))
	f.choose(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleMemory, f.catalog.ID)
	require.True(t, f.a.checkpointChainsResponses(alice, openAIChat), "an OpenAI memory model still chains")
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleMemory, f.aliceModel.ID)
	require.False(t, f.a.checkpointChainsResponses(alice, openAIChat))
	require.False(t, f.a.checkpointChainsResponses(alice, &chatContext{model: "gemini-3.5", modelProvider: string(models.ModelProviderGoogle)}))

	got, err := f.a.utilityTargetFor(alice, provider.UtilityRoleMemory)
	require.NoError(t, err)
	require.Equal(t, "qwen3:0.6b", got.name, "the rebuilt path runs on the chosen memory model")
}
