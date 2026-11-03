package tools

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/filechunker"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// Addressing and reading the user's files by id or folder/name path, shared by the agent file
// tools (ADR 0x034) and the tools that hand a file on elsewhere (the private build's
// load_file_to_container). Refusals are FileRefusal errors whose text is meant for the model.

// fileTextMaxBytes caps the text ReadFileText loads from one file.
const fileTextMaxBytes = 4 * agentFileMaxBytes

// FileRefusal is a refusal meant for the model (a bad reference, no such file, not text); its text
// is the tool result's error. Any other error from these helpers is a lookup or storage failure.
type FileRefusal struct{ msg string }

func (e FileRefusal) Error() string { return e.msg }

// AsFileRefusal reports whether err is a FileRefusal and returns its text.
func AsFileRefusal(err error) (string, bool) {
	var refusal FileRefusal
	if errors.As(err, &refusal) {
		return refusal.msg, true
	}
	return "", false
}

func fileRefusalf(format string, args ...any) error {
	return FileRefusal{msg: fmt.Sprintf(format, args...)}
}

// UserFileLookup is the datastore surface ResolveUserFile reads through; *datastore.Datastore
// satisfies it.
type UserFileLookup interface {
	GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error)
	FindFileAttachmentsAtPath(ctx context.Context, userID uuid.UUID, folder, name string) ([]*models.FileAttachment, error)
}

// FilePath is the folder/name path a file is addressed by.
func FilePath(att *models.FileAttachment) string {
	if att.Folder == "" {
		return att.Name
	}
	return att.Folder + "/" + att.Name
}

// SplitFilePath splits a "folder/name" path into a normalized folder and a name.
func SplitFilePath(p string) (folder, name string, err error) {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return "", "", fileRefusalf("path is empty")
	}
	dir, base := path.Split(p)
	if folder, err = models.NormalizeFolder(dir); err != nil {
		return "", "", fileRefusalf("invalid folder in path: %v", err)
	}
	return folder, strings.TrimSpace(base), nil
}

// ResolveUserFile finds the user's file that ref names: an id, or a folder/name path such as
// "notes/journal.md" (the name matched case-insensitively). When a path names several files,
// prefer (if non-nil) picks the single one it accepts; otherwise the path is ambiguous and the
// refusal lists their ids.
func ResolveUserFile(ctx context.Context, lookup UserFileLookup, userID uuid.UUID, ref string, prefer func(*models.FileAttachment) bool) (*models.FileAttachment, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fileRefusalf("file is required: pass a file id or its path, e.g. \"notes/journal.md\"")
	}
	if id, err := uuid.Parse(ref); err == nil {
		att, err := lookup.GetFileAttachment(ctx, userID, id)
		if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
			return nil, fileRefusalf("no file with id %s", ref)
		}
		return att, err
	}
	folder, name, err := SplitFilePath(ref)
	if err != nil {
		return nil, err
	}
	matches, err := lookup.FindFileAttachmentsAtPath(ctx, userID, folder, name)
	if err != nil {
		return nil, err
	}
	switch len(matches) {
	case 0:
		return nil, fileRefusalf("no file at %q; list files with list (kind=\"files\")", ref)
	case 1:
		return matches[0], nil
	}
	var preferred []*models.FileAttachment
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.ID.String())
		if prefer != nil && prefer(m) {
			preferred = append(preferred, m)
		}
	}
	if len(preferred) == 1 {
		return preferred[0], nil
	}
	return nil, fileRefusalf("%d files are at %q; pass one of their ids instead: %s", len(matches), ref, strings.Join(ids, ", "))
}

// ReadableAsText reports whether att's content can be read as text.
func ReadableAsText(att *models.FileAttachment) bool {
	if strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
		return false
	}
	if att.AgentAuthored() || att.IsSkillFile() || strings.HasPrefix(att.FileType, "text/") ||
		filechunker.IsTextType(att.FileType) || filechunker.IsTextFileByExtension(att.Name) {
		return true
	}
	_, ok := agentFileTypes[strings.ToLower(path.Ext(att.Name))]
	return ok
}

// ReadFileText loads att's full text. Agent files and skill bundle files are read byte for byte
// from their current object; other files go through the attachment text resolver, which also knows
// legacy keys. A file that is not text, too large, not UTF-8, or whose stored content is missing is
// refused.
func ReadFileText(ctx context.Context, logger *zap.Logger, files storage.FileStore, att *models.FileAttachment) (string, error) {
	if !ReadableAsText(att) {
		return "", fileRefusalf("%q is not a text file (%s); use find_context to look at it", FilePath(att), att.FileType)
	}
	var raw []byte
	if att.AgentAuthored() || att.IsSkillFile() {
		// Agent files only ever live under the owner's agent prefix (storage.FileKeyForAgentFile)
		// and skill files under the skills prefix (storage.FileKeyForSkillFile); refuse any other
		// key rather than read an object the row should not point at.
		prefix := storage.AgentFileKeyPrefix(att.UserID)
		if att.IsSkillFile() {
			prefix = storage.SkillFileKeyPrefix(att.UserID)
		}
		if !strings.HasPrefix(att.S3Key, prefix) {
			return "", fileRefusalf("%q could not be read: its stored content is missing", FilePath(att))
		}
		data, err := files.DownloadFile(ctx, att.S3Key)
		if err != nil {
			return "", err
		}
		raw = data
	} else {
		text, ok := storage.ResolveAttachmentTextContent(ctx, logger, files, att.UserID, att)
		if !ok {
			return "", fileRefusalf("%q could not be read: its stored content is missing or empty", FilePath(att))
		}
		raw = []byte(text)
	}
	if len(raw) > fileTextMaxBytes {
		return "", fileRefusalf("%q is too large to read here (%d bytes)", FilePath(att), len(raw))
	}
	if !utf8.Valid(raw) {
		return "", fileRefusalf("%q is not valid UTF-8 text", FilePath(att))
	}
	return string(raw), nil
}
