package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestChatLoadedSkills_AddRemoveRetain(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createGeneratedTestSchema)
	defer cleanup()
	ctx := context.Background()
	userID := createMoodTestUser(t, ds)
	other := createMoodTestUser(t, ds)
	chat, err := ds.dbClient.Chat.Create().SetName("c").SetOwnerID(userID).Save(ctx)
	require.NoError(t, err)

	a, err := ds.CreateRitual(ctx, userID, models.Ritual{Name: "A", Description: "d", Content: "c"})
	require.NoError(t, err)
	b, err := ds.CreateRitual(ctx, userID, models.Ritual{Name: "B", Description: "d", Content: "c"})
	require.NoError(t, err)

	added, err := ds.AddChatLoadedSkills(ctx, userID, chat.ID, []uuid.UUID{a.ID, b.ID, a.ID})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{a.ID, b.ID}, added)
	added, err = ds.AddChatLoadedSkills(ctx, userID, chat.ID, []uuid.UUID{a.ID})
	require.NoError(t, err)
	require.Empty(t, added, "already loaded")

	loaded, err := ds.ListChatLoadedSkillIDs(ctx, userID, chat.ID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{a.ID, b.ID}, loaded)

	// Another user cannot read or change the chat's loaded skills.
	_, err = ds.ListChatLoadedSkillIDs(ctx, other, chat.ID)
	require.ErrorIs(t, err, ErrChatNotFound)
	_, err = ds.AddChatLoadedSkills(ctx, other, chat.ID, []uuid.UUID{a.ID})
	require.ErrorIs(t, err, ErrChatNotFound)

	removed, err := ds.RemoveChatLoadedSkills(ctx, userID, chat.ID, []uuid.UUID{b.ID, uuid.New()})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{b.ID}, removed)

	unloaded, err := ds.RetainChatLoadedSkills(ctx, userID, chat.ID, map[uuid.UUID]struct{}{})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{a.ID}, unloaded)
	loaded, err = ds.ListChatLoadedSkillIDs(ctx, userID, chat.ID)
	require.NoError(t, err)
	require.Empty(t, loaded)
}

func TestListRitualsInChatScopeAndMessageRitualIDs(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createGeneratedTestSchema)
	defer cleanup()
	ctx := context.Background()
	userID := createMoodTestUser(t, ds)
	p1 := createMoodTestPersonality(t, ds, userID)
	p2 := createMoodTestPersonality(t, ds, userID)

	global, err := ds.CreateRitual(ctx, userID, models.Ritual{Name: "Zed Global", Description: "d", Content: "c"})
	require.NoError(t, err)
	forP1, err := ds.CreateRitual(ctx, userID, models.Ritual{Name: "Alpha P1", Description: "d", Content: "c", PersonalityID: p1})
	require.NoError(t, err)
	_, err = ds.CreateRitual(ctx, userID, models.Ritual{Name: "Beta P2", Description: "d", Content: "c", PersonalityID: p2})
	require.NoError(t, err)

	scope, err := ds.ListRitualsInChatScope(ctx, userID, p1)
	require.NoError(t, err)
	require.Len(t, scope, 2)
	require.Equal(t, "alpha-p1", scope[0].Slug, "sorted by slug")
	require.Equal(t, "zed-global", scope[1].Slug)

	scope, err = ds.ListRitualsInChatScope(ctx, userID, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, scope, 1)
	require.Equal(t, global.ID, scope[0].ID)

	// Skills a message of the chat invoked.
	chat, err := ds.dbClient.Chat.Create().SetName("c").SetOwnerID(userID).SetPersonalityID(p1).Save(ctx)
	require.NoError(t, err)
	_, err = ds.dbClient.ChatMessage.Create().SetMessage("hi").SetOrigin(chatmessage.OriginUser).SetChatID(chat.ID).AddRitualIDs(forP1.ID).Save(ctx)
	require.NoError(t, err)
	used, err := ds.ListChatMessageRitualIDs(ctx, userID, chat.ID)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]struct{}{forP1.ID: {}}, used)

	// ListRituals' free-text filter matches the slug too.
	page, err := ds.ListRituals(ctx, userID, 1, 10, models.RitualFilters{Query: strPtr("zed-glo")})
	require.NoError(t, err)
	require.Equal(t, 1, page.TotalCount)
}
