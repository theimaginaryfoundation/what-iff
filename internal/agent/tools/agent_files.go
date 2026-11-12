package tools

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/filedelete"
	"github.com/theimaginaryfoundation/what-iff/internal/filesave"
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
	agentFileMaxBytes     = models.AgentFileMaxBytes
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
	UserFileLookup
	CreateFileAttachment(ctx context.Context, userID uuid.UUID, att models.FileAttachment) (*models.FileAttachment, error)
	SwapAgentFileObject(ctx context.Context, userID, id uuid.UUID, oldKey, newKey string, sizeBytes int64, sha256Hex string) (bool, error)
	MoveFileAttachmentsToFolder(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, folder string) (int, error)
	RenameGalleryFile(ctx context.Context, userID, id uuid.UUID, name string) (string, error)
	CountAgentFiles(ctx context.Context, userID uuid.UUID) (int, error)
	// HasFileEditGrant: the user mentioned the chat's agent in a comment on the file (ADR 0x046).
	HasFileEditGrant(ctx context.Context, userID, chatID, fileID uuid.UUID) (bool, error)
	storage.AttachmentKeyRefs
}

// userFileSaver saves new content for one of the user's files, as the file workspace does;
// *filesave.Service satisfies it.
type userFileSaver interface {
	Replace(ctx context.Context, userID uuid.UUID, req filesave.Request) (*models.FileAttachment, error)
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
	// saver changes the content of a user's file the agent was granted (ADR 0x046); nil refuses.
	saver  userFileSaver
	logger *zap.Logger
}

// WithUserFileSaver sets how the tool saves a user's file it was granted (ADR 0x046).
func (t *AgentFileTool) WithUserFileSaver(saver userFileSaver) *AgentFileTool {
	t.saver = saver
	return t
}

// NewAgentFileTool constructs an AgentFileTool backed by the datastore and object store.
func NewAgentFileTool(ds *datastore.Datastore, files storage.FileStore, logger *zap.Logger) *AgentFileTool {
	return &AgentFileTool{store: ds, files: files, deleter: filedelete.New(ds, files, logger), logger: logger}
}

// agentFileRef is how results name a file.
type agentFileRef struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Bytes    int    `json:"bytes,omitempty"`
	Lines    int    `json:"lines,omitempty"`
	Editable bool   `json:"editable"`
}

// checkAgentFolder refuses a folder under a reserved top-level folder: skills/ holds skill bundles
// (models.ReservedSkillsFolder, ADR 0x035), written only by the skill editor and import.
func checkAgentFolder(folder string) error {
	if models.IsReservedFolder(strings.ToLower(folder)) {
		return fileRefusalf("the %q folder is managed by your skills; write elsewhere", models.ReservedSkillsFolder+"/")
	}
	return nil
}

// ownsAgentFile reports whether the agent in chat may change att: an agent-authored file created
// by this chat's personality, or with no personality, by this conversation.
func ownsAgentFile(chat *models.Chat, att *models.FileAttachment) bool {
	if att == nil || !att.AgentAuthored() {
		return false
	}
	if chat.PersonalityID != uuid.Nil {
		return att.CreatedByPersonalityID != nil && *att.CreatedByPersonalityID == chat.PersonalityID
	}
	return att.CreatedByPersonalityID == nil && att.CreatedByChatID != nil && *att.CreatedByChatID == chat.ID
}

// granted reports whether the user granted chat's agent att (ADR 0x046): a comment on the file
// that mentions the agent. Skill files stay with the skill editor.
func (t *AgentFileTool) granted(ctx context.Context, chat *models.Chat, att *models.FileAttachment) bool {
	if att == nil || att.IsSkillFile() || chat.ID == uuid.Nil {
		return false
	}
	ok, err := t.store.HasFileEditGrant(ctx, chat.UserID, chat.ID, att.ID)
	if err != nil {
		t.logger.Warn("agent file tools: could not read a file edit grant", zap.String("file_attachment_id", att.ID.String()), zap.Error(err))
		return false
	}
	return ok
}

// mayChange reports whether the agent in chat may change att's content: its own file, or one the
// user granted it.
func (t *AgentFileTool) mayChange(ctx context.Context, chat *models.Chat, att *models.FileAttachment) bool {
	return ownsAgentFile(chat, att) || t.granted(ctx, chat, att)
}

// refEditable is ref with Editable saying whether the agent may change the content.
func (t *AgentFileTool) refEditable(ctx context.Context, chat *models.Chat, att *models.FileAttachment, content string) agentFileRef {
	r := t.ref(chat, att, content)
	r.Editable = t.mayChange(ctx, chat, att)
	return r
}

func (t *AgentFileTool) ref(chat *models.Chat, att *models.FileAttachment, content string) agentFileRef {
	r := agentFileRef{ID: att.ID.String(), Path: FilePath(att), Editable: ownsAgentFile(chat, att)}
	if content != "" {
		r.Bytes = len(content)
		r.Lines = strings.Count(content, "\n")
		if !strings.HasSuffix(content, "\n") {
			r.Lines++
		}
	}
	return r
}

// resolve finds the file a "file" argument names. A path that names several files prefers the one
// this agent may edit; otherwise it is ambiguous.
func (t *AgentFileTool) resolve(ctx context.Context, chat *models.Chat, file string) (*models.FileAttachment, error) {
	return ResolveUserFile(ctx, t.store, chat.UserID, file, func(att *models.FileAttachment) bool { return ownsAgentFile(chat, att) })
}

// resolveChangeable is resolve plus the check that the agent may change the file's content: its
// own, or one the user granted it by mentioning it in a comment on the file (ADR 0x046).
func (t *AgentFileTool) resolveChangeable(ctx context.Context, chat *models.Chat, file string) (*models.FileAttachment, error) {
	att, err := t.resolve(ctx, chat, file)
	if err != nil {
		return nil, err
	}
	if t.granted(ctx, chat, att) {
		return att, nil
	}
	return t.checkOwned(chat, att)
}

// resolveOwned is resolve plus the check that the file is the agent's own: deleting, moving and
// renaming are for its own files only, even ones it was granted.
func (t *AgentFileTool) resolveOwned(ctx context.Context, chat *models.Chat, file string) (*models.FileAttachment, error) {
	att, err := t.resolve(ctx, chat, file)
	if err != nil {
		return nil, err
	}
	return t.checkOwned(chat, att)
}

func (t *AgentFileTool) checkOwned(chat *models.Chat, att *models.FileAttachment) (*models.FileAttachment, error) {
	if !ownsAgentFile(chat, att) {
		what := "the user's file"
		switch {
		case att.IsSkillFile():
			what = "a skill's file, managed in the skill editor"
		case att.AgentAuthored():
			what = "a file another agent created"
		}
		hint := ""
		if !att.IsSkillFile() {
			hint = " The user can let you change it by mentioning you in a comment on it."
		}
		return nil, fileRefusalf("%q is %s, which you can read but not change. To work from it, copy it to a new file of your own with action \"copy\".%s", FilePath(att), what, hint)
	}
	return att, nil
}

// readText loads att's full text.
func (t *AgentFileTool) readText(ctx context.Context, att *models.FileAttachment) (string, error) {
	return ReadFileText(ctx, t.logger, t.files, att)
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
		return "", "", fileRefusalf("invalid file name: %v", err)
	}
	ext := strings.ToLower(path.Ext(norm))
	mimeType, ok := agentFileTypes[ext]
	if !ok {
		exts := make([]string, 0, len(agentFileTypes))
		for e := range agentFileTypes {
			exts = append(exts, e)
		}
		sort.Strings(exts)
		return "", "", fileRefusalf("%q is not a text type agents can create; use one of %s", ext, strings.Join(exts, " "))
	}
	return norm, mimeType, nil
}

func checkAgentFileSize(content string) error {
	if len(content) > agentFileMaxBytes {
		return fileRefusalf("the file would be %d bytes; files are limited to %d bytes. Split it into several files.", len(content), agentFileMaxBytes)
	}
	return nil
}
