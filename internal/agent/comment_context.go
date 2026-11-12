package agent

import (
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Document comments sent to the agent (ADR 0x046). A user who mentions the agent in a comment on
// a file sends it a message: the comment's text is the message, and what it is about (the file,
// what the thread is on, the thread so far) is context the message carries but the chat does not
// show. That context belongs to its message, so it is rendered in the message's own turn, after
// the text, rather than merged into the context of the conversation as a whole.

// sentWithMessage is the context a message was sent with (its document comment), which the
// after-inference rewrite of the message's context keeps.
func sentWithMessage(items []models.AdditionalContextItem) []models.AdditionalContextItem {
	var out []models.AdditionalContextItem
	for _, it := range items {
		if it.Type == models.AdditionalContextTypeFileComment {
			out = append(out, it)
		}
	}
	return out
}

// withFileCommentContext is a user message's turn: its text, then the document comment it was
// sent from, when it was.
func withFileCommentContext(body string, msg *models.ChatMessage) string {
	if msg == nil {
		return body
	}
	var parts []string
	for _, it := range msg.AdditionalContext {
		if it.Type == models.AdditionalContextTypeFileComment && strings.TrimSpace(it.Content) != "" {
			parts = append(parts, strings.TrimSpace(it.Content))
		}
	}
	if len(parts) == 0 {
		return body
	}
	return body + "\n\n" + strings.Join(parts, "\n\n")
}
