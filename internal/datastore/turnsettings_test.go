package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entpersonality "github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/userpreference"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// A new thread is seeded with its personality's reasoning effort and agent loop, falling back to
// the account default per setting, and keeps them after (ADR 0x026). Only explicit changes write
// them on UpdateChat.
func TestCreateChat_SeedsTurnSettingsFromPersonalityThenAccount(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	userID := createAgentJobTestUser(t, ds)
	modelID := createAgentJobTestModel(t, ds)
	_, err := ds.dbClient.UserPreference.Create().SetUserID(userID).SetDefaultModel(modelID).
		SetReasoningEffort(userpreference.ReasoningEffortLow).
		SetAgentLoop(userpreference.AgentLoopAdvanced).
		Save(ctx)
	require.NoError(t, err)
	personalityID := uuid.New()
	createTestPersonality(t, ds, personalityID, userID)
	require.NoError(t, ds.dbClient.Personality.UpdateOneID(personalityID).SetReasoningEffort(entpersonality.ReasoningEffortHigh).Exec(ctx))

	chat, err := ds.CreateChat(ctx, userID, models.Chat{Name: "seeded", PersonalityID: personalityID})
	require.NoError(t, err)
	require.Equal(t, models.ReasoningEffortHigh, *chat.ReasoningEffort, "the personality wins over the account default")
	require.Equal(t, models.AgentLoopAdvanced, *chat.AgentLoop, "the account default fills what the personality leaves unset")

	medium := models.ReasoningEffortMedium
	explicit, err := ds.CreateChat(ctx, userID, models.Chat{Name: "explicit", PersonalityID: personalityID, TurnSettingValues: models.TurnSettingValues{ReasoningEffort: &medium}})
	require.NoError(t, err)
	require.Equal(t, models.ReasoningEffortMedium, *explicit.ReasoningEffort, "a value given at creation wins")

	// A sandbox (the Discord relay) is not seeded: other people start its turns.
	sandbox, err := ds.CreateChat(ctx, userID, models.Chat{Name: "relay", PersonalityID: personalityID, ContextScope: models.ContextScopeSandbox})
	require.NoError(t, err)
	require.Nil(t, sandbox.ReasoningEffort)
	require.Nil(t, sandbox.AgentLoop)

	// Later changes to the personality do not reach the existing thread.
	require.NoError(t, ds.dbClient.Personality.UpdateOneID(personalityID).ClearReasoningEffort().Exec(ctx))
	reread, err := ds.GetChat(ctx, userID, chat.ID)
	require.NoError(t, err)
	require.Equal(t, models.ReasoningEffortHigh, *reread.ReasoningEffort)

	// A stale copy without the explicit flag does not write the settings back.
	stale := *reread
	stale.ReasoningEffort = nil
	updated, err := ds.UpdateChat(ctx, userID, stale)
	require.NoError(t, err)
	require.Equal(t, models.ReasoningEffortHigh, *updated.ReasoningEffort)

	// An explicit clear does.
	stale.SetReasoningEffort = true
	updated, err = ds.UpdateChat(ctx, userID, stale)
	require.NoError(t, err)
	require.Nil(t, updated.ReasoningEffort)
	require.Equal(t, models.AgentLoopAdvanced, *updated.AgentLoop)
}

func TestUpdateUserPreferences_TurnSettings(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	userID := createAgentJobTestUser(t, ds)
	modelID := createAgentJobTestModel(t, ds)
	_, err := ds.dbClient.UserPreference.Create().SetUserID(userID).SetDefaultModel(modelID).Save(ctx)
	require.NoError(t, err)

	high := models.ReasoningEffortHigh
	prefs, err := ds.UpdateUserPreferences(ctx, userID, models.UserPreferences{TurnSettingValues: models.TurnSettingValues{ReasoningEffort: &high}})
	require.NoError(t, err)
	require.Equal(t, models.ReasoningEffortHigh, *prefs.ReasoningEffort)
	require.Nil(t, prefs.AgentLoop)

	// Omitted fields are left alone.
	prefs, err = ds.UpdateUserPreferences(ctx, userID, models.UserPreferences{Theme: "light"})
	require.NoError(t, err)
	require.Equal(t, models.ReasoningEffortHigh, *prefs.ReasoningEffort)

	// "" clears.
	clear := models.ReasoningEffort("")
	prefs, err = ds.UpdateUserPreferences(ctx, userID, models.UserPreferences{TurnSettingValues: models.TurnSettingValues{ReasoningEffort: &clear}})
	require.NoError(t, err)
	require.Nil(t, prefs.ReasoningEffort)

	bad := models.ReasoningEffort("max")
	_, err = ds.UpdateUserPreferences(ctx, userID, models.UserPreferences{TurnSettingValues: models.TurnSettingValues{ReasoningEffort: &bad}})
	require.ErrorIs(t, err, ErrInvalidRequestBody)
}
