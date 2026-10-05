package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// A chat whose memory sensitivity limit is below "sensitive" is a restricted sandbox (see
// models.MemorySensitivity). A thread's sensitivity level governs which DATA comes in; its
// disabled_tools govern which ACTIONS the agent can take. Every tool that can read the user's
// account data asks this file, not the chat's raw limit, so the rules live in one place:
//
//   - memories are read at or below the chat's limit (SQL filters in the datastore;
//     memoryReadableBy covers lookups by ID, where another conversation's Chat memory or checkpoint
//     summary also needs that conversation to be readable);
//   - a thread's limit is also its own classification: another conversation (its messages,
//     bookmarks, a memory's origin) is readable only when that conversation's own limit is at or
//     below this chat's (a public thread reads other public threads), failing closed on lookup
//     errors and unknown limits;
//   - files stay limited to this conversation's and its personality's;
//   - the personality-wide scratchpad is not readable or writable, and account-wide listings
//     (files, jobs, skills, personalities) are not available.
//
// Tools build their refusal text with restrictedNote so the model gets one consistent message.

// restrictedNote is the refusal text for a capability a restricted chat does not have.
func restrictedNote(chat *models.Chat, what string) string {
	return fmt.Sprintf("%s is not available in this restricted conversation (memory sensitivity limit: %s). "+
		"It can use memories up to that level, this conversation and other conversations limited to that level or below, and this conversation's own files.",
		what, chat.MemoryLimit())
}

// memoryReadableBy reports whether chat may read m, looked up by id. An unrestricted chat reads
// everything it owns. A restricted chat additionally needs m's sensitivity at or below its limit
// and, mirroring what retrieval (GetRelatedMemories) would ever hand it:
//
//   - a Chat-scoped memory only from this conversation, or from one it may read
//     (conversationReadable);
//   - a User-scoped memory only when it is not pinned to another personality;
//   - a checkpoint summary only from this conversation, or from one it may read: a summary is that
//     conversation's content, so it follows the conversation's classification as well as its
//     own level.
//
// Anything else (an unknown scope) is refused.
func memoryReadableBy(ctx context.Context, store chatLimitLookup, chat *models.Chat, m *models.Memory) bool {
	if m == nil {
		return false
	}
	if !chat.MemoryRestricted() {
		return true
	}
	if !m.Sensitivity.AllowedUnder(chat.MemoryLimit()) {
		return false
	}
	switch memoryScopeOf(m) {
	case MemoryScopeUser:
		return m.PinnedPersonalityID == nil || (chat.PersonalityID != uuid.Nil && *m.PinnedPersonalityID == chat.PersonalityID)
	case MemoryScopeChat, memoryScopeSummary:
		return m.ChatID == chat.ID || conversationReadable(ctx, store, chat, m.ChatID)
	default:
		return false
	}
}

// memoryScopeSummary is the checkpoint-summary memory scope.
const memoryScopeSummary = "Summary"

// memoryScopeOf is m's scope ("User", "Chat" or "Summary"), from Scope when the loader set it and
// from Level otherwise.
func memoryScopeOf(m *models.Memory) string {
	switch m.Scope {
	case MemoryScopeUser, MemoryScopeChat, memoryScopeSummary:
		return m.Scope
	}
	switch m.Level {
	case models.MemoryLevelGlobal, models.MemoryLevelPersonality:
		return MemoryScopeUser
	case models.MemoryLevelThread:
		return MemoryScopeChat
	case models.MemoryLevelSummary:
		return memoryScopeSummary
	}
	return ""
}

// chatLimitLookup returns the stored memory sensitivity limit of one of the user's chats.
// *datastore.Datastore implements it (GetChatMemorySensitivityLimit).
type chatLimitLookup interface {
	GetChatMemorySensitivityLimit(ctx context.Context, userID, chatID uuid.UUID) (models.MemorySensitivity, error)
}

// conversationReadable reports whether chat may read conversationID (its messages, bookmarks, or
// the source turns of a memory). An unrestricted chat and the chat itself always may. A
// restricted chat may read another conversation only when that conversation's own limit is at or
// below its own (models.ConversationReadableUnder). Any lookup failure (missing store, unknown or
// foreign chat, database error) refuses.
func conversationReadable(ctx context.Context, store chatLimitLookup, chat *models.Chat, conversationID uuid.UUID) bool {
	if !chat.MemoryRestricted() || conversationID == chat.ID {
		return true
	}
	if store == nil || conversationID == uuid.Nil {
		return false
	}
	other, err := store.GetChatMemorySensitivityLimit(ctx, chat.UserID, conversationID)
	if err != nil {
		return false
	}
	return models.ConversationReadableUnder(other, chat.MemoryLimit())
}

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

// cappedMemorySensitivity is the level a memory created from chat gets: the requested
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
