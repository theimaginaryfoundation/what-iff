package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// memObjectStore is an in-memory storage.FileStore.
type memObjectStore struct {
	mu        sync.Mutex
	objects   map[string][]byte
	failNextU bool
}

func newMemObjectStore() *memObjectStore { return &memObjectStore{objects: map[string][]byte{}} }

func (s *memObjectStore) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNextU {
		s.failNextU = false
		return errors.New("upload failed")
	}
	s.objects[key] = append([]byte(nil), content...)
	return nil
}

func (s *memObjectStore) DownloadFile(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, nil
	}
	return data, nil
}

func (s *memObjectStore) DeleteFile(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

// memWorkspaceStore mirrors the datastore's commit semantics (create at 1, conditional advance,
// typed conflicts) in memory.
type memWorkspaceStore struct {
	mu        sync.Mutex
	files     map[string]*models.WorkspaceFile
	revisions map[uuid.UUID][]models.WorkspaceRevisionInput
	// beforeCommit runs inside CommitWorkspaceRevision, to simulate a concurrent writer.
	beforeCommit func()
	touched      int
}

func newMemWorkspaceStore() *memWorkspaceStore {
	return &memWorkspaceStore{files: map[string]*models.WorkspaceFile{}, revisions: map[uuid.UUID][]models.WorkspaceRevisionInput{}}
}

func wsKey(userID uuid.UUID, root string, ref uuid.UUID, p string) string {
	return userID.String() + "|" + root + "|" + ref.String() + "|" + p
}

func (s *memWorkspaceStore) GetWorkspaceFile(_ context.Context, userID uuid.UUID, root string, rootRef uuid.UUID, p string) (*models.WorkspaceFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[wsKey(userID, root, rootRef, p)]
	if !ok {
		return nil, datastore.ErrWorkspaceFileNotFound
	}
	cp := *f
	return &cp, nil
}

func (s *memWorkspaceStore) ListWorkspaceFiles(_ context.Context, userID uuid.UUID, root string, rootRef uuid.UUID, prefix string, limit int) ([]*models.WorkspaceFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.WorkspaceFile
	for _, f := range s.files {
		if f.UserID == userID && f.Root == root && f.RootRef == rootRef && f.State == models.WorkspaceFileLive && strings.HasPrefix(f.Path, prefix) {
			cp := *f
			out = append(out, &cp)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *memWorkspaceStore) GetWorkspaceUsage(_ context.Context, userID uuid.UUID) (models.WorkspaceUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var u models.WorkspaceUsage
	for _, f := range s.files {
		if f.UserID == userID && f.State == models.WorkspaceFileLive {
			u.Files++
			u.Bytes += f.Size
		}
	}
	return u, nil
}

func (s *memWorkspaceStore) CommitWorkspaceRevision(_ context.Context, userID uuid.UUID, in models.WorkspaceRevisionInput) (*models.WorkspaceFile, error) {
	if s.beforeCommit != nil {
		hook := s.beforeCommit
		s.beforeCommit = nil
		hook()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := wsKey(userID, in.Root, in.RootRef, in.Path)
	state := models.WorkspaceFileLive
	if in.Op == models.WorkspaceOpDelete {
		state = models.WorkspaceFileDeleted
	}
	f, ok := s.files[key]
	if !ok {
		if in.BaseRevision > 0 {
			return nil, &datastore.WorkspaceConflictError{Current: 0}
		}
		f = &models.WorkspaceFile{ID: uuid.New(), UserID: userID, Root: in.Root, RootRef: in.RootRef, Path: in.Path, CreatedAt: time.Now()}
		s.files[key] = f
	} else if in.Op == models.WorkspaceOpCreate && f.State == models.WorkspaceFileLive {
		return nil, &datastore.WorkspaceConflictError{Current: f.CurrentRevision}
	} else if in.BaseRevision != models.WorkspaceAnyRevision && in.BaseRevision != f.CurrentRevision {
		return nil, &datastore.WorkspaceConflictError{Current: f.CurrentRevision}
	}
	f.CurrentRevision++
	f.ContentType, f.Size, f.SHA256, f.StorageKey, f.State, f.AuthorClass = in.ContentType, in.Size, in.SHA256, in.StorageKey, state, in.AuthorClass
	f.UpdatedAt = time.Now()
	s.revisions[f.ID] = append(s.revisions[f.ID], in)
	cp := *f
	return &cp, nil
}

func (s *memWorkspaceStore) PurgeWorkspaceRoot(_ context.Context, userID uuid.UUID, root string, rootRef uuid.UUID) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	for k, f := range s.files {
		if f.UserID == userID && f.Root == root && f.RootRef == rootRef {
			for _, rev := range s.revisions[f.ID] {
				if rev.StorageKey != "" {
					keys = append(keys, rev.StorageKey)
				}
			}
			delete(s.revisions, f.ID)
			delete(s.files, k)
		}
	}
	return keys, nil
}

func (s *memWorkspaceStore) TouchWorkspaceFileRead(_ context.Context, _, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touched++
	return nil
}

type wsFixture struct {
	ws      *WorkspaceTool
	reader  *FileReadTool
	list    *ListTool
	store   *memWorkspaceStore
	objects *memObjectStore
	chat    *models.Chat
}

func newWSFixture() *wsFixture {
	f := &wsFixture{
		store:   newMemWorkspaceStore(),
		objects: newMemObjectStore(),
		chat:    &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()},
	}
	f.ws = NewWorkspaceTool(f.store, f.objects, zap.NewNop())
	f.reader = &FileReadTool{
		store:    &fakeFileReadStore{files: map[uuid.UUID]*models.FileAttachment{}},
		logger:   zap.NewNop(),
		cache:    newFileTextCache(fileTextCacheMaxBytes),
		loadText: func(context.Context, uuid.UUID, *models.FileAttachment) (string, bool) { return "", false },
	}
	f.reader.SetWorkspace(f.ws)
	f.list = &ListTool{logger: zap.NewNop()}
	f.list.SetWorkspace(f.ws)
	return f
}

func (f *wsFixture) write(t *testing.T, args map[string]interface{}) writeFileResult {
	t.Helper()
	in, err := json.Marshal(args)
	require.NoError(t, err)
	out, err := f.ws.WriteFile(context.Background(), f.chat, in)
	require.NoError(t, err)
	var res writeFileResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res
}

func (f *wsFixture) read(t *testing.T, args map[string]interface{}) readFileResult {
	t.Helper()
	in, err := json.Marshal(args)
	require.NoError(t, err)
	out, err := f.reader.ReadFile(context.Background(), f.chat, in)
	require.NoError(t, err)
	var res readFileResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res
}

func TestParseWorkspacePath(t *testing.T) {
	chat := &models.Chat{ID: uuid.New(), PersonalityID: uuid.New()}

	ok := map[string]workspaceAddr{
		"agent/journal.md":            {Root: "agent", RootRef: chat.PersonalityID, Path: "journal.md"},
		"chat/work/plan.json":         {Root: "chat", RootRef: chat.ID, Path: "work/plan.json"},
		"Agent/Notes/Dashboards.MD":   {Root: "agent", RootRef: chat.PersonalityID, Path: "Notes/Dashboards.MD"},
		" chat/campaign log.txt ":     {Root: "chat", RootRef: chat.ID, Path: "campaign log.txt"},
		"agent/naïve/café-notes.yaml": {Root: "agent", RootRef: chat.PersonalityID, Path: "naïve/café-notes.yaml"},
	}
	for in, want := range ok {
		got, err := parseWorkspacePath(chat, in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	bad := []string{
		"journal.md", "me/journal.md", "agent/", "agent//x.md", "agent/../x.md", "agent/./x.md",
		`agent\x.md`, "agent/x.exe", "agent/x", "agent/a:b.md", "agent/ x.md", "chat/x.md\x00",
		"agent/" + strings.Repeat("a/", workspaceMaxDepth) + "x.md",
		"agent/" + strings.Repeat("a", workspaceMaxPathLen) + ".md",
	}
	for _, in := range bad {
		_, err := parseWorkspacePath(chat, in)
		assert.Error(t, err, "%q should be rejected", in)
	}

	noPersona := &models.Chat{ID: uuid.New()}
	_, err := parseWorkspacePath(noPersona, "agent/x.md")
	assert.ErrorContains(t, err, "use chat/")
}

func TestWriteFile_CreateReadOverwriteWithRevisions(t *testing.T) {
	f := newWSFixture()

	created := f.write(t, map[string]interface{}{"path": "agent/notes.md", "content": "# Notes\nfirst"})
	require.True(t, created.Success, created.Error)
	assert.Equal(t, models.WorkspaceOpCreate, created.Op)
	assert.Equal(t, 1, created.Revision)

	read := f.read(t, map[string]interface{}{"file": "agent/notes.md"})
	require.True(t, read.Success, read.Error)
	assert.Equal(t, "1\t# Notes\n2\tfirst\n", read.Content)
	assert.Equal(t, 1, read.File.Revision)
	assert.Equal(t, "agent/notes.md", read.File.Name)
	assert.Equal(t, 1, f.store.touched, "reads are recorded for upkeep")

	noBase := f.write(t, map[string]interface{}{"path": "agent/notes.md", "content": "replaced"})
	assert.False(t, noBase.Success)
	assert.Contains(t, noBase.Error, "base_revision")

	stale := f.write(t, map[string]interface{}{"path": "agent/notes.md", "content": "replaced", "base_revision": 7})
	assert.False(t, stale.Success)
	assert.True(t, stale.Conflict)
	assert.Equal(t, 1, stale.CurrentRevision)

	replaced := f.write(t, map[string]interface{}{"path": "agent/notes.md", "content": "replaced", "base_revision": 1})
	require.True(t, replaced.Success, replaced.Error)
	assert.Equal(t, 2, replaced.Revision)
	assert.Equal(t, "1\treplaced\n", f.read(t, map[string]interface{}{"file": "agent/notes.md"}).Content,
		"the read cache is keyed by revision, so a new revision is never served stale")
}

func TestWriteFile_AppendCreatesAndJoinsLines(t *testing.T) {
	f := newWSFixture()

	first := f.write(t, map[string]interface{}{"path": "chat/log.txt", "mode": "append", "content": "one"})
	require.True(t, first.Success, first.Error)
	assert.Equal(t, models.WorkspaceOpCreate, first.Op)

	second := f.write(t, map[string]interface{}{"path": "chat/log.txt", "mode": "append", "content": "two"})
	require.True(t, second.Success, second.Error)
	assert.Equal(t, models.WorkspaceOpAppend, second.Op)
	assert.Equal(t, 2, second.Revision)

	assert.Equal(t, "1\tone\n2\ttwo\n", f.read(t, map[string]interface{}{"file": "chat/log.txt"}).Content)

	empty := f.write(t, map[string]interface{}{"path": "chat/log.txt", "mode": "append", "content": ""})
	assert.False(t, empty.Success)
}

func TestWriteFile_ConcurrentAppendIsRefusedNotLost(t *testing.T) {
	f := newWSFixture()
	require.True(t, f.write(t, map[string]interface{}{"path": "chat/log.txt", "content": "a"}).Success)

	// Another writer lands a revision between our read and our commit.
	f.store.beforeCommit = func() {
		_, err := f.store.CommitWorkspaceRevision(context.Background(), f.chat.UserID, models.WorkspaceRevisionInput{
			Root: "chat", RootRef: f.chat.ID, Path: "log.txt", Op: models.WorkspaceOpAppend,
			BaseRevision: models.WorkspaceAnyRevision, StorageKey: "other", AuthorClass: models.WorkspaceAuthorAgent,
		})
		require.NoError(t, err)
	}
	res := f.write(t, map[string]interface{}{"path": "chat/log.txt", "mode": "append", "content": "b"})
	assert.False(t, res.Success)
	assert.True(t, res.Conflict, "the append was based on revision 1 and must not overwrite revision 2")
	assert.Equal(t, 2, res.CurrentRevision)
}

func TestWriteFile_Edit(t *testing.T) {
	f := newWSFixture()
	require.True(t, f.write(t, map[string]interface{}{"path": "agent/rules.md", "content": "call them dashboards\nuse UTC\nuse UTC"}).Success)

	ok := f.write(t, map[string]interface{}{"path": "agent/rules.md", "mode": "edit", "base_revision": 1,
		"edits": []map[string]string{{"old_text": "dashboards", "new_text": "boards"}}})
	require.True(t, ok.Success, ok.Error)
	assert.Equal(t, 2, ok.Revision)
	assert.Contains(t, f.read(t, map[string]interface{}{"file": "agent/rules.md"}).Content, "call them boards")

	ambiguous := f.write(t, map[string]interface{}{"path": "agent/rules.md", "mode": "edit", "base_revision": 2,
		"edits": []map[string]string{{"old_text": "use UTC", "new_text": "x"}}})
	assert.False(t, ambiguous.Success)
	assert.Contains(t, ambiguous.Error, "appears 2 times")

	missing := f.write(t, map[string]interface{}{"path": "agent/rules.md", "mode": "edit", "base_revision": 2,
		"edits": []map[string]string{{"old_text": "nope", "new_text": "x"}}})
	assert.Contains(t, missing.Error, "not found")

	stale := f.write(t, map[string]interface{}{"path": "agent/rules.md", "mode": "edit", "base_revision": 1,
		"edits": []map[string]string{{"old_text": "boards", "new_text": "x"}}})
	assert.True(t, stale.Conflict)

	noFile := f.write(t, map[string]interface{}{"path": "agent/none.md", "mode": "edit", "base_revision": 1,
		"edits": []map[string]string{{"old_text": "a", "new_text": "b"}}})
	assert.Contains(t, noFile.Error, "doesn't exist")
}

func TestWriteFile_DeleteAndRevive(t *testing.T) {
	f := newWSFixture()
	require.True(t, f.write(t, map[string]interface{}{"path": "chat/tmp.md", "content": "x"}).Success)

	noBase := f.write(t, map[string]interface{}{"path": "chat/tmp.md", "mode": "delete"})
	assert.False(t, noBase.Success)

	deleted := f.write(t, map[string]interface{}{"path": "chat/tmp.md", "mode": "delete", "base_revision": 1})
	require.True(t, deleted.Success, deleted.Error)
	assert.Equal(t, 2, deleted.Revision)

	gone := f.read(t, map[string]interface{}{"file": "chat/tmp.md"})
	assert.False(t, gone.Success)
	assert.Contains(t, gone.Error, "was deleted")

	revived := f.write(t, map[string]interface{}{"path": "chat/tmp.md", "content": "back"})
	require.True(t, revived.Success, revived.Error)
	assert.Equal(t, models.WorkspaceOpCreate, revived.Op)
	assert.Equal(t, 3, revived.Revision, "reviving continues the revision history")
}

func TestWriteFile_LimitsAndValidation(t *testing.T) {
	f := newWSFixture()

	tooBig := f.write(t, map[string]interface{}{"path": "chat/big.txt", "content": strings.Repeat("a", workspaceMaxWriteBytes+1)})
	assert.False(t, tooBig.Success)
	assert.Contains(t, tooBig.Error, "too large")

	noContent := f.write(t, map[string]interface{}{"path": "chat/x.txt"})
	assert.False(t, noContent.Success)

	badMode := f.write(t, map[string]interface{}{"path": "chat/x.txt", "mode": "rename", "content": "x"})
	assert.Contains(t, badMode.Error, "unknown mode")

	badPath := f.write(t, map[string]interface{}{"path": "../etc/passwd", "content": "x"})
	assert.False(t, badPath.Success)

	// Growing a file past the per-file limit through appends is refused. Each append also adds a
	// newline separator, so the last full chunk is the one that crosses the limit.
	chunk := strings.Repeat("b", workspaceMaxWriteBytes)
	for i := 0; i < workspaceMaxFileBytes/workspaceMaxWriteBytes-1; i++ {
		require.True(t, f.write(t, map[string]interface{}{"path": "chat/grow.txt", "mode": "append", "content": chunk}).Success, "append %d", i)
	}
	over := f.write(t, map[string]interface{}{"path": "chat/grow.txt", "mode": "append", "content": chunk})
	assert.False(t, over.Success)
	assert.Contains(t, over.Error, "limit")
}

func TestWriteFile_QuotaCountsFilesAndBytes(t *testing.T) {
	f := newWSFixture()
	for i := 0; i < workspaceMaxFilesPerUser; i++ {
		f.store.files[uuid.NewString()] = &models.WorkspaceFile{ID: uuid.New(), UserID: f.chat.UserID, State: models.WorkspaceFileLive, Size: 1}
	}
	res := f.write(t, map[string]interface{}{"path": "chat/one-more.md", "content": "x"})
	assert.False(t, res.Success)
	assert.Contains(t, res.Error, "workspace is full")
}

func TestWriteFile_FailedCommitRemovesUploadedObject(t *testing.T) {
	f := newWSFixture()
	require.True(t, f.write(t, map[string]interface{}{"path": "chat/a.md", "content": "v1"}).Success)
	before := len(f.objects.objects)

	f.store.beforeCommit = func() {
		_, _ = f.store.CommitWorkspaceRevision(context.Background(), f.chat.UserID, models.WorkspaceRevisionInput{
			Root: "chat", RootRef: f.chat.ID, Path: "a.md", Op: models.WorkspaceOpWrite,
			BaseRevision: models.WorkspaceAnyRevision, StorageKey: "racer", AuthorClass: models.WorkspaceAuthorAgent,
		})
	}
	res := f.write(t, map[string]interface{}{"path": "chat/a.md", "content": "v2", "base_revision": 1})
	assert.True(t, res.Conflict)
	assert.Equal(t, before, len(f.objects.objects), "the object of the refused write is deleted")

	f.objects.failNextU = true
	failed := f.write(t, map[string]interface{}{"path": "chat/b.md", "content": "x"})
	assert.False(t, failed.Success)
	assert.Contains(t, failed.Error, "failed to save")
}

func TestWorkspace_ObjectKeysStayUnderUserPrefix(t *testing.T) {
	f := newWSFixture()
	require.True(t, f.write(t, map[string]interface{}{"path": "chat/a.md", "content": "x"}).Success)
	for key := range f.objects.objects {
		assert.True(t, strings.HasPrefix(key, WorkspaceObjectPrefix(f.chat.UserID)), key)
	}
}

func TestGrepAndList_IncludeWorkspace(t *testing.T) {
	f := newWSFixture()
	require.True(t, f.write(t, map[string]interface{}{"path": "agent/recipes/chart-relay.md", "content": "steps\nset thread_ts first"}).Success)
	require.True(t, f.write(t, map[string]interface{}{"path": "chat/scratch.txt", "content": "nothing"}).Success)

	in, _ := json.Marshal(map[string]interface{}{"pattern": "thread_ts"})
	out, err := f.reader.GrepFiles(context.Background(), f.chat, in)
	require.NoError(t, err)
	var grep grepFilesResult
	require.NoError(t, json.Unmarshal([]byte(out), &grep))
	require.Len(t, grep.Matches, 1)
	assert.Equal(t, "agent/recipes/chart-relay.md", grep.Matches[0].File)
	assert.Equal(t, 2, grep.Matches[0].Line)

	named, _ := json.Marshal(map[string]interface{}{"pattern": "nothing", "files": []string{"chat/scratch.txt"}})
	out, err = f.reader.GrepFiles(context.Background(), f.chat, named)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(out), &grep))
	require.Len(t, grep.Matches, 1)

	listIn, _ := json.Marshal(map[string]interface{}{"kind": "workspace", "filter": "agent/"})
	out, err = f.list.List(context.Background(), f.chat, listIn)
	require.NoError(t, err)
	var listed listResult
	require.NoError(t, json.Unmarshal([]byte(out), &listed))
	require.Len(t, listed.Items, 1)
	assert.Equal(t, "agent/recipes/chart-relay.md", listed.Items[0].Name)
	assert.Equal(t, 1, listed.Items[0].Revision)

	all, _ := json.Marshal(map[string]interface{}{"kind": "workspace"})
	out, err = f.list.List(context.Background(), f.chat, all)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(out), &listed))
	assert.Len(t, listed.Items, 2)
}

func TestWorkspace_OtherConversationAndPersonalityAreIsolated(t *testing.T) {
	f := newWSFixture()
	require.True(t, f.write(t, map[string]interface{}{"path": "chat/private.md", "content": "secret"}).Success)
	require.True(t, f.write(t, map[string]interface{}{"path": "agent/notebook.md", "content": "mine"}).Success)

	otherChat := *f.chat
	otherChat.ID = uuid.New()
	f.chat = &otherChat
	assert.False(t, f.read(t, map[string]interface{}{"file": "chat/private.md"}).Success, "chat/ is per conversation")
	assert.True(t, f.read(t, map[string]interface{}{"file": "agent/notebook.md"}).Success, "agent/ is shared by the personality's conversations")

	otherPersona := otherChat
	otherPersona.PersonalityID = uuid.New()
	f.chat = &otherPersona
	assert.False(t, f.read(t, map[string]interface{}{"file": "agent/notebook.md"}).Success, "another personality has its own notebook")
}

func TestWriteFile_ConcurrentCreateDoesNotReplaceTheFirst(t *testing.T) {
	f := newWSFixture()
	// Another writer creates the file between our existence check and our commit.
	f.store.beforeCommit = func() {
		_, err := f.store.CommitWorkspaceRevision(context.Background(), f.chat.UserID, models.WorkspaceRevisionInput{
			Root: "chat", RootRef: f.chat.ID, Path: "new.md", Op: models.WorkspaceOpCreate,
			BaseRevision: models.WorkspaceAnyRevision, StorageKey: "first", AuthorClass: models.WorkspaceAuthorAgent,
		})
		require.NoError(t, err)
	}
	res := f.write(t, map[string]interface{}{"path": "chat/new.md", "content": "second"})
	assert.False(t, res.Success)
	assert.True(t, res.Conflict)
	got, err := f.store.GetWorkspaceFile(context.Background(), f.chat.UserID, "chat", f.chat.ID, "new.md")
	require.NoError(t, err)
	assert.Equal(t, "first", got.StorageKey)
}

func TestWriteFile_RejectsNonPositiveBaseRevision(t *testing.T) {
	f := newWSFixture()
	require.True(t, f.write(t, map[string]interface{}{"path": "agent/a.md", "content": "v1"}).Success)
	for _, base := range []int{-1, 0} {
		res := f.write(t, map[string]interface{}{"path": "agent/a.md", "content": "v2", "base_revision": base})
		assert.False(t, res.Success, "base_revision %d must not bypass the stale-write check", base)
		assert.Contains(t, res.Error, "1 or higher")
		del := f.write(t, map[string]interface{}{"path": "agent/a.md", "mode": "delete", "base_revision": base})
		assert.False(t, del.Success)
	}
	assert.Equal(t, "1\tv1\n", f.read(t, map[string]interface{}{"file": "agent/a.md"}).Content)
}

func TestWorkspace_IDsArePathsThatRoundTrip(t *testing.T) {
	f := newWSFixture()
	require.True(t, f.write(t, map[string]interface{}{"path": "chat/plan.md", "content": "step one"}).Success)

	read := f.read(t, map[string]interface{}{"file": "chat/plan.md"})
	require.True(t, read.Success)
	assert.Equal(t, "chat/plan.md", read.File.ID)
	again := f.read(t, map[string]interface{}{"file": read.File.ID})
	assert.True(t, again.Success, "the id a result shows can be passed straight back")

	listIn, _ := json.Marshal(map[string]interface{}{"kind": "workspace"})
	out, err := f.list.List(context.Background(), f.chat, listIn)
	require.NoError(t, err)
	var listed listResult
	require.NoError(t, json.Unmarshal([]byte(out), &listed))
	require.Len(t, listed.Items, 1)
	assert.Equal(t, "chat/plan.md", listed.Items[0].ID)
}

func TestWorkspace_PurgeRootRemovesFilesAndObjects(t *testing.T) {
	f := newWSFixture()
	require.True(t, f.write(t, map[string]interface{}{"path": "chat/a.md", "content": "one"}).Success)
	require.True(t, f.write(t, map[string]interface{}{"path": "chat/a.md", "mode": "append", "content": "two"}).Success)
	require.True(t, f.write(t, map[string]interface{}{"path": "agent/keep.md", "content": "notebook"}).Success)
	require.Len(t, f.objects.objects, 3)

	require.NoError(t, f.ws.PurgeRoot(context.Background(), f.chat.UserID, models.WorkspaceRootChat, f.chat.ID))
	assert.False(t, f.read(t, map[string]interface{}{"file": "chat/a.md"}).Success)
	assert.True(t, f.read(t, map[string]interface{}{"file": "agent/keep.md"}).Success, "other roots are untouched")
	assert.Len(t, f.objects.objects, 1, "every revision object of the purged root is deleted")
}
