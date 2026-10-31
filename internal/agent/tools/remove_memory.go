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

const removeMemoryToolName = "remove_memory"

// RemoveMemoryDescription is the agent-facing description.
const RemoveMemoryDescription = `Remove one memory that is wrong or out of date from active memory, so neither you nor the user's other conversations recall it again.

Use it to correct memory: when the user tells you a memory is wrong, or something they said replaces it ("I moved to Lisbon" makes "Lives in Berlin" stale). Find the memory first with find_context; its sources and results name each memory as memory:<uuid>. If the corrected fact is worth keeping, save it with create_memory as well.

Notes:
- memory_id must be the full id of one memory (memory:<uuid> or the bare uuid). A short prefix is refused: removal never guesses.
- Only memories this conversation can recall are removable: its own thread memories, and the user's global memories that are unpinned or pinned to this personality. Starred memories and conversation summaries cannot be removed; the user manages those in Memories.
- reason (optional, short) is kept with the removal so the user can see why.
- The memory is not destroyed. The user sees the removal in Memories > Merge history and can undo it there.`

// RemoveMemoryToolSpec is the shared function-tool shape for remove_memory.
var RemoveMemoryToolSpec = FunctionToolSpec{
	Name:        removeMemoryToolName,
	Description: RemoveMemoryDescription,
	Properties: map[string]interface{}{
		"memory_id": map[string]interface{}{
			"type":        "string",
			"description": "The full id of the memory to remove, as find_context shows it: memory:<uuid> or the bare uuid.",
		},
		"reason": map[string]interface{}{
			"type":        "string",
			"description": fmt.Sprintf("Why it is being removed, in a few words (e.g. 'user moved to Lisbon'). At most %d characters.", models.MaxMemoryRemovalReasonRunes),
		},
	},
	Required: []string{"memory_id"},
}

// removeMemoryStore is the narrow datastore surface remove_memory needs. *datastore.Datastore
// satisfies it.
type removeMemoryStore interface {
	GetMemory(ctx context.Context, userID, id uuid.UUID) (*models.Memory, error)
	RemoveMemory(ctx context.Context, userID, memoryID uuid.UUID, removal models.MemoryRemoval) (*models.MemoryMergeEvent, error)
}

// RemoveMemoryTool implements the remove_memory agent tool (ADR 0x036).
type RemoveMemoryTool struct {
	store  removeMemoryStore
	logger *zap.Logger
}

// NewRemoveMemoryTool constructs a RemoveMemoryTool backed by the datastore.
func NewRemoveMemoryTool(ds *datastore.Datastore, logger *zap.Logger) *RemoveMemoryTool {
	return &RemoveMemoryTool{store: ds, logger: logger}
}

type removeMemoryArgs struct {
	MemoryID string `json:"memory_id"`
	Reason   string `json:"reason"`
}

type removeMemoryResult struct {
	Success  bool   `json:"success"`
	MemoryID string `json:"memory_id,omitempty"`
	Content  string `json:"content,omitempty"`
	// EventID is the remove event in the memory lifecycle log (find_context lifecycle_events).
	EventID string `json:"event_id,omitempty"`
	Note    string `json:"note,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Remove executes a remove_memory call. external is true on a turn whose memories are external
// (a Discord relay thread): people outside the account are speaking, so only an external memory
// may be removed there, never one of the owner's own. A refusal is reported to the model as an
// error result, not returned as a Go error, like the other tools.
func (t *RemoveMemoryTool) Remove(ctx context.Context, chat *models.Chat, external bool, args []byte) (string, error) {
	fail := func(msg string) (string, error) {
		return marshalToolResult(removeMemoryResult{Error: msg}, removeMemoryToolName)
	}

	// A sandbox may not change the owner's memory. The tool is not offered there; this refuses a
	// call the model makes anyway.
	if chat.IsSandboxed() {
		return fail(sandboxedNote("Removing memories"))
	}

	var a removeMemoryArgs
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
		t.logger.Error("remove_memory lookup failed", zap.String("user_id", chat.UserID.String()), zap.Error(err))
		return fail("could not look up the memory; try again")
	}
	if why := memoryNotRemovableHere(chat, external, mem); why != "" {
		return fail(fmt.Sprintf("memory %s cannot be removed here: %s", id, why))
	}

	removal := models.MemoryRemoval{ChatID: &chat.ID, Reason: a.Reason}
	if chat.PersonalityID != uuid.Nil {
		pid := chat.PersonalityID
		removal.PersonalityID = &pid
	}
	event, err := t.store.RemoveMemory(ctx, chat.UserID, id, removal)
	switch {
	case errors.Is(err, datastore.ErrMemoryNotRemovable):
		return fail(fmt.Sprintf("memory %s cannot be removed: %s", id, strings.TrimPrefix(err.Error(), datastore.ErrMemoryNotRemovable.Error()+": ")))
	case errors.Is(err, datastore.ErrMemoryNotFound):
		return fail(fmt.Sprintf("memory %s not found", id))
	case err != nil:
		t.logger.Error("remove_memory failed", zap.String("user_id", chat.UserID.String()), zap.String("memory_id", id.String()), zap.Error(err))
		return fail("could not remove the memory; try again")
	}

	return marshalToolResult(removeMemoryResult{
		Success:  true,
		MemoryID: memorySourceLabel(id),
		Content:  mem.Content,
		EventID:  event.ID.String(),
		Note:     "Removed from active memory at " + event.CreatedAt.UTC().Format(time.RFC3339) + ". The user can undo it in Memories > Merge history.",
	}, removeMemoryToolName)
}

// memoryNotRemovableHere is why this conversation may not remove mem, or "" when it may. A
// conversation removes only what its own memory retrieval reads (datastore.relatedMemoryReadable):
// its own thread memories, and User-scoped memories that are unpinned or pinned to its
// personality. On an external turn only an external memory qualifies. The memory's own state
// (starred, already removed, a summary) is the datastore's check.
func memoryNotRemovableHere(chat *models.Chat, external bool, mem *models.Memory) string {
	if external && !mem.Provenance.IsExternal() {
		return "in a relayed thread only memories learned from people in it can be removed; this one is the user's own"
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
