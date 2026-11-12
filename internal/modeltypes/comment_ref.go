package modeltypes

import "github.com/google/uuid"

// CommentRef is a document comment a chat message is about (ADR 0x046): on a user's message, the
// comment that mentioned the agent and sent it; on the agent's reply, a comment it made or replied
// to. The chat shows it as a link that opens the file at the thread.
type CommentRef struct {
	FileID   uuid.UUID `json:"file_id"`
	FileName string    `json:"file_name"`
	ThreadID string    `json:"thread_id"`
	// CommentID is the comment within the thread (the thread's own id for its first comment).
	CommentID string `json:"comment_id,omitempty"`
	// Quote is what the thread is on, as the comments panel shows it; empty for the whole file.
	Quote string `json:"quote,omitempty"`
}
