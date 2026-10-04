package tools

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// A chat whose memory sensitivity limit is below "sensitive" is a restricted sandbox (see
// models.MemorySensitivity). Every tool that can read the user's account data asks this file, not
// the chat's raw limit, so the rules live in one place:
//
//   - memories and entities are read at or below the chat's limit (SQL filters in the datastore;
//     the single-row checks here cover lookups by ID);
//   - other conversations (their messages, summaries, bookmarks, a memory's origin) are not
//     readable, only the current one;
//   - files stay limited to this conversation's and its personality's;
//   - the personality-wide scratchpad and agent/ notebook are not readable or writable, and
//     account-wide listings (conversations, files, jobs) are not available.
//
// Tools build their refusal text with restrictedNote so the model gets one consistent message.

// restrictedNote is the refusal text for a capability a restricted chat does not have.
func restrictedNote(chat *models.Chat, what string) string {
	return fmt.Sprintf("%s is not available in this restricted conversation (memory sensitivity limit: %s). "+
		"It can use memories and entities up to that level, this conversation, and this conversation's own files and chat/ workspace.",
		what, chat.MemoryLimit())
}

// memoryReadableBy reports whether chat may read m. Besides the sensitivity limit, a restricted
// chat cannot read the checkpoint summary of another conversation.
func memoryReadableBy(chat *models.Chat, m *models.Memory) bool {
	if m == nil {
		return false
	}
	if !chat.MemoryRestricted() {
		return true
	}
	if !m.Sensitivity.AllowedUnder(chat.MemoryLimit()) {
		return false
	}
	if m.Level == models.MemoryLevelSummary && m.ChatID != chat.ID {
		return false
	}
	return true
}

// otherConversationBlocked reports whether reading conversationID is refused: a restricted chat
// may only read its own conversation.
func otherConversationBlocked(chat *models.Chat, conversationID uuid.UUID) bool {
	return chat.MemoryRestricted() && conversationID != chat.ID
}

// restrictedMemoryScopeNote is returned when a restricted chat asked for a User-scoped memory.
const restrictedMemoryScopeNote = "This is a restricted conversation, so the memory was kept to this conversation (Chat scope) instead of the whole account."

// agentMemorySensitivity parses the sensitivity an agent tool passed. Only personal and sensitive
// are allowed (empty means the default, personal): an agent may never mark a memory public, which
// is assignable only through the memory manager. ok is false for any other value.
func agentMemorySensitivity(raw string) (models.MemorySensitivity, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", true
	}
	s, valid := models.ParseMemorySensitivity(raw)
	if !valid || s == models.MemorySensitivityPublic {
		return "", false
	}
	return s, true
}

// cappedMemorySensitivity is the level a memory or entity created from chat gets: the requested
// level (empty means the personal default) passed through models.CapToLimit. A restricted chat's
// default level is lowered to its limit so what it writes stays readable there; an explicit
// sensitive is never lowered.
func cappedMemorySensitivity(chat *models.Chat, requested models.MemorySensitivity) models.MemorySensitivity {
	return models.CapToLimit(requested, chat.MemoryLimit())
}

// fileInChatScope reports whether fa is within what a restricted chat may read: an upload on this
// conversation or a document attached to its personality. Unrestricted chats are not limited here.
func fileInChatScope(chat *models.Chat, fa *models.FileAttachment) bool {
	if fa == nil {
		return false
	}
	if !chat.MemoryRestricted() {
		return true
	}
	if fa.ChatID != nil && *fa.ChatID == chat.ID {
		return true
	}
	return chat.PersonalityID != uuid.Nil && fa.PersonalityID != nil && *fa.PersonalityID == chat.PersonalityID
}
