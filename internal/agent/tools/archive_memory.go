package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const archiveMemoryToolName = "archive_memory"

// ArchiveMemoryDescription is the agent-facing description.
const ArchiveMemoryDescription = `Archive one memory that is wrong or out of date. An archived memory leaves active memory: search, find_context and automatic recall stop returning it, for you and for the user's other conversations.

Archiving is not deleting. The memory and its text are kept, and the user can bring it back from Memories (the Archived view, or Undo in Merge history), so archive freely when a memory is wrong, and say so to the user.

Use it to correct memory: when the user tells you a memory is wrong, or something they said replaces it ("I moved to Lisbon" makes "Lives in Berlin" stale). Find the memory first with find_context; its sources and results name each memory as memory:<uuid>. If the corrected fact is worth keeping, save it with create_memory as well.

Notes:
- memory_id must be the full id of one memory (memory:<uuid> or the bare uuid). A short prefix is refused: archiving never guesses which memory you meant.
- Only memories this conversation can recall can be archived: its own thread memories, and the user's global memories that are unpinned or pinned to this personality. Starred memories and conversation summaries cannot be archived; the user manages those in Memories.
- reason (optional, short) is kept with the archive so the user can see why.`

// ArchiveMemoryToolSpec is the shared function-tool shape for archive_memory.
var ArchiveMemoryToolSpec = FunctionToolSpec{
	Name:        archiveMemoryToolName,
	Description: ArchiveMemoryDescription,
	Properties: map[string]interface{}{
		"memory_id": map[string]interface{}{
			"type":        "string",
			"description": "The full id of the memory to archive, as find_context shows it: memory:<uuid> or the bare uuid.",
		},
		"reason": map[string]interface{}{
			"type":        "string",
			"description": fmt.Sprintf("Why it is being archived, in a few words (e.g. 'user moved to Lisbon'). At most %d characters.", models.MaxMemoryArchiveReasonRunes),
		},
	},
	Required: []string{"memory_id"},
}

// archiveMemoryStore is the narrow datastore surface archive_memory needs. *datastore.Datastore
// satisfies it.
type archiveMemoryStore interface {
	GetMemory(ctx context.Context, userID, id uuid.UUID) (*models.Memory, error)
	ArchiveMemory(ctx context.Context, userID, memoryID uuid.UUID, archival models.MemoryArchival) (*models.MemoryMergeEvent, error)
}

// ArchiveMemoryTool implements the archive_memory agent tool (ADR 0x036).
type ArchiveMemoryTool struct {
	store  archiveMemoryStore
	logger *zap.Logger
}

// NewArchiveMemoryTool constructs a ArchiveMemoryTool backed by the datastore.
func NewArchiveMemoryTool(ds *datastore.Datastore, logger *zap.Logger) *ArchiveMemoryTool {
	return &ArchiveMemoryTool{store: ds, logger: logger}
}

type archiveMemoryArgs struct {
	MemoryID string `json:"memory_id"`
	Reason   string `json:"reason"`
}

type archiveMemoryResult struct {
	Success  bool   `json:"success"`
	MemoryID string `json:"memory_id,omitempty"`
	Content  string `json:"content,omitempty"`
	// EventID is the archive event in the memory lifecycle log (find_context lifecycle_events).
	EventID string `json:"event_id,omitempty"`
	Note    string `json:"note,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Archive executes an archive_memory call. external is true on a turn whose memories are external
// (a Discord relay thread): people outside the account are speaking, so only an external memory
// may be archived there, never one of the owner's own. A refusal is reported to the model as an
// error result, not returned as a Go error, like the other tools.
func (t *ArchiveMemoryTool) Archive(ctx context.Context, chat *models.Chat, external bool, args []byte) (string, error) {
	fail := func(msg string) (string, error) {
		return marshalToolResult(archiveMemoryResult{Error: msg}, archiveMemoryToolName)
	}

	// A sandbox may not change the owner's memory. The tool is not offered there; this refuses a
	// call the model makes anyway.
	if chat.IsSandboxed() {
		return fail(sandboxedNote("Archiving memories"))
	}

	var a archiveMemoryArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return fail(fmt.Sprintf("invalid arguments: %v", err))
	}
	token := normalizeMemoryTarget(a.MemoryID)
	if token == "" {
		return fail("memory_id is required: pass the full id find_context shows (memory:<uuid>)")
	}
	id, err := uuid.Parse(token)
	if err != nil {
		return fail(fmt.Sprintf("%q is not a full memory id; pass the whole memory:<uuid> from find_context, not a prefix or a description", a.MemoryID))
	}

	mem, err := t.store.GetMemory(ctx, chat.UserID, id)
	switch {
	case errors.Is(err, datastore.ErrMemoryNotFound):
		return fail(fmt.Sprintf("memory %s not found", id))
	case err != nil:
		t.logger.Error("archive_memory lookup failed", zap.String("user_id", chat.UserID.String()), zap.Error(err))
		return fail("could not look up the memory; try again")
	}
	if why := memoryNotArchivableHere(chat, external, mem); why != "" {
		return fail(fmt.Sprintf("memory %s cannot be archived here: %s", id, why))
	}

	archival := models.MemoryArchival{ChatID: &chat.ID, Reason: a.Reason}
	if chat.PersonalityID != uuid.Nil {
		pid := chat.PersonalityID
		archival.PersonalityID = &pid
	}
	event, err := t.store.ArchiveMemory(ctx, chat.UserID, id, archival)
	switch {
	case errors.Is(err, datastore.ErrMemoryNotArchivable):
		return fail(fmt.Sprintf("memory %s cannot be archived: %s", id, strings.TrimPrefix(err.Error(), datastore.ErrMemoryNotArchivable.Error()+": ")))
	case errors.Is(err, datastore.ErrMemoryNotFound):
		return fail(fmt.Sprintf("memory %s not found", id))
	case err != nil:
		t.logger.Error("archive_memory failed", zap.String("user_id", chat.UserID.String()), zap.String("memory_id", id.String()), zap.Error(err))
		return fail("could not archive the memory; try again")
	}

	return marshalToolResult(archiveMemoryResult{
		Success:  true,
		MemoryID: memorySourceLabel(id),
		Content:  mem.Content,
		EventID:  event.ID.String(),
		Note:     "Archived at " + event.CreatedAt.UTC().Format(time.RFC3339) + ": it is out of active memory but not deleted, and the user can restore it from Memories (the Archived view, or Undo in Merge history).",
	}, archiveMemoryToolName)
}

// memoryNotArchivableHere is why this conversation may not archive mem, or "" when it may. A
// conversation archives only what its own memory retrieval reads (datastore.relatedMemoryReadable):
// its own thread memories, and User-scoped memories that are unpinned or pinned to its
// personality. On an external turn only an external memory qualifies. The memory's own state
// (starred, already inactive, a summary) is the datastore's check.
func memoryNotArchivableHere(chat *models.Chat, external bool, mem *models.Memory) string {
	if external && !mem.Provenance.IsExternal() {
		return "in a relayed thread only memories learned from people in it can be archived; this one is the user's own"
	}
	switch memoryScopeOf(mem) {
	case MemoryScopeChat:
		if mem.ChatID != chat.ID {
			return "it belongs to another conversation; the user can delete it in Memories"
		}
	case MemoryScopeUser:
		if mem.PinnedPersonalityID != nil && *mem.PinnedPersonalityID != chat.PersonalityID {
			return "it is pinned to another personality; the user can delete it in Memories"
		}
	}
	return ""
}
