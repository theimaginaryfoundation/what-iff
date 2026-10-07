package agent

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Memory provenance (models.MemoryProvenance). A Discord relay thread is a conversation with
// people outside the account: whoever the binding lets tag the bot. What the agent learns
// there is stored as external, so it is shown to the model as unverified and attributed to its
// speaker wherever it is read later (memoryutil.FormatMemoryForContext), and the owner can review
// it in the memory manager. Scope is chosen exactly as in any chat (a sandboxed relay thread always
// writes Chat scope); provenance only records where the memory came from.
//
// Every write path of a turn takes its provenance from chatContext.memoryProvenance:
// create_memory (createMemoryOrigin), checkpoint extraction and its merges and links
// (compactMemoriesFromCheckpoint). A fold's survivor becomes external when any member is
// (datastore.WithNewMemberOrigin, models.MergeOrigins).

// memoryProvenanceForChat is the provenance memories written from chat get: external for a
// Discord relay thread (datastore.IsExternalRelayChat, sandboxed or not), user otherwise. A
// lookup failure in a sandboxed chat reads as external: the safe mistake is to mark the owner's
// own words unverified, not to pass a stranger's words off as the owner's. (An ordinary chat
// keeps user on a failure, so a database hiccup does not relabel the owner's ordinary chats.)
func (a *Agent) memoryProvenanceForChat(ctx context.Context, userID uuid.UUID, chat *models.Chat) models.MemoryProvenance {
	if chat == nil || chat.ID == uuid.Nil || a == nil || a.ds == nil {
		return models.MemoryProvenanceUser
	}
	external, err := a.ds.IsExternalRelayChat(ctx, userID, chat.ID)
	if err != nil {
		if !chat.IsSandboxed() {
			return models.MemoryProvenanceUser
		}
		if a.logger != nil {
			a.logger.Warn("memory provenance lookup failed; treating the sandboxed chat's memories as external",
				zap.String("chat_id", chat.ID.String()), zap.Error(err))
		}
		return models.MemoryProvenanceExternal
	}
	if external {
		return models.MemoryProvenanceExternal
	}
	return models.MemoryProvenanceUser
}

// createMemoryOrigin is the origin create_memory writes with this turn: the chat's provenance and,
// for an external memory, the Discord author of the message the turn answers (the tool's speaker
// argument overrides it). The author is read when the tool runs, since the relay records it just
// after the turn starts.
func (a *Agent) createMemoryOrigin(ctx context.Context, chatCtx *chatContext) models.MemoryOrigin {
	origin := models.MemoryOrigin{Provenance: models.MemoryProvenanceUser}
	if chatCtx == nil {
		return origin
	}
	origin.Provenance = chatCtx.memoryProvenance.OrDefault()
	if !origin.External() || chatCtx.triggerMessageID == uuid.Nil || a.ds == nil {
		return origin
	}
	speaker, err := a.ds.ExternalSpeakerForMessage(ctx, chatCtx.userID, chatCtx.triggerMessageID)
	if err != nil {
		if a.logger == nil {
			return origin
		}
		a.logger.Warn("could not read the Discord author of the triggering message",
			zap.String("chat_message_id", chatCtx.triggerMessageID.String()), zap.Error(err))
		return origin
	}
	origin.Speaker = speaker
	return origin
}

// externalMemoryChat reports whether this turn's memories are external.
func (c *chatContext) externalMemoryChat() bool {
	return c != nil && c.memoryProvenance.IsExternal()
}

// stampExtractedProvenance gives a checkpoint's extracted memories their provenance: every one is
// external in a Discord relay thread; elsewhere none is, and a speaker means nothing.
func stampExtractedProvenance(collapsed []memoryutil.CollapsedExtractedMemory, external bool) []memoryutil.CollapsedExtractedMemory {
	for i := range collapsed {
		if external {
			collapsed[i].Provenance = models.MemoryProvenanceExternal
			continue
		}
		collapsed[i].Provenance = models.MemoryProvenanceUser
		collapsed[i].Speaker = ""
	}
	return collapsed
}

// relayMemoryExtractionNote is added to the extraction instructions in a Discord relay thread,
// where the extraction uses the speaker schema variant (models.ExtractedRelayMemory).
const relayMemoryExtractionNote = `This conversation is a Discord channel relayed into this thread. The people writing in it are Discord users, each labelled by display name at the start of their message ("name (Discord, #channel): text"). They are NOT the account owner ("the user" in the instructions above means the person who said it, not the owner), and what they say is unverified.
- Attribute each memory to the person it came from: set speaker to their display name exactly as labelled before "(Discord". Use an empty speaker when it is unclear who said it, or when the memory is your own observation.
- Write each memory about the person it concerns, by name (for example "alice is learning Rust"), never as a fact about the account owner.`

// memoryExtractionDeveloperNote is the extraction developer message for this turn: the
// scratchpad-aware or sandboxed variant, plus the relay note in a Discord relay thread.
func memoryExtractionDeveloperNote(chatCtx *chatContext) string {
	msg := memoryExtractionDeveloperMessageFor(chatCtx)
	if chatCtx.externalMemoryChat() {
		msg += "\n\n" + relayMemoryExtractionNote
	}
	return msg
}

var relayMemoryExtractionSchema = provider.GenerateSchema[models.ExtractedRelayMemoryResponse]()

// memoryExtractionSchemaFor picks the extraction schema: the speaker variant in a Discord
// relay thread, the default otherwise.
func memoryExtractionSchemaFor(chatCtx *chatContext) map[string]interface{} {
	if chatCtx.externalMemoryChat() {
		return relayMemoryExtractionSchema
	}
	return memoryExtractionSchema
}

// decodeExtractedMemories parses an extraction response in the schema memoryExtractionSchemaFor
// chose. The speaker is kept only in a relay thread.
func decodeExtractedMemories(chatCtx *chatContext, raw []byte) ([]models.ExtractedMemory, error) {
	if chatCtx.externalMemoryChat() {
		var relay models.ExtractedRelayMemoryResponse
		if err := json.Unmarshal(raw, &relay); err != nil {
			return nil, err
		}
		return relay.ToExtracted(), nil
	}
	var out models.ExtractedMemoryResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Memories, nil
}
