package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/filecomments"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
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
	diagram := file.FileType == "application/vnd.jgraph.mxfile"
	threads, err := change(&filecomments.Current{File: file, Text: m.text[id], Image: image, Diagram: diagram, Threads: m.threads[id]})
	if err != nil {
		return nil, err
	}
	if diagram {
		m.threads[id] = filecomments.OnCells(threads, m.text[id])
	} else {
		m.threads[id] = filecomments.Place(threads, m.text[id])
	}
	return &models.FileComments{Threads: m.threads[id], ContentVersion: 1}, nil
}

func runComments(t *testing.T, tool *FileCommentsTool, chat *models.Chat, args map[string]any) fileCommentsResult {
	t.Helper()
	in, _ := json.Marshal(args)
	out, err := tool.Run(appTurn, chat, in)
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

func TestFileCommentsTool_Diagrams(t *testing.T) {
	userID := uuid.New()
	store := &fakeAgentFileStore{userID: userID, rows: map[uuid.UUID]*models.FileAttachment{}}
	flow, _ := store.CreateFileAttachment(context.Background(), userID, models.FileAttachment{Name: "flow.drawio", FileType: "application/vnd.jgraph.mxfile"})
	const xml = `<mxfile><diagram id="p1" name="Main"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/>` +
		`<mxCell id="a" value="User" vertex="1" parent="1"><mxGeometry width="80" height="40" as="geometry"/></mxCell>` +
		`</root></mxGraphModel></diagram></mxfile>`
	mem := &memComments{files: store, text: map[uuid.UUID]string{flow.ID: xml}, threads: map[uuid.UUID][]models.FileCommentThread{}}
	tool := &FileCommentsTool{files: store, comments: mem, logger: zap.NewNop(), now: time.Now}
	chat := &models.Chat{ID: uuid.New(), UserID: userID, PersonalityName: "Iris"}

	// add on an element, by its id and its page's name: the thread quotes its label.
	res := runComments(t, tool, chat, map[string]any{"action": "add", "file": flow.ID.String(), "cell": "a", "page": "Main", "text": "Who is this?"})
	require.Empty(t, res.Error)
	th := mem.threads[flow.ID][0]
	require.Equal(t, &models.FileCommentCell{ID: "a", Page: "p1"}, th.Cell)
	require.Equal(t, "User", res.CommentRef.Quote)

	// list says which element.
	res = runComments(t, tool, chat, map[string]any{"action": "list", "file": flow.ID.String()})
	require.Equal(t, "User", res.Threads[0].On)
	require.Equal(t, "element a on page p1", res.Threads[0].Where)

	// An element that is not there, or a quote, is refused with what to do instead.
	res = runComments(t, tool, chat, map[string]any{"action": "add", "file": flow.ID.String(), "cell": "zz", "text": "?"})
	require.Contains(t, res.Error, "diagram tool")
	res = runComments(t, tool, chat, map[string]any{"action": "add", "file": flow.ID.String(), "quote": "User", "text": "?"})
	require.Contains(t, res.Error, "give cell")
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

// fakeOffice is an in-memory Office service: threads per file, changed through a fake format.
type fakeOffice struct {
	threads  []officecomments.Thread
	adds     []officecomments.Anchor
	chatIDs  []uuid.UUID
	resolved map[string]bool
}

func (f *fakeOffice) FormatOf(file *models.FileAttachment) officecomments.Format {
	if strings.HasSuffix(file.Name, ".docx") {
		return fakeFormat{f}
	}
	return nil
}
func (f *fakeOffice) List(context.Context, uuid.UUID, uuid.UUID) ([]officecomments.Thread, error) {
	return f.threads, nil
}
func (f *fakeOffice) Change(_ context.Context, _ uuid.UUID, chatID *uuid.UUID, _ uuid.UUID, change func(officecomments.Format, []byte) ([]byte, string, error)) (string, error) {
	f.chatIDs = append(f.chatIDs, *chatID)
	_, id, err := change(fakeFormat{f}, nil)
	return id, err
}

type fakeFormat struct{ f *fakeOffice }

func (fakeFormat) List([]byte) ([]officecomments.Thread, error) { return nil, nil }
func (ff fakeFormat) Add(_ []byte, at officecomments.Anchor, c officecomments.Comment) ([]byte, string, error) {
	if at.Quote == "" {
		return nil, "", officecomments.ErrBadAnchor
	}
	ff.f.adds = append(ff.f.adds, at)
	c.ID = "w2"
	ff.f.threads = append(ff.f.threads, officecomments.Thread{ID: "w2", On: at.Quote, Where: "paragraph 3", Comments: []officecomments.Comment{c}})
	return nil, "w2", nil
}
func (ff fakeFormat) Reply(_ []byte, id string, c officecomments.Comment) ([]byte, error) {
	for i := range ff.f.threads {
		if ff.f.threads[i].ID == id {
			c.ID = "w9"
			ff.f.threads[i].Comments = append(ff.f.threads[i].Comments, c)
			return nil, nil
		}
	}
	return nil, officecomments.ErrNoThread
}
func (ff fakeFormat) Resolve(_ []byte, id string, resolved bool) ([]byte, error) {
	for i := range ff.f.threads {
		if ff.f.threads[i].ID == id {
			ff.f.threads[i].Resolved = resolved
			return nil, nil
		}
	}
	return nil, officecomments.ErrNoThread
}

func TestFileCommentsTool_OfficeFiles(t *testing.T) {
	userID := uuid.New()
	store := &fakeAgentFileStore{userID: userID, rows: map[uuid.UUID]*models.FileAttachment{}}
	doc, _ := store.CreateFileAttachment(context.Background(), userID, models.FileAttachment{Name: "brief.docx", FileType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"})
	office := &fakeOffice{threads: []officecomments.Thread{{ID: "w1", On: "ship it", Where: "paragraph 2",
		Comments: []officecomments.Comment{{ID: "w1", AuthorName: "Pat", Text: "@Iris when?"}}}}}
	tool := (&FileCommentsTool{files: store, grants: store, logger: zap.NewNop(), now: time.Now}).WithOffice(office)
	chat := &models.Chat{ID: uuid.New(), UserID: userID, PersonalityID: uuid.New(), PersonalityName: "Iris"}

	res := runComments(t, tool, chat, map[string]any{"action": "list", "file": "brief.docx"})
	require.Equal(t, []fileThreadView{{ID: "w1", On: "ship it", Where: "paragraph 2", Comments: []fileCommentView{{ID: "w1", Author: "Pat", Text: "@Iris when?"}}}}, res.Threads)

	// Changing an Office file's comments rewrites the file: it takes the grant a mention gives.
	res = runComments(t, tool, chat, map[string]any{"action": "reply", "file": "brief.docx", "thread_id": "w1", "text": "Friday."})
	require.Contains(t, res.Error, "only after the user mentions you")
	require.Empty(t, office.chatIDs, "nothing written")
	store.grants = map[[2]uuid.UUID]bool{{chat.ID, doc.ID}: true}

	res = runComments(t, tool, chat, map[string]any{"action": "reply", "file": "brief.docx", "thread_id": "w1", "text": "Friday."})
	require.Empty(t, res.Error)
	require.Equal(t, "Iris", office.threads[0].Comments[1].AuthorName)
	require.Equal(t, "agent:"+chat.PersonalityID.String(), office.threads[0].Comments[1].AuthorID)
	require.Equal(t, "w9", res.CommentRef.CommentID)
	require.Equal(t, "ship it", res.CommentRef.Quote)
	require.Equal(t, chat.ID, office.chatIDs[0], "saved from this chat")

	res = runComments(t, tool, chat, map[string]any{"action": "add", "file": "brief.docx", "quote": "the plan", "text": "Which plan?"})
	require.Empty(t, res.Error)
	require.Equal(t, "w2", res.CommentRef.ThreadID)
	require.Equal(t, "paragraph 3", res.Thread.Where)
	res = runComments(t, tool, chat, map[string]any{"action": "add", "file": "brief.docx", "text": "On the file"})
	require.Contains(t, res.Error, "Word needs a quote")
	res = runComments(t, tool, chat, map[string]any{"action": "resolve", "file": "brief.docx", "thread_id": "nope", "resolved": true})
	require.Contains(t, res.Error, "no thread")
	res = runComments(t, tool, chat, map[string]any{"action": "resolve", "file": "brief.docx", "thread_id": "w1", "resolved": true})
	require.Empty(t, res.Error)
	require.True(t, office.threads[0].Resolved)
	_ = doc
}
