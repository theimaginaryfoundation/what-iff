package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
)

type weaveFixture struct {
	ds      *Datastore
	owner   uuid.UUID
	other   uuid.UUID
	persona uuid.UUID
	// discord, buzz: sandboxed relay threads of persona; private: a sandboxed thread nobody else
	// talks in; open: an account (not sandboxed) thread; elsewhere: a sandbox of another persona.
	discord, buzz, private, open, elsewhere uuid.UUID
}

func newWeaveFixture(t *testing.T) *weaveFixture {
	t.Helper()
	ds, cleanup := newTestDatastore(t, createMemoryImportTestSchema, alterChatsTableForAgentJobTests)
	t.Cleanup(cleanup)
	f := &weaveFixture{ds: ds, owner: uuid.New(), other: uuid.New(), persona: uuid.New(),
		discord: uuid.New(), buzz: uuid.New(), private: uuid.New(), open: uuid.New(), elsewhere: uuid.New()}
	createTestUser(t, ds, f.owner)
	createTestUser(t, ds, f.other)
	createTestPersonality(t, ds, f.persona, f.owner)
	otherPersona := uuid.New()
	createTestPersonality(t, ds, otherPersona, f.owner)
	mk := func(id uuid.UUID, name string, scope entchat.ContextScope, persona uuid.UUID) {
		_, err := ds.dbClient.Chat.Create().SetID(id).SetName(name).SetOwnerID(f.owner).
			SetContextScope(scope).SetPersonalityID(persona).Save(context.Background())
		require.NoError(t, err)
	}
	mk(f.discord, "Discord · #general", entchat.ContextScopeSandbox, f.persona)
	mk(f.buzz, "Buzz · #town-square", entchat.ContextScopeSandbox, f.persona)
	mk(f.private, "Notes", entchat.ContextScopeSandbox, f.persona)
	mk(f.open, "Everyday", entchat.ContextScopeAccount, f.persona)
	mk(f.elsewhere, "Other persona", entchat.ContextScopeSandbox, otherPersona)
	return f
}

func TestCreateWeaveJoinsOnlySandboxesOfOnePersona(t *testing.T) {
	f := newWeaveFixture(t)
	ctx := context.Background()

	_, err := f.ds.CreateWeave(ctx, f.owner, "rooms", []uuid.UUID{f.discord})
	assert.ErrorIs(t, err, ErrWeaveChat, "a weave joins at least two threads")
	_, err = f.ds.CreateWeave(ctx, f.owner, "rooms", []uuid.UUID{f.discord, f.open})
	assert.ErrorIs(t, err, ErrWeaveChat, "only sandboxed threads")
	assert.Contains(t, err.Error(), "not sandboxed")
	_, err = f.ds.CreateWeave(ctx, f.owner, "rooms", []uuid.UUID{f.discord, f.elsewhere})
	assert.ErrorIs(t, err, ErrWeaveChat, "one personality")
	_, err = f.ds.CreateWeave(ctx, f.other, "rooms", []uuid.UUID{f.discord, f.buzz})
	assert.ErrorIs(t, err, ErrChatNotFound, "another account's threads are not found")

	w, err := f.ds.CreateWeave(ctx, f.owner, "rooms", []uuid.UUID{f.discord, f.buzz})
	require.NoError(t, err)
	assert.Equal(t, "rooms", w.Name)
	assert.Len(t, w.Chats, 2)

	_, err = f.ds.CreateWeave(ctx, f.owner, "again", []uuid.UUID{f.buzz, f.private})
	assert.ErrorIs(t, err, ErrWeaveChat, "a thread is in at most one weave")

	_, err = f.ds.GetWeave(ctx, f.other, w.ID)
	assert.ErrorIs(t, err, ErrWeaveNotFound)
}

func TestWeavePeersWidenTheSandboxReadsOnly(t *testing.T) {
	f := newWeaveFixture(t)
	ctx := context.Background()
	fromDiscord, fromBuzz, fromPrivate, owners := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, insertMemoryMergeTestMemory(t, f.ds, fromDiscord, f.owner, f.discord, entmemory.ScopeChat, "alice likes Rust", nil))
	require.NoError(t, insertMemoryMergeTestMemory(t, f.ds, fromBuzz, f.owner, f.buzz, entmemory.ScopeChat, "bob ships on Fridays", nil))
	require.NoError(t, insertMemoryMergeTestMemory(t, f.ds, fromPrivate, f.owner, f.private, entmemory.ScopeChat, "a private note", nil))
	require.NoError(t, insertMemoryMergeTestMemory(t, f.ds, owners, f.owner, uuid.Nil, entmemory.ScopeUser, "The owner's PIN", nil))
	all := []uuid.UUID{fromDiscord, fromBuzz, fromPrivate, owners}
	readable := func(chatID uuid.UUID) []uuid.UUID {
		peers, err := readableChatIDs(ctx, f.ds.dbClient, f.owner, chatID)
		require.NoError(t, err)
		ids, err := f.ds.dbClient.Memory.Query().
			Where(entmemory.IDIn(all...), relatedMemoryReadable(chatID, f.persona, true, peers[1:]...)).
			IDs(ctx)
		require.NoError(t, err)
		return ids
	}

	assert.ElementsMatch(t, []uuid.UUID{fromDiscord}, readable(f.discord), "unwoven: only itself")

	w, err := f.ds.CreateWeave(ctx, f.owner, "rooms", []uuid.UUID{f.discord, f.buzz})
	require.NoError(t, err)
	assert.ElementsMatch(t, []uuid.UUID{fromDiscord, fromBuzz}, readable(f.discord), "woven: itself and its peer")
	assert.ElementsMatch(t, []uuid.UUID{fromDiscord, fromBuzz}, readable(f.buzz))
	assert.ElementsMatch(t, []uuid.UUID{fromPrivate}, readable(f.private), "outside the weave nothing changes")

	// The replay filter follows the same set, so leaving stops replaying a peer's memories.
	got, err := f.ds.MemoryIDsReadableInChat(ctx, f.owner, f.discord, all)
	require.NoError(t, err)
	assert.Equal(t, map[uuid.UUID]struct{}{fromDiscord: {}, fromBuzz: {}}, got)

	chat, err := f.ds.GetChat(ctx, f.owner, f.discord)
	require.NoError(t, err)
	require.Len(t, chat.WeavePeers, 1)
	assert.Equal(t, f.buzz, chat.WeavePeers[0].ID)
	assert.Equal(t, "Buzz · #town-square", chat.WeavePeers[0].Name)
	assert.True(t, chat.CanReadChat(f.buzz))
	assert.False(t, chat.CanReadChat(f.private))

	_, err = f.ds.RemoveChatFromWeave(ctx, f.owner, w.ID, f.buzz)
	require.NoError(t, err)
	assert.ElementsMatch(t, []uuid.UUID{fromDiscord}, readable(f.discord), "a peer that left is no longer read")
	got, err = f.ds.MemoryIDsReadableInChat(ctx, f.owner, f.discord, all)
	require.NoError(t, err)
	assert.Equal(t, map[uuid.UUID]struct{}{fromDiscord: {}}, got)
}

func TestAPeerThatLeftTheSandboxDropsOut(t *testing.T) {
	f := newWeaveFixture(t)
	ctx := context.Background()
	_, err := f.ds.CreateWeave(ctx, f.owner, "rooms", []uuid.UUID{f.discord, f.buzz})
	require.NoError(t, err)
	require.NoError(t, f.ds.dbClient.Chat.UpdateOneID(f.buzz).SetContextScope(entchat.ContextScopeAccount).Exec(ctx))

	ids, err := readableChatIDs(ctx, f.ds.dbClient, f.owner, f.discord)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{f.discord}, ids, "fail closed: only sandboxed peers count")
}

func TestAddRemoveAndDeleteWeave(t *testing.T) {
	f := newWeaveFixture(t)
	ctx := context.Background()
	w, err := f.ds.CreateWeave(ctx, f.owner, "rooms", []uuid.UUID{f.discord, f.buzz})
	require.NoError(t, err)

	w, err = f.ds.AddChatToWeave(ctx, f.owner, w.ID, f.private)
	require.NoError(t, err)
	assert.Len(t, w.Chats, 3)
	w, err = f.ds.AddChatToWeave(ctx, f.owner, w.ID, f.private)
	require.NoError(t, err, "adding a member again is a no-op")
	assert.Len(t, w.Chats, 3)
	_, err = f.ds.AddChatToWeave(ctx, f.owner, w.ID, f.elsewhere)
	assert.ErrorIs(t, err, ErrWeaveChat, "another personality's thread")
	_, err = f.ds.AddChatToWeave(ctx, f.owner, w.ID, f.open)
	assert.ErrorIs(t, err, ErrWeaveChat, "an account thread")

	_, err = f.ds.RemoveChatFromWeave(ctx, f.owner, w.ID, f.open)
	assert.ErrorIs(t, err, ErrWeaveChat, "not a member")

	list, err := f.ds.ListWeaves(ctx, f.owner)
	require.NoError(t, err)
	require.Len(t, list, 1)
	others, err := f.ds.ListWeaves(ctx, f.other)
	require.NoError(t, err)
	assert.Empty(t, others)

	require.NoError(t, f.ds.DeleteWeave(ctx, f.owner, w.ID))
	n, err := f.ds.dbClient.Chat.Query().Where(entchat.WeaveIDNotNil()).Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, n, "deleting a weave unweaves its threads")
	assert.ErrorIs(t, f.ds.DeleteWeave(ctx, f.owner, w.ID), ErrWeaveNotFound)
}
