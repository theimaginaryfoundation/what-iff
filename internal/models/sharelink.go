package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ShareLink is a public link to one of the user's files, as its owner sees it (ADR 0x051).
type ShareLink struct {
	// Key is the link's own part: the share site's /s/<key>.
	Key    string    `json:"key"`
	FileID uuid.UUID `json:"file_id"`
	// FileName and Folder are the shared file's now.
	FileName string `json:"file_name"`
	Folder   string `json:"folder,omitempty"`
	FileType string `json:"file_type"`
	// WebApp is true when the link shares the page's folder as a web app (an HTML file in a
	// folder); Folder is then the project's root.
	WebApp           bool      `json:"web_app"`
	PasscodeRequired bool      `json:"passcode_required"`
	Passcode         string    `json:"passcode,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// SharedItem is what a share link opens, found by its key (ADR 0x051 §5): never shown as it is to
// a visitor, who sees only what the share site needs.
type SharedItem struct {
	Key     string
	OwnerID uuid.UUID
	File    *FileAttachment
	// PasscodeCiphertext is the encrypted passcode; empty when the link needs none.
	PasscodeCiphertext string
	PasscodeGeneration int
}

// ErrShareLinkNotFound is returned when a file has no share link, or a key names none.
var ErrShareLinkNotFound = errors.New("share link not found")

// ErrShareNotShareable is returned for a file that cannot be shared (a skill's file).
var ErrShareNotShareable = errors.New("this file cannot be shared")

// SharesAsWebApp reports whether a file is shared as a web app: a web page (IsHTMLFile) in a
// gallery folder (ADR 0x051 §1). A page at the top level is shared as a single file.
func SharesAsWebApp(f *FileAttachment) bool {
	return f != nil && f.Folder != "" && IsHTMLFile(f.Name, f.FileType)
}
