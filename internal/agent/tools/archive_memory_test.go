package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeArchiveMemoryStore serves one stored memory and records the archive it is asked for.
type fakeArchiveMemoryStore struct {
	mem        *models.Memory
	archiveErr error
	archived   *models.MemoryArchival
	archivedID uuid.UUID
}

func (f *fakeArchiveMemoryStore) GetMemory(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.Memory, error) {
	if f.mem == nil || f.mem.ID != id {
		return nil, datastore.ErrMemoryNotFound
	}
	return f.mem, nil
}

func (f *fakeArchiveMemoryStore) ArchiveMemory(_ context.Context, _ uuid.UUID, id uuid.UUID, archival models.MemoryArchival) (*models.MemoryMergeEvent, error) {
	if f.archiveErr != nil {
		return nil, f.archiveErr
	}
	f.archivedID, f.archived = id, &archival
	return &models.MemoryMergeEvent{ID: uuid.New(), SurvivorMemoryID: id, MergeType: models.MemoryMergeTypeArchive, CreatedAt: time.Now()}, nil
}

func runArchiveMemory(t *testing.T, store *fakeArchiveMemoryStore, chat *models.Chat, external bool, args string) archiveMemoryResult {
	t.Helper()
	tool := &ArchiveMemoryTool{store: store, logger: zap.NewNop()}
	out, err := tool.Archive(context.Background(), chat, external, []byte(args))
	require.NoError(t, err, "refusals are results, not Go errors")
	var res archiveMemoryResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res
}

func TestArchiveMemory_ArchivesAnExactIDWithProvenance(t *testing.T) {
	personalityID := uuid.New()
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: personalityID}
	mem := &models.Memory{ID: uuid.New(), Content: "Lives in Berlin", Scope: MemoryScopeUser, PinnedPersonalityID: &personalityID}
	store := &fakeArchiveMemoryStore{mem: mem}

	res := runArchiveMemory(t, store, chat, false, fmt.Sprintf(`{"memory_id":"Memory:%s","reason":"moved to Lisbon"}`, mem.ID))
	require.True(t, res.Success, res.Error)
	require.Equal(t, "memory:"+mem.ID.String(), res.MemoryID)
	require.Equal(t, "Lives in Berlin", res.Content)
	require.NotEmpty(t, res.EventID)
	require.Contains(t, res.Note, "not deleted")
	require.Contains(t, res.Note, "Archived view")

	require.Equal(t, mem.ID, store.archivedID)
	require.Equal(t, &chat.ID, store.archived.ChatID)
	require.Equal(t, &personalityID, store.archived.PersonalityID)
	require.Equal(t, "moved to Lisbon", store.archived.Reason)
}

func TestArchiveMemory_NoPersonalityRecordsOnlyTheChat(t *testing.T) {
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New()}
	mem := &models.Memory{ID: uuid.New(), Content: "x", Scope: MemoryScopeChat, ChatID: chat.ID}
	store := &fakeArchiveMemoryStore{mem: mem}

	res := runArchiveMemory(t, store, chat, false, fmt.Sprintf(`{"memory_id":%q}`, mem.ID))
	require.True(t, res.Success, res.Error)
	require.Nil(t, store.archived.PersonalityID)
	require.Equal(t, &chat.ID, store.archived.ChatID)
}

func TestArchiveMemory_Refusals(t *testing.T) {
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
			args: fmt.Sprintf(`{"memory_id":%q}`, id), storeErr: fmt.Errorf("%w: the user starred it", datastore.ErrMemoryNotArchivable),
			want: "cannot be archived: the user starred it",
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
			store := &fakeArchiveMemoryStore{mem: tc.mem, archiveErr: tc.storeErr}
			c := chat
			if tc.chat != nil {
				c = tc.chat
			}
			res := runArchiveMemory(t, store, c, tc.external, tc.args)
			require.False(t, res.Success)
			require.Contains(t, res.Error, tc.want)
			require.Nil(t, store.archived, "nothing was archived")
		})
	}
}

// On an external (Discord relay) turn, a memory learned from the people in the thread may go.
func TestArchiveMemory_ExternalTurnArchivesExternalMemory(t *testing.T) {
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New()}
	mem := &models.Memory{ID: uuid.New(), Scope: MemoryScopeChat, ChatID: chat.ID, Provenance: models.MemoryProvenanceExternal}
	store := &fakeArchiveMemoryStore{mem: mem}

	res := runArchiveMemory(t, store, chat, true, fmt.Sprintf(`{"memory_id":%q}`, mem.ID))
	require.True(t, res.Success, res.Error)
}

// find_context lifecycle_events shows an archive's reason, so the agent can see why a memory went.
func TestToRecallLifecycleEvent_ArchiveCarriesReason(t *testing.T) {
	ev := toRecallLifecycleEvent(&models.MemoryMergeEvent{
		ID: uuid.New(), SurvivorMemoryID: uuid.New(), MergeType: models.MemoryMergeTypeArchive,
		Content: "Lives in Berlin", Archival: &models.MemoryArchival{Reason: "moved to Lisbon"},
	})
	require.Equal(t, "archive", ev.MergeType)
	require.Equal(t, "moved to Lisbon", ev.Reason)
}

// The agent is told that archiving is not deleting and where the user restores a memory, so it
// archives a wrong memory without hesitating and can tell the user how to get it back.
func TestArchiveMemoryDescription_SaysTheUserCanRestore(t *testing.T) {
	require.Contains(t, ArchiveMemoryDescription, "Archiving is not deleting")
	require.Contains(t, ArchiveMemoryDescription, "Archived view")
	require.NotContains(t, strings.ToLower(ArchiveMemoryDescription), "delete it", "the tool never offers deletion")
}
