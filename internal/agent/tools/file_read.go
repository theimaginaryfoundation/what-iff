package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/filechunker"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// read_file and grep_files give the agent exact, address-based access to uploaded text files:
// line ranges with a cursor, a structural outline, and host-side search. They complement
// find_context, which finds things by meaning; these find things by position and by exact text.
// Filtering happens here on the host, so the model pays for the matching lines, not the file.
const (
	ToolNameReadFile  = "read_file"
	ToolNameGrepFiles = "grep_files"

	readFileDefaultLines = 200
	readFileMaxLines     = 2000
	// fileToolMaxChars bounds one result's text so a single call cannot dominate the context window.
	fileToolMaxChars = 24000
	// fileToolMaxLineChars truncates pathological single lines (minified JSON, base64 blobs).
	fileToolMaxLineChars = 2000
	// fileToolMaxBytes is the largest file these tools will load; uploads are capped at 30MB.
	fileToolMaxBytes = 32 << 20

	outlineMaxEntries      = 200
	outlineFallbackEntries = 20

	grepDefaultMatches = 50
	grepMaxMatches     = 200
	grepDefaultContext = 1
	grepMaxContext     = 5
	grepMaxFiles       = 25
	grepMaxPatternLen  = 500
)

// ReadFileToolSpec reads an uploaded text file by line range, or returns its outline.
var ReadFileToolSpec = FunctionToolSpec{
	Name: ToolNameReadFile,
	Description: "Read the exact text of an uploaded text file, by file ID or exact file name, as numbered lines. " +
		"Use it for code, logs, transcripts, CSV, JSON and long documents when you need exact wording or one section. " +
		"Results are paged: pass next_cursor back as cursor to continue. " +
		"On a large file, call it with outline=true first to see its structure (Markdown headings, top-level JSON or YAML keys, CSV columns) with line numbers, then read only the range you need. " +
		"To find where something is mentioned, use grep_files. For questions about meaning across many files, use find_context. PDFs and images are not supported.",
	Properties: map[string]interface{}{
		"file": map[string]interface{}{
			"type":        "string",
			"description": "The file's ID, or its exact file name.",
		},
		"start_line": map[string]interface{}{
			"type":        "integer",
			"description": "Optional: first line to return (1-based). Defaults to 1.",
		},
		"end_line": map[string]interface{}{
			"type":        "integer",
			"description": fmt.Sprintf("Optional: last line to return (inclusive). Defaults to %d lines from start_line; at most %d lines per call.", readFileDefaultLines, readFileMaxLines),
		},
		"cursor": map[string]interface{}{
			"type":        "string",
			"description": "Optional: next_cursor from a previous read_file result, to continue where it stopped.",
		},
		"outline": map[string]interface{}{
			"type":        "boolean",
			"description": "Optional: return the file's structure with line numbers instead of its text.",
		},
	},
	Required: []string{"file"},
}

// GrepFilesToolSpec searches uploaded text files for a phrase or pattern.
var GrepFilesToolSpec = FunctionToolSpec{
	Name: ToolNameGrepFiles,
	Description: "Search uploaded text files for an exact phrase or a regular expression, and get the matching lines with line numbers and surrounding context. " +
		"Searches this conversation's files and the active personality's documents unless you name specific files. " +
		"Use it to locate something, then read around it with read_file. " +
		"Matching is literal and case-insensitive by default; set regex=true for RE2 regular expressions.",
	Properties: map[string]interface{}{
		"pattern": map[string]interface{}{
			"type":        "string",
			"description": "The phrase to find, or a regular expression when regex is true.",
		},
		"files": map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": fmt.Sprintf("Optional: file IDs or exact file names to search (at most %d). Defaults to this conversation's files and the personality's documents.", grepMaxFiles),
		},
		"regex": map[string]interface{}{
			"type":        "boolean",
			"description": "Optional: treat pattern as an RE2 regular expression.",
		},
		"case_sensitive": map[string]interface{}{
			"type":        "boolean",
			"description": "Optional: match case exactly. Defaults to false.",
		},
		"context_lines": map[string]interface{}{
			"type":        "integer",
			"description": fmt.Sprintf("Optional: lines of context before and after each match (0-%d). Defaults to %d.", grepMaxContext, grepDefaultContext),
		},
		"max_matches": map[string]interface{}{
			"type":        "integer",
			"description": fmt.Sprintf("Optional: maximum matches to return (1-%d). Defaults to %d.", grepMaxMatches, grepDefaultMatches),
		},
	},
	Required: []string{"pattern"},
}

// fileReadStore is the datastore surface the file tools need. Every lookup is owner-scoped.
type fileReadStore interface {
	GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error)
	ListFileAttachments(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error)
	ListFileAttachmentsInChatScope(ctx context.Context, userID, chatID uuid.UUID, personalityID *uuid.UUID, limit int) ([]*models.FileAttachment, error)
}

// FileReadTool implements read_file and grep_files.
type FileReadTool struct {
	store  fileReadStore
	logger *zap.Logger
	cache  *fileTextCache
	// loadText returns a file's text. It defaults to the object store via
	// storage.ResolveAttachmentTextContent; tests swap it out.
	loadText func(ctx context.Context, userID uuid.UUID, fa *models.FileAttachment) (string, bool)
}

// NewFileReadTool constructs the file tools. fileStore may be nil, in which case every read
// reports that the file's text is unavailable.
func NewFileReadTool(store fileReadStore, fileStore storage.FileStore, logger *zap.Logger) *FileReadTool {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &FileReadTool{
		store:  store,
		logger: logger,
		cache:  newFileTextCache(fileTextCacheMaxBytes),
		loadText: func(ctx context.Context, userID uuid.UUID, fa *models.FileAttachment) (string, bool) {
			return storage.ResolveAttachmentTextContent(ctx, logger, fileStore, userID, fa)
		},
	}
}

type fileRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

func toFileRef(fa *models.FileAttachment) *fileRef {
	return &fileRef{ID: fa.ID.String(), Name: fa.Name, Type: fa.FileType}
}

// --- read_file -----------------------------------------------------------------

type readFileArgs struct {
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Cursor    string `json:"cursor"`
	Outline   bool   `json:"outline"`
}

type outlineEntry struct {
	Line  int    `json:"line"`
	Level int    `json:"level,omitempty"`
	Text  string `json:"text"`
}

type readFileResult struct {
	Success    bool           `json:"success"`
	Error      string         `json:"error,omitempty"`
	File       *fileRef       `json:"file,omitempty"`
	TotalLines int            `json:"total_lines,omitempty"`
	StartLine  int            `json:"start_line,omitempty"`
	EndLine    int            `json:"end_line,omitempty"`
	Content    string         `json:"content,omitempty"`
	NextCursor string         `json:"next_cursor,omitempty"`
	Outline    []outlineEntry `json:"outline,omitempty"`
	Note       string         `json:"note,omitempty"`
}

// ReadFile handles a read_file call.
func (t *FileReadTool) ReadFile(ctx context.Context, chat *models.Chat, input []byte) (string, error) {
	var a readFileArgs
	if err := json.Unmarshal(input, &a); err != nil {
		return t.readFail(fmt.Sprintf("invalid arguments: %v", err))
	}
	if chat == nil {
		return t.readFail("read_file needs an active conversation")
	}
	ref, ok := validateNonEmptyString(a.File)
	if !ok {
		return t.readFail("file cannot be empty: pass a file ID or exact file name")
	}

	fa, note, err := t.resolveFile(ctx, chat, ref)
	if err != nil {
		return t.readFail(err.Error())
	}
	if fa == nil {
		return t.readFail(note)
	}

	lines, err := t.fileLines(ctx, chat.UserID, fa)
	if err != nil {
		return marshalToolResult(readFileResult{Success: false, Error: err.Error(), File: toFileRef(fa)}, ToolNameReadFile)
	}

	if a.Outline {
		entries, outlineNote := buildOutline(fa, lines)
		return marshalToolResult(readFileResult{
			Success:    true,
			File:       toFileRef(fa),
			TotalLines: len(lines),
			Outline:    entries,
			Note:       outlineNote,
		}, ToolNameReadFile)
	}

	start := a.StartLine
	if c := strings.TrimSpace(a.Cursor); c != "" {
		n, err := strconv.Atoi(c)
		if err != nil || n < 1 {
			return t.readFail(fmt.Sprintf("cursor %q is not a next_cursor value from read_file", a.Cursor))
		}
		start = n
	}
	if start < 1 {
		start = 1
	}
	if start > len(lines) {
		return marshalToolResult(readFileResult{
			Success:    true,
			File:       toFileRef(fa),
			TotalLines: len(lines),
			Note:       fmt.Sprintf("start_line %d is past the end of the file (%d lines).", start, len(lines)),
		}, ToolNameReadFile)
	}

	end := a.EndLine
	if end < start {
		end = start + readFileDefaultLines - 1
	}
	if end > start+readFileMaxLines-1 {
		end = start + readFileMaxLines - 1
	}
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	last := start - 1
	for n := start; n <= end; n++ {
		line := formatNumberedLine(n, lines[n-1])
		if b.Len()+len(line) > fileToolMaxChars && n > start {
			break
		}
		b.WriteString(line)
		last = n
	}

	res := readFileResult{
		Success:    true,
		File:       toFileRef(fa),
		TotalLines: len(lines),
		StartLine:  start,
		EndLine:    last,
		Content:    b.String(),
	}
	if last < len(lines) {
		res.NextCursor = strconv.Itoa(last + 1)
		if last < end {
			res.Note = fmt.Sprintf("Stopped at line %d to stay within the size limit; continue with cursor %q.", last, res.NextCursor)
		}
	}
	return marshalToolResult(res, ToolNameReadFile)
}

func (t *FileReadTool) readFail(msg string) (string, error) {
	return marshalToolResult(readFileResult{Success: false, Error: msg}, ToolNameReadFile)
}

func formatNumberedLine(n int, line string) string {
	if len(line) > fileToolMaxLineChars {
		cut := fileToolMaxLineChars
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		line = line[:cut] + fmt.Sprintf(" … [line truncated, %d chars]", len(line))
	}
	return strconv.Itoa(n) + "\t" + line + "\n"
}

// --- grep_files ----------------------------------------------------------------

type grepFilesArgs struct {
	Pattern       string   `json:"pattern"`
	Files         []string `json:"files"`
	Regex         bool     `json:"regex"`
	CaseSensitive bool     `json:"case_sensitive"`
	ContextLines  *int     `json:"context_lines"`
	MaxMatches    int      `json:"max_matches"`
}

type grepMatch struct {
	FileID string   `json:"file_id"`
	File   string   `json:"file"`
	Line   int      `json:"line"`
	Text   string   `json:"text"`
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

type grepFileSummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Matches int    `json:"matches"`
}

type grepSkipped struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

type grepFilesResult struct {
	Success   bool              `json:"success"`
	Error     string            `json:"error,omitempty"`
	Matches   []grepMatch       `json:"matches,omitempty"`
	Searched  []grepFileSummary `json:"searched,omitempty"`
	Skipped   []grepSkipped     `json:"skipped,omitempty"`
	Truncated bool              `json:"truncated,omitempty"`
	Note      string            `json:"note,omitempty"`
}

// GrepFiles handles a grep_files call.
func (t *FileReadTool) GrepFiles(ctx context.Context, chat *models.Chat, input []byte) (string, error) {
	var a grepFilesArgs
	if err := json.Unmarshal(input, &a); err != nil {
		return t.grepFail(fmt.Sprintf("invalid arguments: %v", err))
	}
	if chat == nil {
		return t.grepFail("grep_files needs an active conversation")
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return t.grepFail("pattern cannot be empty")
	}
	if len(a.Pattern) > grepMaxPatternLen {
		return t.grepFail(fmt.Sprintf("pattern is too long (max %d characters)", grepMaxPatternLen))
	}

	match, err := compileGrepMatcher(a.Pattern, a.Regex, a.CaseSensitive)
	if err != nil {
		return t.grepFail(err.Error())
	}

	contextLines := grepDefaultContext
	if a.ContextLines != nil {
		contextLines = *a.ContextLines
	}
	contextLines = clampInt(contextLines, 0, grepMaxContext)
	maxMatches := a.MaxMatches
	if maxMatches <= 0 {
		maxMatches = grepDefaultMatches
	}
	maxMatches = clampInt(maxMatches, 1, grepMaxMatches)

	res := grepFilesResult{Success: true}
	files, err := t.grepTargets(ctx, chat, a.Files, &res)
	if err != nil {
		return t.grepFail(err.Error())
	}
	if len(files) == 0 && len(res.Skipped) == 0 {
		res.Note = "No files to search: this conversation has no uploaded files and the personality has no documents. Name files explicitly to search others."
		return marshalToolResult(res, ToolNameGrepFiles)
	}

	budget := fileToolMaxChars
	for _, fa := range files {
		if res.Truncated {
			break
		}
		lines, err := t.fileLines(ctx, chat.UserID, fa)
		if err != nil {
			res.Skipped = append(res.Skipped, grepSkipped{File: fa.Name, Reason: err.Error()})
			continue
		}
		summary := grepFileSummary{ID: fa.ID.String(), Name: fa.Name}
		for i, line := range lines {
			if !match(line) {
				continue
			}
			summary.Matches++
			if len(res.Matches) >= maxMatches {
				res.Truncated = true
				continue // keep counting this file's matches for the summary
			}
			m := grepMatch{
				FileID: fa.ID.String(),
				File:   fa.Name,
				Line:   i + 1,
				Text:   clipLine(line),
				Before: clipLines(lines[max(0, i-contextLines):i]),
				After:  clipLines(lines[i+1 : min(len(lines), i+1+contextLines)]),
			}
			cost := matchCost(m)
			if cost > budget {
				res.Truncated = true
				continue
			}
			budget -= cost
			res.Matches = append(res.Matches, m)
		}
		res.Searched = append(res.Searched, summary)
	}

	if res.Truncated {
		res.Note = "More matches exist than were returned. Narrow the pattern, lower context_lines, or search fewer files; per-file totals are in searched."
	} else if len(res.Matches) == 0 {
		res.Note = "No matches."
	}
	return marshalToolResult(res, ToolNameGrepFiles)
}

func (t *FileReadTool) grepFail(msg string) (string, error) {
	return marshalToolResult(grepFilesResult{Success: false, Error: msg}, ToolNameGrepFiles)
}

// grepTargets resolves the files to search: the named ones, or the conversation's default scope.
// Files that cannot be resolved or are not text are reported in res.Skipped rather than failing
// the whole call.
func (t *FileReadTool) grepTargets(ctx context.Context, chat *models.Chat, refs []string, res *grepFilesResult) ([]*models.FileAttachment, error) {
	if len(refs) > 0 {
		if len(refs) > grepMaxFiles {
			return nil, fmt.Errorf("too many files (max %d per call)", grepMaxFiles)
		}
		out := make([]*models.FileAttachment, 0, len(refs))
		seen := make(map[uuid.UUID]struct{}, len(refs))
		for _, ref := range refs {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			fa, note, err := t.resolveFile(ctx, chat, ref)
			if err != nil {
				return nil, err
			}
			if fa == nil {
				res.Skipped = append(res.Skipped, grepSkipped{File: ref, Reason: note})
				continue
			}
			if _, dup := seen[fa.ID]; dup {
				continue
			}
			seen[fa.ID] = struct{}{}
			out = append(out, fa)
		}
		return out, nil
	}

	var personalityID *uuid.UUID
	if chat.PersonalityID != uuid.Nil {
		pid := chat.PersonalityID
		personalityID = &pid
	}
	scoped, err := t.store.ListFileAttachmentsInChatScope(ctx, chat.UserID, chat.ID, personalityID, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list this conversation's files: %v", err)
	}
	out := make([]*models.FileAttachment, 0, len(scoped))
	for _, fa := range scoped {
		if fa == nil || !isTextAttachment(fa) {
			continue // images and unsupported types are simply not part of the default scope
		}
		if len(out) == grepMaxFiles {
			res.Note = fmt.Sprintf("Searched the %d most recent text files; name files explicitly to search others.", grepMaxFiles)
			break
		}
		out = append(out, fa)
	}
	return out, nil
}

func compileGrepMatcher(pattern string, isRegex, caseSensitive bool) (func(string) bool, error) {
	if isRegex {
		expr := pattern
		if !caseSensitive {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid regular expression: %v", err)
		}
		return re.MatchString, nil
	}
	if caseSensitive {
		return func(line string) bool { return strings.Contains(line, pattern) }, nil
	}
	needle := strings.ToLower(pattern)
	return func(line string) bool { return strings.Contains(strings.ToLower(line), needle) }, nil
}

func clipLine(line string) string {
	if len(line) <= fileToolMaxLineChars {
		return line
	}
	cut := fileToolMaxLineChars
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return line[:cut] + " …"
}

func clipLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = clipLine(l)
	}
	return out
}

func matchCost(m grepMatch) int {
	n := len(m.Text) + len(m.File) + 64
	for _, l := range m.Before {
		n += len(l) + 4
	}
	for _, l := range m.After {
		n += len(l) + 4
	}
	return n
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// --- shared: resolution and loading ----------------------------------------------

// resolveFile turns a file ID or exact name into an attachment the user owns. Names are matched
// exactly (case-insensitive), preferring this conversation's files, so a common name like
// "notes.md" resolves to the copy in front of the user. A nil attachment with a note means "not
// found" (the note says why); an error means the lookup itself failed.
func (t *FileReadTool) resolveFile(ctx context.Context, chat *models.Chat, ref string) (*models.FileAttachment, string, error) {
	if id, err := uuid.Parse(ref); err == nil {
		fa, err := t.store.GetFileAttachment(ctx, chat.UserID, id)
		if err != nil || fa == nil {
			return nil, fmt.Sprintf("no file found with ID %q", ref), nil
		}
		return fa, "", nil
	}

	var personalityID *uuid.UUID
	if chat.PersonalityID != uuid.Nil {
		pid := chat.PersonalityID
		personalityID = &pid
	}
	if scoped, err := t.store.ListFileAttachmentsInChatScope(ctx, chat.UserID, chat.ID, personalityID, 0); err == nil {
		for _, fa := range scoped {
			if fa != nil && strings.EqualFold(fa.Name, ref) {
				return fa, "", nil
			}
		}
	}

	page, err := t.store.ListFileAttachments(ctx, chat.UserID, 1, 10, models.FileAttachmentFilters{Name: strPtr(ref)})
	if err != nil {
		return nil, "", fmt.Errorf("failed to look up file %q: %v", ref, err)
	}
	var suggestions []string
	seen := make(map[string]struct{})
	if page != nil {
		for _, item := range page.Results {
			fa, ok := item.(*models.FileAttachment)
			if !ok || fa == nil {
				continue
			}
			if strings.EqualFold(fa.Name, ref) {
				return fa, "", nil
			}
			if _, dup := seen[fa.Name]; !dup && fa.Name != "" {
				seen[fa.Name] = struct{}{}
				suggestions = append(suggestions, fa.Name)
			}
		}
	}
	note := fmt.Sprintf("no file exactly named %q", ref)
	if len(suggestions) > 0 {
		note += "; nearby files: " + strings.Join(suggestions, ", ") + ". Retry with an exact name or the file's ID"
	}
	return nil, note, nil
}

// extraTextExtensions are plain-text formats the file tools read even though the chunker does not
// embed them (CSV and XML are uploadable today; the rest arrive from tools and imports).
var extraTextExtensions = map[string]struct{}{
	".csv": {}, ".tsv": {}, ".xml": {}, ".yaml": {}, ".yml": {}, ".toml": {}, ".ini": {},
	".log": {}, ".jsonl": {}, ".ndjson": {}, ".srt": {}, ".vtt": {}, ".sql": {},
}

// isTextAttachment decides whether the file tools will try to read a file as text. It is broader
// than the chunker's list on purpose: anything text/*, the chunker's types, and common data
// formats. fileLines still rejects content that is not valid UTF-8.
func isTextAttachment(fa *models.FileAttachment) bool {
	ct := strings.ToLower(strings.TrimSpace(fa.FileType))
	if strings.HasPrefix(ct, models.ImageMIMEPrefix) {
		return false
	}
	if strings.HasPrefix(ct, "text/") || filechunker.IsTextType(ct) || filechunker.IsTextFileByExtension(fa.Name) {
		return true
	}
	if _, ok := extraTextExtensions[strings.ToLower(filepath.Ext(fa.Name))]; ok {
		return true
	}
	return strings.HasSuffix(ct, "+json") || strings.HasSuffix(ct, "+xml") || ct == "application/xml" || ct == "application/x-ndjson"
}

// fileLines loads a text attachment and splits it into lines (CRLF-tolerant). Results are cached
// by attachment ID and storage key, which are immutable for a given upload.
func (t *FileReadTool) fileLines(ctx context.Context, userID uuid.UUID, fa *models.FileAttachment) ([]string, error) {
	if !isTextAttachment(fa) {
		return nil, fmt.Errorf("%s is not a text file (type %s); use find_context to read it", fa.Name, fa.FileType)
	}
	key := fa.ID.String() + "|" + fa.S3Key
	if lines, ok := t.cache.get(key); ok {
		return lines, nil
	}
	text, ok := t.loadText(ctx, userID, fa)
	if !ok {
		return nil, fmt.Errorf("the text of %s is not available", fa.Name)
	}
	if len(text) > fileToolMaxBytes {
		return nil, fmt.Errorf("%s is too large to read here (%d MB); use find_context to search it", fa.Name, len(text)>>20)
	}
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("%s does not contain valid UTF-8 text", fa.Name)
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	t.cache.put(key, lines, len(text))
	return lines, nil
}
