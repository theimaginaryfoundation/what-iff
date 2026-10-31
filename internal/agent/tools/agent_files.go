package tools

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/filechunker"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/filedelete"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// Agent file tools (ADR 0x034): agent_file_write and agent_file_read. Files are ordinary file
// attachments in the user's file area (they show in the gallery and in list), stored under
// users/{uid}/agent/ and never chunked for search. An agent may change only the files its
// personality created (with no personality, the files its conversation created); every other file,
// including all of the user's uploads, is read-only to it. Loadable; not wired into pre- or
// post-processing.

const (
	agentFileWriteToolName = "agent_file_write"
	agentFileReadToolName  = "agent_file_read"

	// agentFileMaxBytes caps one file; agentFileMaxCallBytes caps the text one call sends.
	agentFileMaxBytes     = 1 << 20
	agentFileMaxCallBytes = 256 << 10
	// agentFileMaxPerUser caps the agent-authored files an account holds.
	agentFileMaxPerUser = 1000
)

// agentFileTypes are the extensions an agent can create, with their stored MIME types. A name
// without an extension gets ".md".
var agentFileTypes = map[string]string{
	".md":    "text/markdown",
	".txt":   "text/plain",
	".json":  "application/json",
	".jsonl": "application/jsonl",
	".yaml":  "application/yaml",
	".yml":   "application/yaml",
	".csv":   "text/csv",
	".tsv":   "text/tab-separated-values",
	".toml":  "application/toml",
	".xml":   "application/xml",
	".html":  "text/html",
	".log":   "text/plain",
}

// agentFileStore is the narrow datastore surface the file tools need. *datastore.Datastore
// satisfies it.
type agentFileStore interface {
	GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error)
	FindFileAttachmentsAtPath(ctx context.Context, userID uuid.UUID, folder, name string) ([]*models.FileAttachment, error)
	CreateFileAttachment(ctx context.Context, userID uuid.UUID, att models.FileAttachment) (*models.FileAttachment, error)
	SwapAgentFileObject(ctx context.Context, userID, id uuid.UUID, oldKey, newKey string) (bool, error)
	MoveFileAttachmentsToFolder(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, folder string) (int, error)
	RenameGalleryFile(ctx context.Context, userID, id uuid.UUID, name string) (string, error)
	CountAgentFiles(ctx context.Context, userID uuid.UUID) (int, error)
	storage.AttachmentKeyRefs
}

// agentFileDeleter deletes a file row and releases its objects; *filedelete.Service satisfies it.
type agentFileDeleter interface {
	DeleteFile(ctx context.Context, userID, id uuid.UUID) error
}

// AgentFileTool implements agent_file_write and agent_file_read.
type AgentFileTool struct {
	store   agentFileStore
	files   storage.FileStore
	deleter agentFileDeleter
	logger  *zap.Logger
}

// NewAgentFileTool constructs an AgentFileTool backed by the datastore and object store.
func NewAgentFileTool(ds *datastore.Datastore, files storage.FileStore, logger *zap.Logger) *AgentFileTool {
	return &AgentFileTool{store: ds, files: files, deleter: filedelete.New(ds, files, logger), logger: logger}
}

// errAgentFile is a refusal meant for the model; its text is the tool result's error.
type errAgentFile struct{ msg string }

func (e errAgentFile) Error() string { return e.msg }

func agentFileErrorf(format string, args ...any) error {
	return errAgentFile{msg: fmt.Sprintf(format, args...)}
}

// agentFileRef is how results name a file.
type agentFileRef struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Bytes    int    `json:"bytes,omitempty"`
	Lines    int    `json:"lines,omitempty"`
	Editable bool   `json:"editable"`
}

// filePath is the folder/name path the tools address att by.
func filePath(att *models.FileAttachment) string {
	if att.Folder == "" {
		return att.Name
	}
	return att.Folder + "/" + att.Name
}

// splitAgentFilePath splits a "folder/name" path into a normalized folder and a name.
func splitAgentFilePath(p string) (folder, name string, err error) {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return "", "", agentFileErrorf("path is empty")
	}
	dir, base := path.Split(p)
	if folder, err = models.NormalizeFolder(dir); err != nil {
		return "", "", agentFileErrorf("invalid folder in path: %v", err)
	}
	return folder, strings.TrimSpace(base), nil
}

// ownsAgentFile reports whether the agent in chat may change att: an agent-authored file created
// by this chat's personality, or with no personality, by this conversation.
func ownsAgentFile(chat *models.Chat, att *models.FileAttachment) bool {
	if att == nil || !att.IsAgentAuthored() {
		return false
	}
	if chat.PersonalityID != uuid.Nil {
		return att.CreatedByPersonalityID != nil && *att.CreatedByPersonalityID == chat.PersonalityID
	}
	return att.CreatedByPersonalityID == nil && att.CreatedByChatID != nil && *att.CreatedByChatID == chat.ID
}

func (t *AgentFileTool) ref(chat *models.Chat, att *models.FileAttachment, content string) agentFileRef {
	r := agentFileRef{ID: att.ID.String(), Path: filePath(att), Editable: ownsAgentFile(chat, att)}
	if content != "" {
		r.Bytes = len(content)
		r.Lines = strings.Count(content, "\n")
		if !strings.HasSuffix(content, "\n") {
			r.Lines++
		}
	}
	return r
}

// resolve finds the file a "file" argument names: an id, or a folder/name path. A path that names
// several files prefers the one this agent may edit; otherwise it is ambiguous.
func (t *AgentFileTool) resolve(ctx context.Context, chat *models.Chat, file string) (*models.FileAttachment, error) {
	file = strings.TrimSpace(file)
	if file == "" {
		return nil, agentFileErrorf("file is required: pass a file id or its path, e.g. \"notes/journal.md\"")
	}
	if id, err := uuid.Parse(file); err == nil {
		att, err := t.store.GetFileAttachment(ctx, chat.UserID, id)
		if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
			return nil, agentFileErrorf("no file with id %s", file)
		}
		return att, err
	}
	folder, name, err := splitAgentFilePath(file)
	if err != nil {
		return nil, err
	}
	matches, err := t.store.FindFileAttachmentsAtPath(ctx, chat.UserID, folder, name)
	if err != nil {
		return nil, err
	}
	switch len(matches) {
	case 0:
		return nil, agentFileErrorf("no file at %q; list files with list (kind=\"files\")", file)
	case 1:
		return matches[0], nil
	}
	var mine []*models.FileAttachment
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.ID.String())
		if ownsAgentFile(chat, m) {
			mine = append(mine, m)
		}
	}
	if len(mine) == 1 {
		return mine[0], nil
	}
	return nil, agentFileErrorf("%d files are at %q; pass one of their ids instead: %s", len(matches), file, strings.Join(ids, ", "))
}

// resolveOwned is resolve plus the edit check.
func (t *AgentFileTool) resolveOwned(ctx context.Context, chat *models.Chat, file string) (*models.FileAttachment, error) {
	att, err := t.resolve(ctx, chat, file)
	if err != nil {
		return nil, err
	}
	if !ownsAgentFile(chat, att) {
		what := "the user's file"
		switch {
		case att.IsSkillFile():
			what = "a skill's file, managed in the skill editor"
		case att.IsAgentAuthored():
			what = "a file another agent created"
		}
		return nil, agentFileErrorf("%q is %s, which you can read but not change. To work from it, copy it to a new file of your own with action \"copy\".", filePath(att), what)
	}
	return att, nil
}

// readableAsText reports whether att's content can be read as text.
func readableAsText(att *models.FileAttachment) bool {
	if strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
		return false
	}
	if att.IsAgentAuthored() || strings.HasPrefix(att.FileType, "text/") ||
		filechunker.IsTextType(att.FileType) || filechunker.IsTextFileByExtension(att.Name) {
		return true
	}
	_, ok := agentFileTypes[strings.ToLower(path.Ext(att.Name))]
	return ok
}

// readText loads att's full text. Agent and skill files are read from their current object; other
// files go through the attachment text resolver, which also knows legacy keys.
func (t *AgentFileTool) readText(ctx context.Context, att *models.FileAttachment) (string, error) {
	if !readableAsText(att) {
		return "", agentFileErrorf("%q is not a text file (%s); use find_context to look at it", filePath(att), att.FileType)
	}
	var raw []byte
	if att.StoredUnchunked() {
		data, err := t.files.DownloadFile(ctx, att.S3Key)
		if err != nil {
			return "", err
		}
		raw = data
	} else {
		text, ok := storage.ResolveAttachmentTextContent(ctx, t.logger, t.files, att.UserID, att)
		if !ok {
			return "", nil
		}
		raw = []byte(text)
	}
	if len(raw) > 4*agentFileMaxBytes {
		return "", agentFileErrorf("%q is too large to read here (%d bytes)", filePath(att), len(raw))
	}
	if !utf8.Valid(raw) {
		return "", agentFileErrorf("%q is not valid UTF-8 text", filePath(att))
	}
	return string(raw), nil
}

// agentFileName checks a new file's name and returns it with its MIME type. A name without an
// extension gets ".md".
func agentFileName(name string) (string, string, error) {
	name = strings.TrimSpace(name)
	if path.Ext(name) == "" {
		name += ".md"
	}
	norm, err := models.NormalizeFileName(name, name)
	if err != nil {
		return "", "", agentFileErrorf("invalid file name: %v", err)
	}
	ext := strings.ToLower(path.Ext(norm))
	mimeType, ok := agentFileTypes[ext]
	if !ok {
		exts := make([]string, 0, len(agentFileTypes))
		for e := range agentFileTypes {
			exts = append(exts, e)
		}
		sort.Strings(exts)
		return "", "", agentFileErrorf("%q is not a text type agents can create; use one of %s", ext, strings.Join(exts, " "))
	}
	return norm, mimeType, nil
}

func checkAgentFileSize(content string) error {
	if len(content) > agentFileMaxBytes {
		return agentFileErrorf("the file would be %d bytes; files are limited to %d bytes. Split it into several files.", len(content), agentFileMaxBytes)
	}
	return nil
}
