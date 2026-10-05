package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	entmerge "github.com/theimaginaryfoundation/what-iff/ent/memorymergeevent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// A restricted (sandbox) chat's checkpoint merge or link is confined to memories that chat
// created (WithChatMemoriesOnly): it may not fold, rewrite, retire or relink anything else. The
// scope of what it creates is the caller's choice, as in any chat.

func foldTestVector(seed float32) []float32 {
	vec := make([]float32, 8)
	vec[0] = seed
	vec[1] = seed / 2
	return vec
}

// foldTestMemory describes a memory row inserted by insertFoldTestMemory.
type foldTestMemory struct {
	scope      entmemory.Scope // default User
	content    string
	confidence float64
	embedding  []float32 // nil = no embedding row
}

func insertFoldTestMemory(t *testing.T, ds *Datastore, userID uuid.UUID, m foldTestMemory) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	scope := m.scope
	if scope == "" {
		scope = entmemory.ScopeUser
	}
	row, err := ds.dbClient.Memory.Create().
		SetContent(m.content).
		SetScope(scope).
		SetType(entmemory.TypeContext).
		SetStatus(entmemory.StatusActive).
		SetConfidence(m.confidence).
		SetOwnerID(userID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	require.NoError(t, err)
	if m.embedding != nil {
		_, err := ds.dbClient.Embedding.Create().
			SetEmbedding(pgvector.NewVector(m.embedding)).
			SetMemoryID(row.ID).
			Save(ctx)
		require.NoError(t, err)
	}
	return row.ID
}

func foldTestGroup(canonical string) models.MemoryMergeGroupProposal {
	return models.MemoryMergeGroupProposal{CanonicalContent: canonical, Scope: "User", Confidence: models.MemoryConfidenceHigh}
}

func foldTestEvent(t *testing.T, ds *Datastore, userID, survivorID uuid.UUID) *ent.MemoryMergeEvent {
	t.Helper()
	row, err := ds.dbClient.MemoryMergeEvent.Query().
		Where(entmerge.UserIDEQ(userID), entmerge.SurvivorMemoryIDEQ(survivorID)).
		Only(context.Background())
	require.NoError(t, err)
	return row
}

func newFoldTestDatastore(t *testing.T) (*Datastore, uuid.UUID, uuid.UUID) {
	t.Helper()
	ds, cleanup := newMemoryMergeTestDatastore(t)
	t.Cleanup(cleanup)
	userID := uuid.New()
	chatID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, chatID))
	return ds, userID, chatID
}

func newChatOnlyFixture(t *testing.T) (ds *Datastore, userID, chatID, otherChatID uuid.UUID) {
	t.Helper()
	ds, userID, chatID = newFoldTestDatastore(t)
	otherChatID = uuid.New()
	require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, otherChatID))
	return ds, userID, chatID, otherChatID
}

func chatOnlyGroup(canonical, scope string) models.MemoryMergeGroupProposal {
	return models.MemoryMergeGroupProposal{CanonicalContent: canonical, Scope: scope, Confidence: models.MemoryConfidenceHigh}
}

func TestPersistMemoryMergeGroup_ChatMemoriesOnly_RefusesForeignSurvivorAndAbsorbed(t *testing.T) {
	ds, userID, chatID, otherChatID := newChatOnlyFixture(t)
	ctx := context.Background()
	mine, mine2, owners, elsewhere := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, insertMemoryMergeTestMemory(t, ds, mine, userID, chatID, entmemory.ScopeChat, "Likes tea", nil))
	require.NoError(t, insertMemoryMergeTestMemory(t, ds, mine2, userID, chatID, entmemory.ScopeChat, "likes TEA", nil))
	require.NoError(t, insertMemoryMergeTestMemory(t, ds, owners, userID, uuid.Nil, entmemory.ScopeUser, "The owner's PIN is 1234", nil))
	require.NoError(t, insertMemoryMergeTestMemory(t, ds, elsewhere, userID, otherChatID, entmemory.ScopeChat, "Another chat's note", nil))

	statusOf := func(id uuid.UUID) entmemory.Status {
		row, err := ds.dbClient.Memory.Get(ctx, id)
		require.NoError(t, err)
		return row.Status
	}
	contentOf := func(id uuid.UUID) string {
		row, err := ds.dbClient.Memory.Get(ctx, id)
		require.NoError(t, err)
		return row.Content
	}

	// A survivor the chat did not create (owner's User-scope memory, or another chat's).
	for _, foreign := range []uuid.UUID{owners, elsewhere} {
		_, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, chatOnlyGroup("rewritten by a stranger", "Chat"), 2,
			&foreign, []uuid.UUID{mine}, foldTestVector(3), uuid.Nil, nil, nil, WithChatMemoriesOnly())
		require.ErrorIs(t, err, ErrMemoryOutsideChat)
		require.Equal(t, entmemory.StatusActive, statusOf(foreign))
		require.Equal(t, entmemory.StatusActive, statusOf(mine), "nothing was retired")
	}
	require.Equal(t, "The owner's PIN is 1234", contentOf(owners), "the owner's memory was not rewritten")

	// A survivor of this chat absorbing a memory it did not create: also refused, nothing retired.
	for _, foreign := range []uuid.UUID{owners, elsewhere} {
		_, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, chatOnlyGroup("Likes tea", "Chat"), 2,
			&mine, []uuid.UUID{foreign}, nil, uuid.Nil, nil, nil, WithChatMemoriesOnly())
		require.ErrorIs(t, err, ErrMemoryOutsideChat)
		require.Equal(t, entmemory.StatusActive, statusOf(foreign), "a foreign memory is not retired")
	}

	// Control: without the option the same call goes through (the guard is what stops it).
	_, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, chatOnlyGroup("Likes tea", "Chat"), 2,
		&mine, []uuid.UUID{elsewhere}, nil, uuid.Nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, entmemory.StatusInactive, statusOf(elsewhere))

	// Folding this chat's own memories is allowed.
	_, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, chatOnlyGroup("Likes tea", "Chat"), 2,
		&mine, []uuid.UUID{mine2}, nil, uuid.Nil, nil, nil, WithChatMemoriesOnly())
	require.NoError(t, err)
	require.Equal(t, entmemory.StatusInactive, statusOf(mine2))
}

func TestPersistMemoryMergeGroup_ChatMemoriesOnly_NewMemoryKeepsItsScope(t *testing.T) {
	ds, userID, chatID, _ := newChatOnlyFixture(t)
	ctx := context.Background()

	// A new-only group the grouping model proposed at User scope stays User scope; the level the
	// caller passed (already capped by the chat's limit) is stored.
	mem, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, chatOnlyGroup("Prefers metric units", "User"), 1,
		nil, nil, foldTestVector(1), uuid.Nil, nil, nil, WithChatMemoriesOnly(), WithNewMemberSensitivity(models.MemorySensitivityPublic))
	require.NoError(t, err)
	row, err := ds.dbClient.Memory.Get(ctx, mem.ID)
	require.NoError(t, err)
	require.Equal(t, entmemory.ScopeUser, row.Scope)
	require.Equal(t, entmemory.SensitivityPublic, row.Sensitivity)
	source, err := row.QueryChat().OnlyID(ctx)
	require.NoError(t, err, "a User memory records the chat it came from")
	require.Equal(t, chatID, source)
	// ... so the same restricted chat can fold it on a later checkpoint.
	_, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, chatOnlyGroup("Prefers metric units", "User"), 2,
		&mem.ID, nil, nil, uuid.Nil, nil, nil, WithChatMemoriesOnly())
	require.NoError(t, err)

	// A Chat-scoped one is created in the asking chat.
	mem, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, chatOnlyGroup("Mallory runs the Discord server", "Chat"), 1,
		nil, nil, foldTestVector(2), uuid.Nil, nil, nil, WithChatMemoriesOnly(), WithNewMemberSensitivity(models.MemorySensitivitySensitive))
	require.NoError(t, err)
	row, err = ds.dbClient.Memory.Get(ctx, mem.ID)
	require.NoError(t, err)
	require.Equal(t, entmemory.ScopeChat, row.Scope)
	createdIn, err := row.QueryChat().OnlyID(ctx)
	require.NoError(t, err)
	require.Equal(t, chatID, createdIn)
	require.Equal(t, entmemory.SensitivitySensitive, row.Sensitivity)
}

func TestPersistMemoryLinkGroup_ChatMemoriesOnly_RefusesForeignMembers(t *testing.T) {
	ds, userID, chatID, otherChatID := newChatOnlyFixture(t)
	ctx := context.Background()
	mine, elsewhere := uuid.New(), uuid.New()
	require.NoError(t, insertMemoryMergeTestMemory(t, ds, mine, userID, chatID, entmemory.ScopeChat, "Likes tea", nil))
	require.NoError(t, insertMemoryMergeTestMemory(t, ds, elsewhere, userID, otherChatID, entmemory.ScopeChat, "Another chat's note", nil))

	_, err := ds.PersistMemoryLinkGroup(ctx, userID, chatID, "User", "angles", []uuid.UUID{mine, elsewhere}, nil, nil, nil, uuid.Nil, WithChatMemoriesOnly())
	require.ErrorIs(t, err, ErrMemoryOutsideChat)
	for _, id := range []uuid.UUID{mine, elsewhere} {
		row, err := ds.dbClient.Memory.Get(ctx, id)
		require.NoError(t, err)
		require.Nil(t, row.LinkGroupID, "nothing was linked")
	}

	// This chat's own memory plus a new member links.
	ev, err := ds.PersistMemoryLinkGroup(ctx, userID, chatID, "Chat", "angles", []uuid.UUID{mine},
		[]LinkGroupNewMember{{Content: "Tea is calming", Embedding: foldTestVector(1)}}, nil, nil, uuid.Nil, WithChatMemoriesOnly())
	require.NoError(t, err)
	require.NotNil(t, ev)
	scopes, err := ds.dbClient.Memory.Query().Where(entmemory.LinkGroupID(*ev.LinkGroupID)).All(ctx)
	require.NoError(t, err)
	require.Len(t, scopes, 2)
	for _, m := range scopes {
		require.Equal(t, entmemory.ScopeChat, m.Scope)
	}
}
