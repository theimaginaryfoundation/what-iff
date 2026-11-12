package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/filecomments"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// file_comments (ADR 0x046): the agent reads and writes the comment threads on the user's files,
// the same threads people make in the comments panel. It writes as `agent:<personality id>` under
// the personality's name. Every change goes through filecomments.Service.Update, so a file changed
// meanwhile is read again rather than overwritten. Comments are not the file's content, so the
// agent may comment on any of the user's files, not only its own.

const fileCommentsToolName = "file_comments"

// fileCommentMaxText caps one comment the agent writes.
const fileCommentMaxText = 8000

// FileCommentsDescription is the agent-facing description of file_comments.
const FileCommentsDescription = `Read and write the comment threads on a file: remarks people leave on parts of it, as in a word processor, with replies. Works on text and code files and images (an image's threads are on the whole image).

action:
- "list": the file's threads: each one's id, what it is on (the quoted text with its lines, or the whole file), whether it is resolved, and its comments.
- "reply": add text to the thread thread_id.
- "add": start a thread with text. quote is the exact text of the file the thread is about, copied from the file (a few words or a line, enough to be unique); when the same text appears more than once, occurrence picks one (1 = first). Leave quote empty for a thread on the whole file.
- "resolve": mark thread_id resolved (resolved=true), or open it again (resolved=false).

Comments are plain text, shown as written: no Markdown (no **bold**, lists or headings). Keep them short, like a reviewer's note.

A comment that mentions you arrives as a message that says which file and thread it is on. When your answer is about the file, reply in that thread (and still answer briefly in the chat); make changes the comment asks for in the file itself. The user sees your comments beside the file and as links on your message.

file is a file id, or a path such as "notes/plan.md".`

// FileCommentsToolSpec is the shared function-tool shape for file_comments.
var FileCommentsToolSpec = FunctionToolSpec{
	Name:        fileCommentsToolName,
	Description: FileCommentsDescription,
	Properties: map[string]interface{}{
		"action":     map[string]interface{}{"type": "string", "enum": []string{"list", "reply", "add", "resolve"}},
		"file":       map[string]interface{}{"type": "string", "description": "A file id, or a path such as 'notes/plan.md'."},
		"thread_id":  map[string]interface{}{"type": "string", "description": "The thread (reply, resolve)."},
		"text":       map[string]interface{}{"type": "string", "description": "The comment (reply, add)."},
		"quote":      map[string]interface{}{"type": "string", "description": "Exact text of the file the new thread is on (add). Empty for the whole file."},
		"occurrence": map[string]interface{}{"type": "integer", "minimum": 1, "description": "Which appearance of quote, when it appears more than once (add). 1 = first."},
		"resolved":   map[string]interface{}{"type": "boolean", "description": "Resolve (true) or reopen (false) the thread (resolve)."},
	},
	Required: []string{"action", "file"},
}

type fileCommentsArgs struct {
	Action     string `json:"action"`
	File       string `json:"file"`
	ThreadID   string `json:"thread_id"`
	Text       string `json:"text"`
	Quote      string `json:"quote"`
	Occurrence int    `json:"occurrence"`
	Resolved   *bool  `json:"resolved"`
}

type fileCommentView struct {
	ID     string `json:"id"`
	Author string `json:"author"`
	Text   string `json:"text"`
}

type fileThreadView struct {
	ID string `json:"id"`
	// On is what the thread is on: the quoted text, or "the whole file".
	On        string            `json:"on"`
	FirstLine int               `json:"first_line,omitempty"`
	LastLine  int               `json:"last_line,omitempty"`
	Resolved  bool              `json:"resolved,omitempty"`
	Comments  []fileCommentView `json:"comments"`
}

type fileCommentsResult struct {
	File    *agentFileRef    `json:"file,omitempty"`
	Threads []fileThreadView `json:"threads,omitempty"`
	Thread  *fileThreadView  `json:"thread,omitempty"`
	// CommentRef is the comment a write made, which the reply links to (saved with the message).
	CommentRef *modeltypes.CommentRef `json:"comment_ref,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

// fileCommentsService is the part of filecomments.Service the tool uses.
type fileCommentsService interface {
	Read(ctx context.Context, userID, id uuid.UUID) (*models.FileComments, error)
	Update(ctx context.Context, userID, id uuid.UUID, change func(*filecomments.Current) ([]models.FileCommentThread, error)) (*models.FileComments, error)
}

// FileCommentsTool implements file_comments.
type FileCommentsTool struct {
	files    UserFileLookup
	comments fileCommentsService
	logger   *zap.Logger
	now      func() time.Time
}

// NewFileCommentsTool constructs a FileCommentsTool over the datastore and object store.
func NewFileCommentsTool(ds *datastore.Datastore, files storage.FileStore, logger *zap.Logger) *FileCommentsTool {
	return &FileCommentsTool{files: ds, comments: filecomments.New(ds, files), logger: logger, now: time.Now}
}

// AgentCommentAuthor is who the agent in chat writes comments as.
func AgentCommentAuthor(chat *models.Chat) (id, name string) {
	id = "agent:chat"
	if chat.PersonalityID != uuid.Nil {
		id = "agent:" + chat.PersonalityID.String()
	}
	name = strings.TrimSpace(chat.PersonalityName)
	if name == "" {
		name = "Assistant"
	}
	return id, name
}

// Run handles one file_comments call.
func (t *FileCommentsTool) Run(ctx context.Context, chat *models.Chat, input []byte) (string, error) {
	var args fileCommentsArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return t.refuse(fmt.Sprintf("invalid arguments: %v", err))
	}
	att, err := ResolveUserFile(ctx, t.files, chat.UserID, args.File, nil)
	if err != nil {
		var refusal FileRefusal
		if errors.As(err, &refusal) {
			return t.refuse(refusal.Error())
		}
		return "", err
	}
	ref := &agentFileRef{ID: att.ID.String(), Path: FilePath(att)}
	var res fileCommentsResult
	switch args.Action {
	case "list":
		res, err = t.list(ctx, chat, att)
	case "reply", "add", "resolve":
		res, err = t.change(ctx, chat, att, args)
	default:
		return t.refuse(`action must be "list", "reply", "add" or "resolve"`)
	}
	if msg, ok := refusalOf(err); ok {
		return t.refuse(msg)
	}
	if err != nil {
		return "", err
	}
	res.File = ref
	return marshalToolResult(res, fileCommentsToolName)
}

func (t *FileCommentsTool) refuse(msg string) (string, error) {
	return marshalToolResult(fileCommentsResult{Error: msg}, fileCommentsToolName)
}

// refusalOf turns the errors the agent can act on into the message it is told.
func refusalOf(err error) (string, bool) {
	var refusal FileRefusal
	switch {
	case err == nil:
		return "", false
	case errors.As(err, &refusal):
		return refusal.Error(), true
	case errors.Is(err, filecomments.ErrNotText):
		return "comments can be read and written here only on text, code and image files", true
	case errors.Is(err, filecomments.ErrQuoteNotFound):
		return err.Error() + "; copy the quote exactly from the file (read it with agent_file_read)", true
	case errors.Is(err, filecomments.ErrQuoteAmbiguous):
		return err.Error() + "; quote more of the text, or give occurrence", true
	case errors.Is(err, filecomments.ErrRevision), errors.Is(err, filecomments.ErrStaleContent):
		return "the file or its comments are changing right now; try again", true
	}
	return "", false
}

func (t *FileCommentsTool) list(ctx context.Context, chat *models.Chat, att *models.FileAttachment) (fileCommentsResult, error) {
	var res fileCommentsResult
	_, err := t.comments.Update(ctx, chat.UserID, att.ID, func(cur *filecomments.Current) ([]models.FileCommentThread, error) {
		res.Threads = make([]fileThreadView, 0, len(cur.Threads))
		for _, th := range cur.Threads {
			res.Threads = append(res.Threads, threadView(th, cur.Text))
		}
		return nil, errListed
	})
	if errors.Is(err, errListed) {
		err = nil
	}
	return res, err
}

// errListed ends a list's Update before it writes anything.
var errListed = errors.New("listed")

func threadView(th models.FileCommentThread, text string) fileThreadView {
	v := fileThreadView{ID: th.ID, On: "the whole file", Resolved: th.Resolved}
	if th.Anchor != nil {
		v.On = th.Quote
		v.FirstLine, v.LastLine = filecomments.Lines(text, *th.Anchor)
	} else if th.Quote != "" {
		v.On = fmt.Sprintf("the whole file (it was on %q, which is gone)", th.Quote)
	}
	for _, c := range th.Comments {
		v.Comments = append(v.Comments, fileCommentView{ID: c.ID, Author: c.AuthorName, Text: c.Text})
	}
	return v
}

func (t *FileCommentsTool) change(ctx context.Context, chat *models.Chat, att *models.FileAttachment, args fileCommentsArgs) (fileCommentsResult, error) {
	text := strings.TrimSpace(args.Text)
	if args.Action != "resolve" {
		if text == "" {
			return fileCommentsResult{}, fileRefusalf("text is required")
		}
		if len(text) > fileCommentMaxText {
			return fileCommentsResult{}, fileRefusalf("a comment can be at most %d characters", fileCommentMaxText)
		}
	}
	authorID, authorName := AgentCommentAuthor(chat)
	comment := models.FileComment{ID: uuid.NewString(), AuthorID: authorID, AuthorName: authorName, CreatedAt: t.now().UTC(), Text: text}
	var threadID, commentID string
	got, err := t.comments.Update(ctx, chat.UserID, att.ID, func(cur *filecomments.Current) ([]models.FileCommentThread, error) {
		threads := append([]models.FileCommentThread(nil), cur.Threads...)
		switch args.Action {
		case "add":
			th := models.FileCommentThread{ID: comment.ID, Comments: []models.FileComment{comment}}
			if q := args.Quote; strings.TrimSpace(q) != "" {
				if cur.Image {
					return nil, fileRefusalf("an image's threads are on the whole image; leave quote empty")
				}
				at, err := filecomments.AnchorOn(cur.Text, q, args.Occurrence)
				if err != nil {
					return nil, err
				}
				th.Anchor = at
			}
			threadID, commentID = th.ID, comment.ID
			return append(threads, th), nil
		case "reply", "resolve":
			i := indexOfThread(threads, args.ThreadID)
			if i < 0 {
				return nil, fileRefusalf("no thread %q on this file; list its threads first", args.ThreadID)
			}
			th := threads[i]
			th.Comments = append([]models.FileComment(nil), th.Comments...)
			if args.Action == "reply" {
				th.Comments = append(th.Comments, comment)
				commentID = comment.ID
			} else {
				if args.Resolved == nil {
					return nil, fileRefusalf("resolved is required: true to resolve, false to reopen")
				}
				th.Resolved = *args.Resolved
				commentID = th.ID
			}
			threads[i] = th
			threadID = th.ID
			return threads, nil
		}
		return nil, fileRefusalf("unknown action %q", args.Action)
	})
	if err != nil {
		return fileCommentsResult{}, err
	}
	i := indexOfThread(got.Threads, threadID)
	if i < 0 {
		return fileCommentsResult{}, fmt.Errorf("thread %s missing after write", threadID)
	}
	th := got.Threads[i]
	// The written thread, as list shows it (its lines are read again on the next list).
	view := threadView(th, "")
	quote := ""
	if th.Anchor != nil {
		quote = filecomments.ShortQuote(th.Quote)
	}
	return fileCommentsResult{
		Thread:     &view,
		CommentRef: &modeltypes.CommentRef{FileID: att.ID, FileName: att.Name, ThreadID: th.ID, CommentID: commentID, Quote: quote},
	}, nil
}

func indexOfThread(threads []models.FileCommentThread, id string) int {
	for i, th := range threads {
		if th.ID == id {
			return i
		}
	}
	return -1
}

// CommentRefsFromCalls are the comments a turn's file_comments calls made, for its reply to link
// to (ADR 0x046): one per thread, the last comment made on it.
func CommentRefsFromCalls(calls []*models.ToolCall) []modeltypes.CommentRef {
	var out []modeltypes.CommentRef
	seen := map[string]int{}
	for _, c := range calls {
		if c == nil || c.ToolName != fileCommentsToolName || c.ToolError != "" {
			continue
		}
		var res fileCommentsResult
		if json.Unmarshal([]byte(c.ToolOutput), &res) != nil || res.CommentRef == nil {
			continue
		}
		key := res.CommentRef.FileID.String() + "#" + res.CommentRef.ThreadID
		if i, ok := seen[key]; ok {
			out[i] = *res.CommentRef
			continue
		}
		seen[key] = len(out)
		out = append(out, *res.CommentRef)
	}
	return out
}
