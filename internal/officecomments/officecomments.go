// Package officecomments reads and writes the comments inside Word, PowerPoint and Excel files on
// the server (ADR 0x046), so an agent can take part in them as the browser's editors do. Each
// format (subpackages docx, pptx, xlsx) works on the file's bytes and writes exactly what the
// browser writes (ADR 0x045 §2), so a comment made on either side opens on the other and in Office,
// and leaves every other part of the package as it was.
//
// People are named as in the comments panel: "user:<id>" for a WhatIff user, "agent:<id>" for an
// agent, "author:<name>" for anyone else the file names. In the file a WhatIff user or agent is a
// person with the provider WhatIffProvider and, as its user id, the user's id or the agent's
// whole "agent:<id>".
package officecomments

import (
	"errors"
	"strings"
	"time"
)

// WhatIffProvider is the presence provider the files name WhatIff's users and agents by.
const WhatIffProvider = "WhatIff"

var (
	// ErrNoThread means no thread has the id given.
	ErrNoThread = errors.New("no thread with that id")
	// ErrQuoteNotFound means the quoted text is not where a new thread was asked to go.
	ErrQuoteNotFound = errors.New("the quoted text is not in the file")
	// ErrQuoteAmbiguous means the quoted text is there more than once and no occurrence says which.
	ErrQuoteAmbiguous = errors.New("the quoted text is in the file more than once")
	// ErrBadAnchor means the anchor does not name a place in the file (a slide or cell that is not
	// there, or an anchor of the wrong kind for the format).
	ErrBadAnchor = errors.New("that place is not in the file")
	// ErrNotOffice means the bytes are not a package of the format.
	ErrNotOffice = errors.New("the file is not a valid Office package")
)

// Comment is one comment of a thread.
type Comment struct {
	// ID is the comment's id as the browser's editor gives it (a thread's first comment has the
	// thread's id).
	ID         string
	AuthorID   string
	AuthorName string
	CreatedAt  time.Time
	Text       string
}

// Thread is a thread as the comments panel shows it.
type Thread struct {
	// ID is the thread's id as the browser's editor gives it, so a link to a thread made on one
	// side opens it on the other.
	ID string
	// On is what the thread is on, in words, as the panel labels it: the commented text (Word, a
	// slide's text), the shape's name or "Slide n" (PowerPoint), "Sheet1!B3" (Excel).
	On string
	// Where is where that is, for an agent to find it: "paragraph 4", "slide 2", "sheet Sheet1, cell B3".
	Where    string
	Resolved bool
	Comments []Comment
}

// Anchor is where a new thread goes.
type Anchor struct {
	// Quote is the exact text the thread is on (Word: in the body; PowerPoint: in a shape's text).
	// Occurrence picks one when it appears more than once (1 = first).
	Quote      string
	Occurrence int
	// Slide (PowerPoint, 1-based) limits Quote to one slide, or with no Quote puts the thread on
	// the slide itself.
	Slide int
	// Sheet and Cell (Excel) are the cell, e.g. "Sheet1" and "B3"; an empty Sheet is the first.
	Sheet string
	Cell  string
}

// Format reads and changes the comments of one file format. Every change returns the whole new
// file; nothing else in it changes.
type Format interface {
	List(data []byte) ([]Thread, error)
	// Add starts a thread at the anchor with its first comment and returns its id.
	Add(data []byte, at Anchor, first Comment) (out []byte, threadID string, err error)
	Reply(data []byte, threadID string, c Comment) ([]byte, error)
	Resolve(data []byte, threadID string, resolved bool) ([]byte, error)
}

// ProviderUserID is the user id a file gives a WhatIff person ("user:<id>" → "<id>",
// "agent:<id>" → "agent:<id>"), or "" for someone the file only names.
func ProviderUserID(personID string) string {
	switch {
	case strings.HasPrefix(personID, "user:"):
		return strings.TrimPrefix(personID, "user:")
	case strings.HasPrefix(personID, "agent:"):
		return personID
	}
	return ""
}

// PersonID is the panel's id for a file's author: by the WhatIff user id when the provider is
// WhatIff, else by name.
func PersonID(providerID, userID, name string) string {
	if providerID == WhatIffProvider && userID != "" {
		if strings.HasPrefix(userID, "agent:") {
			return userID
		}
		return "user:" + userID
	}
	return "author:" + name
}
