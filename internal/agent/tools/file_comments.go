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
	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
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
const FileCommentsDescription = `Read and write the comment threads on a file: remarks people leave on parts of it, as in a word processor, with replies. Works on text and code files, images (an image's threads are on the whole image), and Word, PowerPoint and Excel files (their comments are saved in the file, as a new version of it).

action:
- "list": the file's threads: each one's id, what it is on (the quoted text with its lines, or the whole file), whether it is resolved, and its comments.
- "reply": add text to the thread thread_id.
- "add": start a thread with text. quote is the exact text of the file the thread is about, copied from the file (a few words or a line, enough to be unique); when the same text appears more than once, occurrence picks one (1 = first). Leave quote empty for a thread on the whole file (not in Word, where a comment needs text). PowerPoint: quote is text in a shape, and slide (1-based) limits it to one slide; slide alone puts the thread on the slide. Excel: cell (e.g. "B3") on sheet (its name; empty for the first), one thread per cell (reply to an existing one instead).
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
		"slide":      map[string]interface{}{"type": "integer", "minimum": 1, "description": "PowerPoint (add): the slide, 1-based."},
		"sheet":      map[string]interface{}{"type": "string", "description": "Excel (add): the sheet's name; empty for the first sheet."},
		"cell":       map[string]interface{}{"type": "string", "description": "Excel (add): the cell, e.g. 'B3'."},
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
	Slide      int    `json:"slide"`
	Sheet      string `json:"sheet"`
	Cell       string `json:"cell"`
}

type fileCommentView struct {
	ID     string `json:"id"`
	Author string `json:"author"`
	Text   string `json:"text"`
}

type fileThreadView struct {
	ID string `json:"id"`
	// On is what the thread is on: the quoted text, or "the whole file".
	On string `json:"on"`
	// Where is where that is in an Office file ("slide 2", "sheet Sheet1, cell B3").
	Where     string            `json:"where,omitempty"`
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
	files UserFileLookup
	// grants says whether the agent may rewrite an Office file to change its comments (ADR 0x046).
	grants   fileEditGrants
	comments fileCommentsService
	// office reads and writes the comments inside Word, PowerPoint and Excel files; nil leaves
	// them unsupported.
	office officeComments
	logger *zap.Logger
	now    func() time.Time
}

// officeComments is the part of filecomments.Office the tool uses.
type officeComments interface {
	FormatOf(file *models.FileAttachment) officecomments.Format
	List(ctx context.Context, userID, id uuid.UUID) ([]officecomments.Thread, error)
	Change(ctx context.Context, userID uuid.UUID, chatID *uuid.UUID, id uuid.UUID, change func(officecomments.Format, []byte) ([]byte, string, error)) (string, error)
}

// WithOffice sets how the tool reads and writes comments in Office files.
func (t *FileCommentsTool) WithOffice(office officeComments) *FileCommentsTool {
	t.office = office
	return t
}

// NewFileCommentsTool constructs a FileCommentsTool over the datastore and object store.
func NewFileCommentsTool(ds *datastore.Datastore, files storage.FileStore, logger *zap.Logger) *FileCommentsTool {
	return &FileCommentsTool{files: ds, grants: ds, comments: filecomments.New(ds, files), logger: logger, now: time.Now}
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
	// Comments are on the owner's account-wide files, outside any sandbox.
	if chat.IsSandboxed() {
		return t.refuse(sandboxedNote("File comments"))
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
	if t.office != nil && t.office.FormatOf(att) != nil {
		// A comment in an Office file is part of the file: changing one writes a new version of
		// it, which takes the same grant as changing its text.
		if args.Action != "list" && (t.grants == nil || !hasEditGrant(ctx, t.grants, t.logger, chat, att)) {
			return t.refuse("changing the comments of a Word, PowerPoint or Excel file rewrites the file, which you may do only after the user mentions you in a comment on it from this chat; you can still list them")
		}
		res, err = t.runOffice(ctx, chat, att, args)
		if msg, ok := refusalOf(err); ok {
			return t.refuse(msg)
		}
		if err != nil {
			return "", err
		}
		res.File = ref
		return marshalToolResult(res, fileCommentsToolName)
	}
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
		return "comments can be read and written here only on images and on UTF-8 text and code files of at most 4 MB", true
	case errors.Is(err, filecomments.ErrQuoteNotFound):
		return err.Error() + "; copy the quote exactly from the file (read it with agent_file_read)", true
	case errors.Is(err, filecomments.ErrQuoteAmbiguous):
		return err.Error() + "; quote more of the text, or give occurrence", true
	case errors.Is(err, officecomments.ErrNoThread):
		return "no thread with that id on this file; list its threads first", true
	case errors.Is(err, officecomments.ErrQuoteNotFound):
		return err.Error() + "; copy the quote exactly from the file", true
	case errors.Is(err, officecomments.ErrQuoteAmbiguous):
		return err.Error() + "; quote more of the text, or give occurrence", true
	case errors.Is(err, officecomments.ErrBadAnchor):
		return err.Error() + "; check the quote, slide, sheet or cell (Word needs a quote; Excel a cell without a thread)", true
	case errors.Is(err, officecomments.ErrOldComment):
		return err.Error() + "; start a new thread instead", true
	case errors.Is(err, officecomments.ErrTooLarge):
		return "the file is too large to read its comments here", true
	case errors.Is(err, officecomments.ErrNotOffice):
		return "the file could not be read as an Office file", true
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

// runOffice handles a call on a Word, PowerPoint or Excel file, whose comments are in the file.
func (t *FileCommentsTool) runOffice(ctx context.Context, chat *models.Chat, att *models.FileAttachment, args fileCommentsArgs) (fileCommentsResult, error) {
	if args.Action == "list" {
		threads, err := t.office.List(ctx, chat.UserID, att.ID)
		if err != nil {
			return fileCommentsResult{}, err
		}
		res := fileCommentsResult{Threads: make([]fileThreadView, 0, len(threads))}
		for _, th := range threads {
			res.Threads = append(res.Threads, officeThreadView(th))
		}
		return res, nil
	}
	text := strings.TrimSpace(args.Text)
	switch args.Action {
	case "reply", "add":
		if text == "" {
			return fileCommentsResult{}, fileRefusalf("text is required")
		}
		if len(text) > fileCommentMaxText {
			return fileCommentsResult{}, fileRefusalf("a comment can be at most %d characters", fileCommentMaxText)
		}
	case "resolve":
		if args.Resolved == nil {
			return fileCommentsResult{}, fileRefusalf("resolved is required: true to resolve, false to reopen")
		}
	default:
		return fileCommentsResult{}, fileRefusalf(`action must be "list", "reply", "add" or "resolve"`)
	}
	authorID, authorName := AgentCommentAuthor(chat)
	comment := officecomments.Comment{AuthorID: authorID, AuthorName: authorName, CreatedAt: t.now().UTC(), Text: text}
	chatID := chat.ID
	threadID, err := t.office.Change(ctx, chat.UserID, &chatID, att.ID, func(f officecomments.Format, data []byte) ([]byte, string, error) {
		switch args.Action {
		case "add":
			at := officecomments.Anchor{Quote: args.Quote, Occurrence: args.Occurrence, Slide: args.Slide, Sheet: args.Sheet, Cell: args.Cell}
			return f.Add(data, at, comment)
		case "reply":
			out, err := f.Reply(data, args.ThreadID, comment)
			return out, args.ThreadID, err
		default:
			out, err := f.Resolve(data, args.ThreadID, *args.Resolved)
			return out, args.ThreadID, err
		}
	})
	if err != nil {
		return fileCommentsResult{}, err
	}
	// Read back what was written, for the result and the reply's link.
	threads, err := t.office.List(ctx, chat.UserID, att.ID)
	if err != nil {
		return fileCommentsResult{}, err
	}
	for _, th := range threads {
		if th.ID != threadID {
			continue
		}
		view := officeThreadView(th)
		commentID := th.ID
		if args.Action == "reply" && len(th.Comments) > 0 {
			commentID = th.Comments[len(th.Comments)-1].ID
		}
		return fileCommentsResult{
			Thread:     &view,
			CommentRef: &modeltypes.CommentRef{FileID: att.ID, FileName: att.Name, ThreadID: th.ID, CommentID: commentID, Quote: filecomments.ShortQuote(th.On)},
		}, nil
	}
	return fileCommentsResult{}, fmt.Errorf("thread %s missing after write", threadID)
}

func officeThreadView(th officecomments.Thread) fileThreadView {
	v := fileThreadView{ID: th.ID, On: th.On, Where: th.Where, Resolved: th.Resolved}
	for _, c := range th.Comments {
		v.Comments = append(v.Comments, fileCommentView{ID: c.ID, Author: c.AuthorName, Text: c.Text})
	}
	return v
}
