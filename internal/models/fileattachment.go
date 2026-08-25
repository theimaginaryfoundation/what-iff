package models

import (
	"time"

	"github.com/google/uuid"
)

// ImageMIMEPrefix is the MIME type prefix for all image types (image/png, image/jpeg, …).
// Use this constant instead of a bare "image/" literal to prevent drift.
const ImageMIMEPrefix = "image/"

// FileAttachment represents a file that has been uploaded and attached to the system
type FileAttachment struct {
	ID       uuid.UUID `json:"id"`
	UserID   uuid.UUID `json:"user_id"`
	FileID   *string   `json:"file_id,omitempty"`
	Name     string    `json:"name"`
	FileType string    `json:"file_type"`
	// Description is optional user-provided context for gallery metadata.
	Description *string `json:"description,omitempty"`
	// FileContent is ephemeral base64 image data used only while forwarding a tool result
	// into a model vision request. Persisted attachment bytes live exclusively in S3.
	FileContent string `json:"-"`
	// S3Key is the canonical object key for the full-resolution file. Set at
	// upload time so that renaming the display Name never breaks retrieval.
	S3Key         string     `json:"s3_key,omitempty"`
	ChatMessageID *uuid.UUID `json:"chat_message_id,omitempty"`
	// ChatID is populated when the attachment is fetched with the chat edge
	// loaded (GetFileAttachment). Used by the gallery to fall back to
	// FileKeyForChat for pre-migration user-uploaded images.
	ChatID        *uuid.UUID `json:"chat_id,omitempty"`
	PersonalityID *uuid.UUID `json:"personality_id,omitempty"`
	// Personalities captures all personalities associated with this image, including
	// direct file attachment ownership and expression-image associations.
	Personalities []FileAttachmentPersonalityRef `json:"personalities,omitempty"`
	CreatedAt     time.Time                      `json:"created_at"`
}

// FileAttachmentPersonalityRef is a lightweight personality reference included
// in file attachment payloads for gallery metadata and filtering.
type FileAttachmentPersonalityRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// FileAttachmentFilters defines filters for listing file attachments
type FileAttachmentFilters struct {
	Name          *string    `json:"name,omitempty"`
	FileType      *string    `json:"file_type,omitempty"`
	ChatMessageID *uuid.UUID `json:"chat_message_id,omitempty"`
	PersonalityID *uuid.UUID `json:"personality_id,omitempty"`
	GlobalOnly    *bool      `json:"global_only,omitempty"`
	// DocsOnly narrows the PersonalityID filter to files with a direct
	// personality_id FK (RAG doc files uploaded by the user). When nil or
	// false the default union behaviour is preserved: results include both
	// direct doc attachments and expression images linked via
	// PersonalityExpression. Use DocsOnly=true when you only want to count or
	// manage user-uploaded documents and must not penalise expression images
	// against upload-slot limits.
	DocsOnly *bool      `json:"docs_only,omitempty"`
	MinDate  *time.Time `json:"min_date,omitempty"`
	MaxDate  *time.Time `json:"max_date,omitempty"`
}
