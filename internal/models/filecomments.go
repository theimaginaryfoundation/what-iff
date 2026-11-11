package models

import "time"

// Comments on text and code files (ADR 0x045), kept beside the file (ent FileCommentSet). The
// shapes match the comment panel's: a thread is its first comment and its replies, on a range of
// the file's text or, once all of that text is gone, on the file as a whole.

// FileCommentRange is [Start, End) of the file's text in UTF-16 code units.
type FileCommentRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// FileCommentMention is someone a comment mentions: their `@Name` is written into its text.
type FileCommentMention struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FileComment is one comment of a thread.
type FileComment struct {
	ID         string               `json:"id"`
	AuthorID   string               `json:"author_id"`
	AuthorName string               `json:"author_name"`
	CreatedAt  time.Time            `json:"created_at"`
	Text       string               `json:"text"`
	Mentions   []FileCommentMention `json:"mentions,omitempty"`
}

// FileCommentThread is a thread on a range of the file, or on the whole file when Anchor is nil.
type FileCommentThread struct {
	ID       string            `json:"id"`
	Anchor   *FileCommentRange `json:"anchor"`
	Quote    string            `json:"quote"`
	Resolved bool              `json:"resolved"`
	Comments []FileComment     `json:"comments"`
}

// FileComments is a file's comment threads, placed on the text of ContentVersion.
type FileComments struct {
	Threads        []FileCommentThread `json:"threads"`
	ContentVersion int                 `json:"content_version"`
	// Revision is what a write sends back: a write against another revision is refused.
	Revision int `json:"revision"`
}

// FileCommentSet is the stored set: the threads and the text their anchors are placed against.
type FileCommentSet struct {
	Threads         []FileCommentThread
	BaselineText    string
	BaselineVersion int
	Revision        int
}
