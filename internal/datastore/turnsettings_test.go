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

// A new thread stores only the settings it was given (ADR 0x026); its personality's values and
// the account default are read at each turn (InheritedTurnSettings), so later changes reach it.
// Only explicit changes write the thread's own values on UpdateChat.
func TestCreateChat_StoresOnlyExplicitTurnSettings(t *testing.T) {
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

	chat, err := ds.CreateChat(ctx, userID, models.Chat{Name: "plain", PersonalityID: personalityID})
	require.NoError(t, err)
	require.Nil(t, chat.ReasoningEffort, "nothing is copied from the personality")
	require.Nil(t, chat.AgentLoop, "nothing is copied from the account default")

	fromPersonality, fromAccount, err := ds.InheritedTurnSettings(ctx, userID, personalityID)
	require.NoError(t, err)
	require.Equal(t, models.ReasoningEffortHigh, *fromPersonality.ReasoningEffort)
	require.Nil(t, fromPersonality.AgentLoop)
	require.Equal(t, models.ReasoningEffortLow, *fromAccount.ReasoningEffort)
	require.Equal(t, models.AgentLoopAdvanced, *fromAccount.AgentLoop)

	// A later change to the personality is what the next turn reads.
	require.NoError(t, ds.dbClient.Personality.UpdateOneID(personalityID).ClearReasoningEffort().Exec(ctx))
	fromPersonality, _, err = ds.InheritedTurnSettings(ctx, userID, personalityID)
	require.NoError(t, err)
	require.Nil(t, fromPersonality.ReasoningEffort)

	// Another user's personality is not read.
	other, _, err := ds.InheritedTurnSettings(ctx, uuid.New(), personalityID)
	require.NoError(t, err)
	require.Nil(t, other.ReasoningEffort)

	medium := models.ReasoningEffortMedium
	explicit, err := ds.CreateChat(ctx, userID, models.Chat{Name: "explicit", PersonalityID: personalityID, TurnSettingValues: models.TurnSettingValues{ReasoningEffort: &medium}})
	require.NoError(t, err)
	require.Equal(t, models.ReasoningEffortMedium, *explicit.ReasoningEffort, "a value given at creation is the thread's own")

	// A stale copy without the explicit flag does not write the settings back.
	stale := *explicit
	stale.ReasoningEffort = nil
	updated, err := ds.UpdateChat(ctx, userID, stale)
	require.NoError(t, err)
	require.Equal(t, models.ReasoningEffortMedium, *updated.ReasoningEffort)

	// An explicit clear does.
	stale.SetReasoningEffort = true
	updated, err = ds.UpdateChat(ctx, userID, stale)
	require.NoError(t, err)
	require.Nil(t, updated.ReasoningEffort)
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
