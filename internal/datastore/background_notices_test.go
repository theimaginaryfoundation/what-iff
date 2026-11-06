package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Serial, like the other tests that create tables from the generated schema: its shared
// definitions are written to by the migrator, so doing that in parallel is a data race.
func TestBackgroundNotices_Lifecycle(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createGeneratedTestSchema)
	t.Cleanup(cleanup)
	ctx := context.Background()
	alice, bob, chat := uuid.New(), uuid.New(), uuid.New()
	createTestUser(t, ds, alice)
	createTestUser(t, ds, bob)
	createTestChat(t, ds, chat, alice)

	missingKey := models.BackgroundNotice{Step: models.BackgroundStepMemory, Code: models.BackgroundNoticeProviderNotConfigured, Provider: "Anthropic", Model: "claude-haiku-4-5"}
	settle := func(o models.BackgroundOutcome) {
		t.Helper()
		require.NoError(t, ds.SettleBackgroundNotices(ctx, alice, chat, o))
	}
	list := func(user uuid.UUID) []models.BackgroundNotice {
		t.Helper()
		got, err := ds.ListBackgroundNotices(ctx, user, chat)
		require.NoError(t, err)
		return got
	}

	settle(models.BackgroundOutcome{Failed: []models.BackgroundNotice{missingKey}})
	settle(models.BackgroundOutcome{Failed: []models.BackgroundNotice{missingKey}})
	got := list(alice)
	require.Len(t, got, 1, "one notice per chat and step")
	require.Equal(t, models.BackgroundStepMemory, got[0].Step)
	require.Equal(t, "Anthropic", got[0].Provider)
	require.Equal(t, 2, got[0].Count)
	require.Empty(t, list(bob), "another account never sees it")

	require.ErrorIs(t, ds.DismissBackgroundNotice(ctx, bob, chat, got[0].ID), ErrBackgroundNoticeNotFound)
	require.NoError(t, ds.DismissBackgroundNotice(ctx, alice, chat, got[0].ID))
	require.Empty(t, list(alice))
	settle(models.BackgroundOutcome{Failed: []models.BackgroundNotice{missingKey}})
	require.Empty(t, list(alice), "dismissed stays dismissed while the cause is the same")

	// The same code from another provider is a different problem.
	elsewhere := missingKey
	elsewhere.Provider, elsewhere.Model = "OpenAI", "gpt-5.6-luna"
	settle(models.BackgroundOutcome{Failed: []models.BackgroundNotice{elsewhere}})
	got = list(alice)
	require.Len(t, got, 1, "a change of provider or model shows it again")
	require.Equal(t, "OpenAI", got[0].Provider)
	require.Equal(t, 1, got[0].Count)
	require.NoError(t, ds.DismissBackgroundNotice(ctx, alice, chat, got[0].ID))

	outage := missingKey
	outage.Code = models.BackgroundNoticeProviderError
	settle(models.BackgroundOutcome{Failed: []models.BackgroundNotice{outage}})
	got = list(alice)
	require.Len(t, got, 1, "a new cause shows again")
	require.Equal(t, models.BackgroundNoticeProviderError, got[0].Code)
	require.Equal(t, 1, got[0].Count)

	settle(models.BackgroundOutcome{Succeeded: []models.BackgroundStep{models.BackgroundStepMemory}})
	require.Empty(t, list(alice), "the step succeeding clears it")

	settle(models.BackgroundOutcome{Failed: []models.BackgroundNotice{missingKey}})
	require.NoError(t, ds.dbClient.Chat.DeleteOneID(chat).Exec(ctx))
	n, err := ds.dbClient.BackgroundNotice.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "deleting the chat deletes its notices")
}

// A chat holds one notice per step, so several at once, and an account many across its chats:
// the edges to the chat and the account are each notice's one chat and owner, not a limit of
// one notice per chat or account.
func TestBackgroundNotices_SeveralPerChatAndAccount(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createGeneratedTestSchema)
	t.Cleanup(cleanup)
	ctx := context.Background()
	alice, chatA, chatB := uuid.New(), uuid.New(), uuid.New()
	createTestUser(t, ds, alice)
	createTestChat(t, ds, chatA, alice)
	createTestChat(t, ds, chatB, alice)
	failed := models.BackgroundOutcome{Failed: []models.BackgroundNotice{
		{Step: models.BackgroundStepMemory, Code: models.BackgroundNoticeProviderError},
		{Step: models.BackgroundStepSummary, Code: models.BackgroundNoticeProviderError},
		{Step: models.BackgroundStepScratchpad, Code: models.BackgroundNoticeProviderError},
	}}
	require.NoError(t, ds.SettleBackgroundNotices(ctx, alice, chatA, failed))
	require.NoError(t, ds.SettleBackgroundNotices(ctx, alice, chatB, failed))

	for _, chat := range []uuid.UUID{chatA, chatB} {
		got, err := ds.ListBackgroundNotices(ctx, alice, chat)
		require.NoError(t, err)
		require.Len(t, got, 3)
	}
}
