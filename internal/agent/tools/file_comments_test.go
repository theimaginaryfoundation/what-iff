package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/filecomments"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
	"go.uber.org/zap"
)

// memComments is an in-memory fileCommentsService: one text per file, threads placed on it as the
// real service places them.
type memComments struct {
	files   *fakeAgentFileStore
	text    map[uuid.UUID]string
	threads map[uuid.UUID][]models.FileCommentThread
}

func (m *memComments) Read(_ context.Context, _, id uuid.UUID) (*models.FileComments, error) {
	return &models.FileComments{Threads: m.threads[id], ContentVersion: 1}, nil
}

func (m *memComments) Update(ctx context.Context, userID, id uuid.UUID, change func(*filecomments.Current) ([]models.FileCommentThread, error)) (*models.FileComments, error) {
	file, err := m.files.GetFileAttachment(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	image := file.FileType == "image/png"
	threads, err := change(&filecomments.Current{File: file, Text: m.text[id], Image: image, Threads: m.threads[id]})
	if err != nil {
		return nil, err
	}
	m.threads[id] = filecomments.Place(threads, m.text[id])
	return &models.FileComments{Threads: m.threads[id], ContentVersion: 1}, nil
}

func runComments(t *testing.T, tool *FileCommentsTool, chat *models.Chat, args map[string]any) fileCommentsResult {
	t.Helper()
	in, _ := json.Marshal(args)
	out, err := tool.Run(context.Background(), chat, in)
	require.NoError(t, err)
	var res fileCommentsResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res
}

func TestFileCommentsTool(t *testing.T) {
	userID, personaID := uuid.New(), uuid.New()
	store := &fakeAgentFileStore{userID: userID, rows: map[uuid.UUID]*models.FileAttachment{}}
	plan, _ := store.CreateFileAttachment(context.Background(), userID, models.FileAttachment{Name: "plan.md", Folder: "notes", FileType: "text/markdown"})
	photo, _ := store.CreateFileAttachment(context.Background(), userID, models.FileAttachment{Name: "sky.png", FileType: "image/png"})
	const text = "# Plan\n\nShip three major features.\nThen ship more features.\n"
	mem := &memComments{files: store, text: map[uuid.UUID]string{plan.ID: text}, threads: map[uuid.UUID][]models.FileCommentThread{
		plan.ID: {{ID: "t1", Anchor: &models.FileCommentRange{Start: 19, End: 33}, Quote: "major features",
			Comments: []models.FileComment{{ID: "t1", AuthorID: "user:u", AuthorName: "Pat", Text: "@Iris which three?"}}}},
	}}
	tool := &FileCommentsTool{files: store, comments: mem, logger: zap.NewNop(), now: func() time.Time { return time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC) }}
	chat := &models.Chat{ID: uuid.New(), UserID: userID, PersonalityID: personaID, PersonalityName: "Iris"}

	// list: by path, with what each thread is on and its lines.
	res := runComments(t, tool, chat, map[string]any{"action": "list", "file": "notes/plan.md"})
	require.Empty(t, res.Error)
	require.Equal(t, plan.ID.String(), res.File.ID)
	require.Len(t, res.Threads, 1)
	require.Equal(t, fileThreadView{ID: "t1", On: "major features", FirstLine: 3, LastLine: 3,
		Comments: []fileCommentView{{ID: "t1", Author: "Pat", Text: "@Iris which three?"}}}, res.Threads[0])

	// reply: written as the personality, and linked.
	res = runComments(t, tool, chat, map[string]any{"action": "reply", "file": plan.ID.String(), "thread_id": "t1", "text": "Comments, sheets and slides."})
	require.Empty(t, res.Error)
	reply := mem.threads[plan.ID][0].Comments[1]
	require.Equal(t, "agent:"+personaID.String(), reply.AuthorID)
	require.Equal(t, "Iris", reply.AuthorName)
	require.Equal(t, "t1", res.CommentRef.ThreadID)
	require.Equal(t, reply.ID, res.CommentRef.CommentID)
	require.Equal(t, "plan.md", res.CommentRef.FileName)
	require.Equal(t, "major features", res.CommentRef.Quote)

	// add: placed on the quoted text; a quote found twice needs occurrence; one not found is refused.
	res = runComments(t, tool, chat, map[string]any{"action": "add", "file": plan.ID.String(), "quote": "features", "text": "Which ones?"})
	require.Contains(t, res.Error, "more than once")
	res = runComments(t, tool, chat, map[string]any{"action": "add", "file": plan.ID.String(), "quote": "features", "occurrence": 2, "text": "Which ones?"})
	require.Empty(t, res.Error)
	added := mem.threads[plan.ID][1]
	require.Equal(t, "features", added.Quote)
	first, _ := filecomments.Lines(text, *added.Anchor)
	require.Equal(t, 4, first)
	res = runComments(t, tool, chat, map[string]any{"action": "add", "file": plan.ID.String(), "quote": "not there", "text": "?"})
	require.Contains(t, res.Error, "not in the file")

	// resolve, and an unknown thread.
	res = runComments(t, tool, chat, map[string]any{"action": "resolve", "file": plan.ID.String(), "thread_id": "t1", "resolved": true})
	require.Empty(t, res.Error)
	require.True(t, mem.threads[plan.ID][0].Resolved)
	res = runComments(t, tool, chat, map[string]any{"action": "reply", "file": plan.ID.String(), "thread_id": "nope", "text": "x"})
	require.Contains(t, res.Error, "no thread")

	// An image's threads are on the whole image.
	res = runComments(t, tool, chat, map[string]any{"action": "add", "file": "sky.png", "quote": "sun", "text": "Smaller?"})
	require.Contains(t, res.Error, "whole image")
	res = runComments(t, tool, chat, map[string]any{"action": "add", "file": "sky.png", "text": "Smaller sun?"})
	require.Empty(t, res.Error)
	require.Nil(t, mem.threads[photo.ID][0].Anchor)
	require.Empty(t, res.CommentRef.Quote)
}

func TestCommentRefsFromCalls(t *testing.T) {
	fileID := uuid.New()
	out := func(thread, comment string) string {
		b, _ := json.Marshal(fileCommentsResult{CommentRef: &modeltypes.CommentRef{FileID: fileID, FileName: "plan.md", ThreadID: thread, CommentID: comment}})
		return string(b)
	}
	calls := []*models.ToolCall{
		{ToolName: fileCommentsToolName, ToolOutput: out("t1", "c1")},
		{ToolName: "agent_file_read", ToolOutput: out("t9", "c9")},
		{ToolName: fileCommentsToolName, ToolOutput: out("t2", "c2")},
		{ToolName: fileCommentsToolName, ToolOutput: out("t1", "c3")},
		{ToolName: fileCommentsToolName, ToolOutput: `{"error":"no thread"}`},
	}
	refs := CommentRefsFromCalls(calls)
	require.Len(t, refs, 2)
	require.Equal(t, "c3", refs[0].CommentID, "one link per thread, to its last comment")
	require.Equal(t, "t2", refs[1].ThreadID)
}
