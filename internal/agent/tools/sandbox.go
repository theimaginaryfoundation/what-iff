package tools

import (
	"fmt"
	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// A sandboxed chat (models.Chat.Sandboxed) cannot read anything outside itself, and has a
// locked-down tool set. Every tool that can read the user's account data asks this file, not the
// chat's raw flag, so the rules live in one place:
//
//   - memories: only those created in this conversation (a Chat-scoped memory or its own
//     checkpoint summary); the owner's User-scoped memories and other conversations' memories are
//     not readable (memoryReadableBy, and the SQL scoping in the datastore for searches);
//   - conversations: only this one (conversationReadable);
//   - files: only the files uploaded to this conversation (fileInChatScope), not the personality's
//     documents or the account's library;
//   - the personality-wide scratchpad is not readable or writable, and account-wide listings
//     (jobs, skills, personalities, other conversations) are not available;
//   - memories it creates are always Chat-scoped, so nothing it learns reaches the account.
//
// Tools build their refusal text with sandboxedNote so the model gets one consistent message.

// sandboxedNote is the refusal text for a capability a sandboxed chat does not have.
func sandboxedNote(what string) string {
	return fmt.Sprintf("%s is not available in this sandboxed conversation. "+
		"It can use only this conversation, the memories created in it and the files uploaded to it.", what)
}

// memoryReadableBy reports whether chat may read m, looked up by id. A chat that is not sandboxed
// reads everything it owns. A sandboxed chat reads only a Chat-scoped memory or checkpoint summary
// of this conversation; anything else (a User-scoped memory, another conversation's memory or
// summary, an unknown scope) is refused.
func memoryReadableBy(chat *models.Chat, m *models.Memory) bool {
	if m == nil {
		return false
	}
	if !chat.IsSandboxed() {
		return true
	}
	switch memoryScopeOf(m) {
	case MemoryScopeChat, memoryScopeSummary:
		return m.ChatID == chat.ID
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

// sandboxChatID is the one conversation a sandboxed chat may search the summaries of (its own), or
// uuid.Nil when the chat is not sandboxed and may search all of them.
func sandboxChatID(chat *models.Chat) uuid.UUID {
	if chat.IsSandboxed() {
		return chat.ID
	}
	return uuid.Nil
}

// conversationReadable reports whether chat may read conversationID (its messages, bookmarks, or
// the source turns of a memory). A chat that is not sandboxed always may; a sandboxed chat may read
// only itself.
func conversationReadable(chat *models.Chat, conversationID uuid.UUID) bool {
	return !chat.IsSandboxed() || conversationID == chat.ID
}

// fileInChatScope reports whether fa is within what a sandboxed chat may read: a file uploaded to
// this conversation. Chats that are not sandboxed are not limited here.
func fileInChatScope(chat *models.Chat, fa *models.FileAttachment) bool {
	if fa == nil {
		return false
	}
	if !chat.IsSandboxed() {
		return true
	}
	return fa.ChatID != nil && *fa.ChatID == chat.ID
}
