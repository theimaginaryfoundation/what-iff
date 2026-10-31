package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeRemoveMemoryStore serves one stored memory and records the removal it is asked for.
type fakeRemoveMemoryStore struct {
	mem       *models.Memory
	removeErr error
	removed   *models.MemoryRemoval
	removedID uuid.UUID
}

func (f *fakeRemoveMemoryStore) GetMemory(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.Memory, error) {
	if f.mem == nil || f.mem.ID != id {
		return nil, datastore.ErrMemoryNotFound
	}
	return f.mem, nil
}

func (f *fakeRemoveMemoryStore) RemoveMemory(_ context.Context, _ uuid.UUID, id uuid.UUID, removal models.MemoryRemoval) (*models.MemoryMergeEvent, error) {
	if f.removeErr != nil {
		return nil, f.removeErr
	}
	f.removedID, f.removed = id, &removal
	return &models.MemoryMergeEvent{ID: uuid.New(), SurvivorMemoryID: id, MergeType: models.MemoryMergeTypeRemove, CreatedAt: time.Now()}, nil
}

func runRemoveMemory(t *testing.T, store *fakeRemoveMemoryStore, chat *models.Chat, external bool, args string) removeMemoryResult {
	t.Helper()
	tool := &RemoveMemoryTool{store: store, logger: zap.NewNop()}
	out, err := tool.Remove(context.Background(), chat, external, []byte(args))
	require.NoError(t, err, "refusals are results, not Go errors")
	var res removeMemoryResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res
}

func TestRemoveMemory_RemovesAnExactIDWithProvenance(t *testing.T) {
	personalityID := uuid.New()
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: personalityID}
	mem := &models.Memory{ID: uuid.New(), Content: "Lives in Berlin", Scope: MemoryScopeUser, PinnedPersonalityID: &personalityID}
	store := &fakeRemoveMemoryStore{mem: mem}

	res := runRemoveMemory(t, store, chat, false, fmt.Sprintf(`{"memory_id":"Memory:%s","reason":"moved to Lisbon"}`, mem.ID))
	require.True(t, res.Success, res.Error)
	require.Equal(t, "memory:"+mem.ID.String(), res.MemoryID)
	require.Equal(t, "Lives in Berlin", res.Content)
	require.NotEmpty(t, res.EventID)
	require.Contains(t, res.Note, "Merge history")

	require.Equal(t, mem.ID, store.removedID)
	require.Equal(t, &chat.ID, store.removed.ChatID)
	require.Equal(t, &personalityID, store.removed.PersonalityID)
	require.Equal(t, "moved to Lisbon", store.removed.Reason)
}

func TestRemoveMemory_NoPersonalityRecordsOnlyTheChat(t *testing.T) {
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New()}
	mem := &models.Memory{ID: uuid.New(), Content: "x", Scope: MemoryScopeChat, ChatID: chat.ID}
	store := &fakeRemoveMemoryStore{mem: mem}

	res := runRemoveMemory(t, store, chat, false, fmt.Sprintf(`{"memory_id":%q}`, mem.ID))
	require.True(t, res.Success, res.Error)
	require.Nil(t, store.removed.PersonalityID)
	require.Equal(t, &chat.ID, store.removed.ChatID)
}

func TestRemoveMemory_Refusals(t *testing.T) {
	personalityID := uuid.New()
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: personalityID}
	otherPersonality := uuid.New()
	id := uuid.New()

	cases := []struct {
		name     string
		mem      *models.Memory
		chat     *models.Chat
		external bool
		args     string
		storeErr error
		want     string
	}{
		{name: "invalid json", args: `nope`, want: "invalid arguments"},
		{name: "missing id", args: `{}`, want: "memory_id is required"},
		{name: "prefix is not exact", args: fmt.Sprintf(`{"memory_id":"memory:%s"}`, id.String()[:8]), want: "not a full memory id"},
		{name: "description is not an id", args: `{"memory_id":"the Berlin one"}`, want: "not a full memory id"},
		{name: "unknown id", args: fmt.Sprintf(`{"memory_id":%q}`, id), want: "not found"},
		{
			name: "pinned to another personality",
			mem:  &models.Memory{ID: id, Scope: MemoryScopeUser, PinnedPersonalityID: &otherPersonality},
			args: fmt.Sprintf(`{"memory_id":%q}`, id), want: "pinned to another personality",
		},
		{
			name: "another conversation's thread memory",
			mem:  &models.Memory{ID: id, Scope: MemoryScopeChat, ChatID: uuid.New()},
			args: fmt.Sprintf(`{"memory_id":%q}`, id), want: "another conversation",
		},
		{
			name: "owner's memory on an external turn", external: true,
			mem:  &models.Memory{ID: id, Scope: MemoryScopeUser, Provenance: models.MemoryProvenanceUser},
			args: fmt.Sprintf(`{"memory_id":%q}`, id), want: "the user's own",
		},
		{
			name: "the store refuses (starred)",
			mem:  &models.Memory{ID: id, Scope: MemoryScopeUser},
			args: fmt.Sprintf(`{"memory_id":%q}`, id), storeErr: fmt.Errorf("%w: the user starred it", datastore.ErrMemoryNotRemovable),
			want: "cannot be removed: the user starred it",
		},
		{
			name: "sandboxed chat",
			chat: &models.Chat{ID: chat.ID, UserID: chat.UserID, ContextScope: models.ContextScopeSandbox},
			mem:  &models.Memory{ID: id, Scope: MemoryScopeChat, ChatID: chat.ID},
			args: fmt.Sprintf(`{"memory_id":%q}`, id), want: "sandboxed conversation",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeRemoveMemoryStore{mem: tc.mem, removeErr: tc.storeErr}
			c := chat
			if tc.chat != nil {
				c = tc.chat
			}
			res := runRemoveMemory(t, store, c, tc.external, tc.args)
			require.False(t, res.Success)
			require.Contains(t, res.Error, tc.want)
			require.Nil(t, store.removed, "nothing was removed")
		})
	}
}

// On an external (Discord relay) turn, a memory learned from the people in the thread may go.
func TestRemoveMemory_ExternalTurnRemovesExternalMemory(t *testing.T) {
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New()}
	mem := &models.Memory{ID: uuid.New(), Scope: MemoryScopeChat, ChatID: chat.ID, Provenance: models.MemoryProvenanceExternal}
	store := &fakeRemoveMemoryStore{mem: mem}

	res := runRemoveMemory(t, store, chat, true, fmt.Sprintf(`{"memory_id":%q}`, mem.ID))
	require.True(t, res.Success, res.Error)
}

// find_context lifecycle_events shows a removal's reason, so the agent can see why a memory went.
func TestToRecallLifecycleEvent_RemoveCarriesReason(t *testing.T) {
	ev := toRecallLifecycleEvent(&models.MemoryMergeEvent{
		ID: uuid.New(), SurvivorMemoryID: uuid.New(), MergeType: models.MemoryMergeTypeRemove,
		Content: "Lives in Berlin", Removal: &models.MemoryRemoval{Reason: "moved to Lisbon"},
	})
	require.Equal(t, "remove", ev.MergeType)
	require.Equal(t, "moved to Lisbon", ev.Reason)
}
