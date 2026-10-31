package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// memObjects is an in-memory storage.FileStore.
type memObjects struct{ objs map[string][]byte }

func (m *memObjects) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	m.objs[key] = append([]byte(nil), content...)
	return nil
}
func (m *memObjects) DownloadFile(_ context.Context, key string) ([]byte, error) {
	return m.objs[key], nil
}
func (m *memObjects) DeleteFile(_ context.Context, key string) error {
	delete(m.objs, key)
	return nil
}

// fakeAgentFileStore is an in-memory agentFileStore and agentFileDeleter.
type fakeAgentFileStore struct {
	userID uuid.UUID
	rows   map[uuid.UUID]*models.FileAttachment
	order  []uuid.UUID
	// beforeSwap runs before each swap, to simulate a concurrent writer.
	beforeSwap func(id uuid.UUID)
}

func (f *fakeAgentFileStore) GetFileAttachment(_ context.Context, userID, id uuid.UUID) (*models.FileAttachment, error) {
	r, ok := f.rows[id]
	if !ok || userID != f.userID {
		return nil, datastore.ErrFileAttachmentNotFound
	}
	c := *r
	return &c, nil
}
func (f *fakeAgentFileStore) FindFileAttachmentsAtPath(_ context.Context, userID uuid.UUID, folder, name string) ([]*models.FileAttachment, error) {
	var out []*models.FileAttachment
	for _, id := range f.order {
		r, ok := f.rows[id]
		if ok && userID == f.userID && r.Folder == folder && strings.EqualFold(r.Name, strings.TrimSpace(name)) {
			c := *r
			out = append(out, &c)
		}
	}
	return out, nil
}
func (f *fakeAgentFileStore) CreateFileAttachment(_ context.Context, userID uuid.UUID, att models.FileAttachment) (*models.FileAttachment, error) {
	att.ID, att.UserID = uuid.New(), userID
	f.rows[att.ID] = &att
	f.order = append(f.order, att.ID)
	c := att
	return &c, nil
}
func (f *fakeAgentFileStore) SwapAgentFileObject(_ context.Context, _, id uuid.UUID, oldKey, newKey string) (bool, error) {
	if f.beforeSwap != nil {
		f.beforeSwap(id)
	}
	r, ok := f.rows[id]
	if !ok || !r.AgentAuthored() || r.S3Key != oldKey {
		return false, nil
	}
	r.S3Key = newKey
	return true, nil
}
func (f *fakeAgentFileStore) MoveFileAttachmentsToFolder(_ context.Context, _ uuid.UUID, ids []uuid.UUID, folder string) (int, error) {
	for _, id := range ids {
		f.rows[id].Folder = folder
	}
	return len(ids), nil
}
func (f *fakeAgentFileStore) RenameGalleryFile(_ context.Context, _, id uuid.UUID, name string) (string, error) {
	f.rows[id].Name = name
	return name, nil
}
func (f *fakeAgentFileStore) CountAgentFiles(context.Context, uuid.UUID) (int, error) {
	n := 0
	for _, r := range f.rows {
		if r.AgentAuthored() {
			n++
		}
	}
	return n, nil
}
func (f *fakeAgentFileStore) ReferencedFileAttachmentKeys(_ context.Context, keys []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, r := range f.rows {
		for _, k := range keys {
			if r.S3Key == k {
				out[k] = true
			}
		}
	}
	return out, nil
}
func (f *fakeAgentFileStore) ExistingFileAttachmentIDs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	for _, id := range ids {
		if _, ok := f.rows[id]; ok {
			out[id] = true
		}
	}
	return out, nil
}
func (f *fakeAgentFileStore) DeleteFile(_ context.Context, _, id uuid.UUID) error {
	delete(f.rows, id)
	return nil
}

type agentFileFixture struct {
	tool    *AgentFileTool
	store   *fakeAgentFileStore
	objects *memObjects
	userID  uuid.UUID
}

func newAgentFileFixture() *agentFileFixture {
	userID := uuid.New()
	store := &fakeAgentFileStore{userID: userID, rows: map[uuid.UUID]*models.FileAttachment{}}
	objects := &memObjects{objs: map[string][]byte{}}
	return &agentFileFixture{
		tool:    &AgentFileTool{store: store, files: objects, deleter: store, logger: zap.NewNop()},
		store:   store,
		objects: objects,
		userID:  userID,
	}
}

func (fx *agentFileFixture) chat(personalityID uuid.UUID) *models.Chat {
	return &models.Chat{ID: uuid.New(), UserID: fx.userID, PersonalityID: personalityID}
}

// userUpload adds a file the user uploaded.
func (fx *agentFileFixture) userUpload(folder, name, content string) *models.FileAttachment {
	key := "users/" + fx.userID.String() + "/chats/x/" + uuid.NewString() + "_" + name
	fx.objects.objs[key] = []byte(content)
	att, _ := fx.store.CreateFileAttachment(context.Background(), fx.userID, models.FileAttachment{Name: name, FileType: "text/markdown", S3Key: key, Folder: folder})
	return att
}

func callJSON(t *testing.T, fn func(context.Context, *models.Chat, []byte) (string, error), chat *models.Chat, args map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	out, err := fn(context.Background(), chat, raw)
	require.NoError(t, err)
	var res map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res
}

func requireOK(t *testing.T, res map[string]any) {
	t.Helper()
	require.Empty(t, res["error"], "unexpected error result")
}

func requireRefused(t *testing.T, res map[string]any, contains string) {
	t.Helper()
	msg, _ := res["error"].(string)
	require.Contains(t, msg, contains)
}

func (fx *agentFileFixture) text(t *testing.T, chat *models.Chat, file string) string {
	t.Helper()
	res := callJSON(t, fx.tool.Read, chat, map[string]any{"action": "read", "file": file})
	requireOK(t, res)
	s, _ := res["content"].(string)
	return s
}

func TestAgentFiles_CreateReadAppendPatch(t *testing.T) {
	fx := newAgentFileFixture()
	persona := uuid.New()
	chat := fx.chat(persona)

	res := callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "notes/journal", "content": "# Journal\n\n## Monday\nRained.\n"})
	requireOK(t, res)
	file := res["file"].(map[string]any)
	require.Equal(t, "notes/journal.md", file["path"], "a name without an extension gets .md")
	require.Equal(t, true, file["editable"])
	id := file["id"].(string)
	row := fx.store.rows[uuid.MustParse(id)]
	require.True(t, row.AgentAuthored())
	require.Equal(t, &persona, row.CreatedByPersonalityID)
	require.Nil(t, row.CreatedByChatID)
	require.True(t, strings.HasPrefix(row.S3Key, "users/"+fx.userID.String()+"/agent/"))

	firstKey := row.S3Key
	requireOK(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "append", "file": "notes/journal.md", "content": "## Tuesday\nSunny."}))
	require.Equal(t, "# Journal\n\n## Monday\nRained.\n## Tuesday\nSunny.", fx.text(t, chat, id))
	require.NotContains(t, fx.objects.objs, firstKey, "the replaced version is released")

	requireOK(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "patch", "file": id, "diff": "@@ -4,1 +4,1 @@\n-Rained.\n+Snowed.\n"}))
	require.Contains(t, fx.text(t, chat, id), "Snowed.")

	before := fx.text(t, chat, id)
	requireRefused(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "patch", "file": id, "diff": "@@ @@\n-Hailed.\n+Fog.\n"}), "patch not applied")
	require.Equal(t, before, fx.text(t, chat, id), "a refused patch changes nothing")

	requireRefused(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "Notes/JOURNAL.md", "content": "x"}), "already at")
	requireRefused(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "bin/run.exe", "content": "x"}), "not a text type")
	requireRefused(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "append", "file": id, "content": strings.Repeat("a", agentFileMaxBytes)}), "at most")
}

func TestAgentFiles_OnlyTheCreatingPersonalityChanges(t *testing.T) {
	fx := newAgentFileFixture()
	owner, other := fx.chat(uuid.New()), fx.chat(uuid.New())
	sameOwnerNewChat := fx.chat(owner.PersonalityID)

	res := callJSON(t, fx.tool.Write, owner, map[string]any{"action": "create", "path": "notes/plan.md", "content": "plan\n"})
	requireOK(t, res)
	id := res["file"].(map[string]any)["id"].(string)

	// Another conversation of the same personality may change it.
	requireOK(t, callJSON(t, fx.tool.Write, sameOwnerNewChat, map[string]any{"action": "append", "file": id, "content": "more"}))

	// Another personality may read it but not change, move or delete it.
	read := callJSON(t, fx.tool.Read, other, map[string]any{"action": "read", "file": id})
	requireOK(t, read)
	require.Equal(t, false, read["file"].(map[string]any)["editable"])
	for _, args := range []map[string]any{
		{"action": "append", "file": id, "content": "x"},
		{"action": "patch", "file": id, "diff": "@@ @@\n-plan\n+nope\n"},
		{"action": "delete", "file": id},
		{"action": "move", "file": id, "path": "x.md"},
	} {
		requireRefused(t, callJSON(t, fx.tool.Write, other, args), "another agent created")
	}

	// It can copy it to a file of its own.
	cp := callJSON(t, fx.tool.Write, other, map[string]any{"action": "copy", "file": id, "path": "mine/plan.md"})
	requireOK(t, cp)
	require.Equal(t, true, cp["file"].(map[string]any)["editable"])
	require.Equal(t, "plan\nmore", fx.text(t, other, "mine/plan.md"))

	requireOK(t, callJSON(t, fx.tool.Write, owner, map[string]any{"action": "delete", "file": id}))
	require.NotContains(t, fx.store.rows, uuid.MustParse(id))
}

func TestAgentFiles_UserFilesAreReadOnly(t *testing.T) {
	fx := newAgentFileFixture()
	chat := fx.chat(uuid.New())
	upload := fx.userUpload("docs", "brief.md", "# Brief\nShip it.\n")

	require.Equal(t, "# Brief\nShip it.", fx.text(t, chat, "docs/brief.md"))
	for _, args := range []map[string]any{
		{"action": "append", "file": upload.ID.String(), "content": "x"},
		{"action": "patch", "file": "docs/brief.md", "diff": "@@ @@\n-Ship it.\n+Wait.\n"},
		{"action": "delete", "file": "docs/brief.md"},
		{"action": "move", "file": "docs/brief.md", "path": "x.md"},
	} {
		requireRefused(t, callJSON(t, fx.tool.Write, chat, args), "the user's file")
	}
	requireRefused(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "docs/brief.md", "content": "x"}), "already at")

	cp := callJSON(t, fx.tool.Write, chat, map[string]any{"action": "copy", "file": "docs/brief.md", "path": "drafts/brief.md"})
	requireOK(t, cp)
	requireOK(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "patch", "file": "drafts/brief.md", "diff": "@@ @@\n # Brief\n-Ship it.\n+Wait.\n"}))
	require.Equal(t, "# Brief\nShip it.", fx.text(t, chat, "docs/brief.md"), "the original is untouched")
}

func TestAgentFiles_NoPersonalityOwnsPerConversation(t *testing.T) {
	fx := newAgentFileFixture()
	chat, otherChat := fx.chat(uuid.Nil), fx.chat(uuid.Nil)

	res := callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "scratch.txt", "content": "a"})
	requireOK(t, res)
	row := fx.store.rows[uuid.MustParse(res["file"].(map[string]any)["id"].(string))]
	require.Nil(t, row.CreatedByPersonalityID)
	require.Equal(t, &chat.ID, row.CreatedByChatID)

	requireOK(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "append", "file": "scratch.txt", "content": "b"}))
	requireRefused(t, callJSON(t, fx.tool.Write, otherChat, map[string]any{"action": "append", "file": "scratch.txt", "content": "c"}), "another agent")
	requireRefused(t, callJSON(t, fx.tool.Write, fx.chat(uuid.New()), map[string]any{"action": "append", "file": "scratch.txt", "content": "c"}), "another agent")
}

func TestAgentFiles_MoveAndAmbiguousPaths(t *testing.T) {
	fx := newAgentFileFixture()
	chat := fx.chat(uuid.New())
	requireOK(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "a.md", "content": "x"}))

	mv := callJSON(t, fx.tool.Write, chat, map[string]any{"action": "move", "file": "a.md", "path": "notes/b.md"})
	requireOK(t, mv)
	require.Equal(t, "notes/b.md", mv["file"].(map[string]any)["path"])
	require.Equal(t, "a.md", mv["from"].(map[string]any)["path"])
	requireRefused(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "move", "file": "notes/b.md", "path": "notes/b.txt"}), "keeps the file's type")

	// Two user files at one path: the path is ambiguous, ids still work.
	u1 := fx.userUpload("", "dup.md", "1")
	fx.userUpload("", "dup.md", "2")
	requireRefused(t, callJSON(t, fx.tool.Read, chat, map[string]any{"action": "read", "file": "dup.md"}), "pass one of their ids")
	require.Equal(t, "1", fx.text(t, chat, u1.ID.String()))
}

func TestAgentFiles_RetriesWhenAnotherWriterWins(t *testing.T) {
	fx := newAgentFileFixture()
	chat := fx.chat(uuid.New())
	res := callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "log.txt", "content": "a"})
	requireOK(t, res)
	id := uuid.MustParse(res["file"].(map[string]any)["id"].(string))

	raced := false
	fx.store.beforeSwap = func(id uuid.UUID) {
		if raced {
			return
		}
		raced = true
		// Another append lands between this call's read and its swap, releasing the version it
		// replaced as a real writer would.
		key := "users/" + fx.userID.String() + "/agent/other-log.txt"
		fx.objects.objs[key] = []byte("a\nb")
		delete(fx.objects.objs, fx.store.rows[id].S3Key)
		fx.store.rows[id].S3Key = key
	}
	requireOK(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "append", "file": id.String(), "content": "c"}))
	require.Equal(t, "a\nb\nc", fx.text(t, chat, id.String()))
	require.Len(t, fx.objects.objs, 1, "the losing upload and the replaced version are both cleaned up")
}

func TestAgentFiles_ReadPagingAndHeadingSearch(t *testing.T) {
	fx := newAgentFileFixture()
	chat := fx.chat(uuid.New())
	var b strings.Builder
	b.WriteString("# Plan\nintro\n\n## Open Questions\nq1\n### Detail\nd1\n```\n# not a heading\n```\n## Decisions\nship\n")
	for i := 0; i < agentFileReadMaxLines+10; i++ {
		b.WriteString("filler\n")
	}
	requireOK(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "plan.md", "content": b.String()}))

	page := callJSON(t, fx.tool.Read, chat, map[string]any{"action": "read", "file": "plan.md"})
	requireOK(t, page)
	require.EqualValues(t, 1, page["start_line"])
	require.EqualValues(t, agentFileReadMaxLines, page["end_line"])
	next := page["next_start_line"].(float64)
	page2 := callJSON(t, fx.tool.Read, chat, map[string]any{"action": "read", "file": "plan.md", "start_line": next})
	requireOK(t, page2)
	require.Nil(t, page2["next_start_line"])

	sec := callJSON(t, fx.tool.Read, chat, map[string]any{"action": "search", "file": "plan.md", "heading": "open questions"})
	requireOK(t, sec)
	require.Equal(t, "## Open Questions\nq1\n### Detail\nd1\n```\n# not a heading\n```", sec["content"], "the section runs to the next heading of the same or higher level")

	fuzzy := callJSON(t, fx.tool.Read, chat, map[string]any{"action": "search", "file": "plan.md", "heading": "questions open?"})
	require.Equal(t, "Open Questions", fuzzy["section"].(map[string]any)["title"])

	outline := callJSON(t, fx.tool.Read, chat, map[string]any{"action": "search", "file": "plan.md", "heading": ""})
	require.Len(t, outline["outline"], 4)

	miss := callJSON(t, fx.tool.Read, chat, map[string]any{"action": "search", "file": "plan.md", "heading": "budget"})
	require.Contains(t, miss["note"], "No heading matches")
	require.Len(t, miss["outline"], 4)
}

func TestAgentFiles_SandboxRefused(t *testing.T) {
	fx := newAgentFileFixture()
	chat := fx.chat(uuid.New())
	chat.ContextScope = models.ContextScopeSandbox
	requireRefused(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "a.md", "content": "x"}), "sandboxed")
	requireRefused(t, callJSON(t, fx.tool.Read, chat, map[string]any{"action": "read", "file": "a.md"}), "sandboxed")
}

func TestAgentFiles_SkillsFolderIsReserved(t *testing.T) {
	fx := newAgentFileFixture()
	chat := fx.chat(uuid.New())
	requireOK(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "notes/a.md", "content": "x"}))

	for _, args := range []map[string]any{
		{"action": "create", "path": "skills/b.md", "content": "x"},
		{"action": "create", "path": "Skills/my-skill/references/b.md", "content": "x"},
		{"action": "copy", "file": "notes/a.md", "path": "skills/my-skill/a.md"},
		{"action": "move", "file": "notes/a.md", "path": "skills/a.md"},
	} {
		requireRefused(t, callJSON(t, fx.tool.Write, chat, args), `"skills/" folder is managed by your skills`)
	}
	requireOK(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "skillset/b.md", "content": "x"}))
	requireOK(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "notes/skills/b.md", "content": "x"}))
}

func TestAgentFiles_UnreadableUserFileIsRefusedNotEmpty(t *testing.T) {
	fx := newAgentFileFixture()
	chat := fx.chat(uuid.New())
	upload := fx.userUpload("docs", "gone.md", "text")
	delete(fx.objects.objs, upload.S3Key)

	requireRefused(t, callJSON(t, fx.tool.Read, chat, map[string]any{"action": "read", "file": "docs/gone.md"}), "could not be read")
	requireRefused(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "copy", "file": "docs/gone.md", "path": "copy.md"}), "could not be read")
}

func TestResolveUserFile_PrefersAndRefuses(t *testing.T) {
	fx := newAgentFileFixture()
	ctx := context.Background()
	one := fx.userUpload("", "dup.md", "1")
	fx.userUpload("", "dup.md", "2")

	_, err := ResolveUserFile(ctx, fx.store, fx.userID, "dup.md", nil)
	msg, ok := AsFileRefusal(err)
	require.True(t, ok)
	require.Contains(t, msg, "pass one of their ids")

	got, err := ResolveUserFile(ctx, fx.store, fx.userID, "/DUP.md", func(att *models.FileAttachment) bool { return att.ID == one.ID })
	require.NoError(t, err)
	require.Equal(t, one.ID, got.ID)

	for ref, want := range map[string]string{"": "file is required", "nope.md": "no file at", uuid.NewString(): "no file with id"} {
		_, err := ResolveUserFile(ctx, fx.store, fx.userID, ref, nil)
		msg, ok := AsFileRefusal(err)
		require.True(t, ok, ref)
		require.Contains(t, msg, want)
	}
}

func TestAgentFiles_WriteFieldsFollowTheAction(t *testing.T) {
	fx := newAgentFileFixture()
	chat := fx.chat(uuid.New())
	requireRefused(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "a.md"}), `action "create" takes path and content; missing content`)
	requireRefused(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "a.md", "content": "x", "file": "a.md"}), "leave out file")
	requireRefused(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "delete", "file": "a.md", "diff": "@@ @@"}), `action "delete" takes file; leave out diff`)
	requireRefused(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "rename", "file": "a.md"}), "action must be one of")
	// Empty strings for unused fields are fine: models often send every property.
	requireOK(t, callJSON(t, fx.tool.Write, chat, map[string]any{"action": "create", "path": "a.md", "content": "x", "file": "", "diff": ""}))
}
