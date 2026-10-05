package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type fakeFileReadStore struct {
	files     map[uuid.UUID]*models.FileAttachment
	chatScope []*models.FileAttachment
	listCalls int
}

func (s *fakeFileReadStore) GetFileAttachment(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.FileAttachment, error) {
	if fa, ok := s.files[id]; ok {
		return fa, nil
	}
	return nil, fmt.Errorf("not found")
}

func (s *fakeFileReadStore) ListFileAttachments(_ context.Context, _ uuid.UUID, _, _ int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
	s.listCalls++
	var results []interface{}
	for _, fa := range s.files {
		if filters.Name != nil && strings.Contains(strings.ToLower(fa.Name), strings.ToLower(*filters.Name)) {
			results = append(results, fa)
		}
	}
	return &models.PaginatedResponse{Results: results, TotalCount: len(results)}, nil
}

func (s *fakeFileReadStore) ListFileAttachmentsInChatScope(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID, _ int) ([]*models.FileAttachment, error) {
	return s.chatScope, nil
}

type fileFixture struct {
	tool  *FileReadTool
	store *fakeFileReadStore
	text  map[uuid.UUID]string
	loads map[uuid.UUID]int
	chat  *models.Chat
}

func newFileFixture() *fileFixture {
	f := &fileFixture{
		store: &fakeFileReadStore{files: map[uuid.UUID]*models.FileAttachment{}},
		text:  map[uuid.UUID]string{},
		loads: map[uuid.UUID]int{},
		chat:  &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()},
	}
	f.tool = &FileReadTool{
		store:  f.store,
		logger: zap.NewNop(),
		cache:  newFileTextCache(fileTextCacheMaxBytes),
		loadText: func(_ context.Context, _ uuid.UUID, fa *models.FileAttachment) (string, bool) {
			f.loads[fa.ID]++
			text, ok := f.text[fa.ID]
			return text, ok && text != ""
		},
	}
	return f
}

// addFile registers a file; inScope also puts it in the conversation's default scope.
func (f *fileFixture) addFile(name, fileType, text string, inScope bool) *models.FileAttachment {
	fa := &models.FileAttachment{ID: uuid.New(), Name: name, FileType: fileType, S3Key: "k/" + name}
	f.store.files[fa.ID] = fa
	f.text[fa.ID] = text
	if inScope {
		f.store.chatScope = append(f.store.chatScope, fa)
	}
	return fa
}

func numberedText(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	return strings.Join(lines, "\n")
}

func callRead(t *testing.T, f *fileFixture, args map[string]interface{}) readFileResult {
	t.Helper()
	in, err := json.Marshal(args)
	require.NoError(t, err)
	out, err := f.tool.ReadFile(context.Background(), f.chat, in)
	require.NoError(t, err)
	var res readFileResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res
}

func callGrep(t *testing.T, f *fileFixture, args map[string]interface{}) grepFilesResult {
	t.Helper()
	in, err := json.Marshal(args)
	require.NoError(t, err)
	out, err := f.tool.GrepFiles(context.Background(), f.chat, in)
	require.NoError(t, err)
	var res grepFilesResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res
}

func TestReadFile_DefaultWindowAndCursor(t *testing.T) {
	f := newFileFixture()
	fa := f.addFile("log.txt", "text/plain", numberedText(450), true)

	first := callRead(t, f, map[string]interface{}{"file": fa.ID.String()})
	require.True(t, first.Success, first.Error)
	assert.Equal(t, 450, first.TotalLines)
	assert.Equal(t, 1, first.StartLine)
	assert.Equal(t, readFileDefaultLines, first.EndLine)
	assert.True(t, strings.HasPrefix(first.Content, "1\tline 1\n"))
	assert.Equal(t, "201", first.NextCursor)

	second := callRead(t, f, map[string]interface{}{"file": fa.ID.String(), "cursor": first.NextCursor})
	assert.Equal(t, 201, second.StartLine)
	assert.True(t, strings.HasPrefix(second.Content, "201\tline 201\n"))

	last := callRead(t, f, map[string]interface{}{"file": fa.ID.String(), "start_line": 401})
	assert.Equal(t, 450, last.EndLine)
	assert.Empty(t, last.NextCursor, "no cursor once the end is reached")

	assert.Equal(t, 1, f.loads[fa.ID], "paging reuses the cached text instead of downloading again")
}

func TestReadFile_ExplicitRangeAndLimits(t *testing.T) {
	f := newFileFixture()
	fa := f.addFile("notes.md", "text/markdown", numberedText(3000), true)

	res := callRead(t, f, map[string]interface{}{"file": "notes.md", "start_line": 10, "end_line": 12})
	require.True(t, res.Success, res.Error)
	assert.Equal(t, "10\tline 10\n11\tline 11\n12\tline 12\n", res.Content)
	assert.Equal(t, "13", res.NextCursor)

	huge := callRead(t, f, map[string]interface{}{"file": fa.ID.String(), "start_line": 1, "end_line": 3000})
	assert.LessOrEqual(t, huge.EndLine, readFileMaxLines)
	assert.LessOrEqual(t, len(huge.Content), fileToolMaxChars)

	past := callRead(t, f, map[string]interface{}{"file": fa.ID.String(), "start_line": 5000})
	assert.True(t, past.Success)
	assert.Contains(t, past.Note, "past the end")
}

func TestReadFile_CharBudgetStopsEarlyWithCursor(t *testing.T) {
	f := newFileFixture()
	long := strings.Repeat("x", 1500)
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = long
	}
	fa := f.addFile("wide.txt", "text/plain", strings.Join(lines, "\n"), true)

	res := callRead(t, f, map[string]interface{}{"file": fa.ID.String(), "end_line": 100})
	require.True(t, res.Success)
	assert.Less(t, res.EndLine, 100)
	assert.LessOrEqual(t, len(res.Content), fileToolMaxChars)
	assert.Equal(t, fmt.Sprint(res.EndLine+1), res.NextCursor)
	assert.Contains(t, res.Note, "size limit")
}

func TestReadFile_TruncatesPathologicalLines(t *testing.T) {
	f := newFileFixture()
	fa := f.addFile("min.json", "application/json", strings.Repeat("é", fileToolMaxLineChars), true)
	res := callRead(t, f, map[string]interface{}{"file": fa.ID.String()})
	require.True(t, res.Success)
	assert.Contains(t, res.Content, "line truncated")
	assert.True(t, strings.ToValidUTF8(res.Content, "?") == res.Content, "truncation must not split a rune")
}

func TestReadFile_ResolutionAndErrors(t *testing.T) {
	f := newFileFixture()
	f.addFile("Report.txt", "text/plain", "a\nb", true)
	f.addFile("diagram.png", "image/png", "", true)
	f.addFile("scan.pdf", "application/pdf", "", true)
	f.addFile("data.xml", "application/xml", "<a/>", true)

	byName := callRead(t, f, map[string]interface{}{"file": "report.TXT"})
	assert.True(t, byName.Success, "names match case-insensitively")

	missing := callRead(t, f, map[string]interface{}{"file": "Report"})
	assert.False(t, missing.Success)
	assert.Contains(t, missing.Error, "Report.txt", "suggests the nearby exact name")

	img := callRead(t, f, map[string]interface{}{"file": "diagram.png"})
	assert.False(t, img.Success)
	assert.Contains(t, img.Error, "not a text file")

	pdf := callRead(t, f, map[string]interface{}{"file": "scan.pdf"})
	assert.False(t, pdf.Success)

	xml := callRead(t, f, map[string]interface{}{"file": "data.xml"})
	assert.True(t, xml.Success, "XML is uploadable and readable even though it is not embedded")

	unknownID := callRead(t, f, map[string]interface{}{"file": uuid.New().String()})
	assert.False(t, unknownID.Success)

	empty := callRead(t, f, map[string]interface{}{"file": "  "})
	assert.False(t, empty.Success)

	badCursor := callRead(t, f, map[string]interface{}{"file": "Report.txt", "cursor": "next"})
	assert.False(t, badCursor.Success)
}

func TestReadFile_PrefersConversationCopyOfName(t *testing.T) {
	f := newFileFixture()
	other := f.addFile("notes.md", "text/markdown", "from another chat", false)
	here := f.addFile("notes.md", "text/markdown", "from this chat", true)
	_ = other

	res := callRead(t, f, map[string]interface{}{"file": "notes.md"})
	require.True(t, res.Success)
	assert.Equal(t, here.ID.String(), res.File.ID)
}

func TestReadFile_CRLFAndInvalidUTF8(t *testing.T) {
	f := newFileFixture()
	crlf := f.addFile("win.txt", "text/plain", "one\r\ntwo\r\nthree", true)
	res := callRead(t, f, map[string]interface{}{"file": crlf.ID.String()})
	assert.Equal(t, "1\tone\n2\ttwo\n3\tthree\n", res.Content)

	bin := f.addFile("bin.txt", "text/plain", "ok\xff\xfe", true)
	bad := callRead(t, f, map[string]interface{}{"file": bin.ID.String()})
	assert.False(t, bad.Success)
	assert.Contains(t, bad.Error, "UTF-8")
}

func TestReadFile_Outlines(t *testing.T) {
	f := newFileFixture()

	md := f.addFile("guide.md", "text/markdown", "# Title\nintro\n```\n# not a heading\n```\n## Setup\n### Deep ###\n#hashtag\n", true)
	res := callRead(t, f, map[string]interface{}{"file": md.ID.String(), "outline": true})
	require.True(t, res.Success)
	require.Len(t, res.Outline, 3)
	assert.Equal(t, outlineEntry{Line: 1, Level: 1, Text: "Title"}, res.Outline[0])
	assert.Equal(t, outlineEntry{Line: 6, Level: 2, Text: "Setup"}, res.Outline[1])
	assert.Equal(t, outlineEntry{Line: 7, Level: 3, Text: "Deep"}, res.Outline[2])

	js := f.addFile("state.json", "application/json", "{\n  \"name\": \"x\",\n  \"party\": [1, 2, 3],\n  \"meta\": {\n    \"a\": 1\n  }\n}", true)
	res = callRead(t, f, map[string]interface{}{"file": js.ID.String(), "outline": true})
	require.Len(t, res.Outline, 3)
	assert.Equal(t, outlineEntry{Line: 2, Level: 1, Text: "name: string"}, res.Outline[0])
	assert.Equal(t, outlineEntry{Line: 3, Level: 1, Text: "party: array (3 items)"}, res.Outline[1])
	assert.Equal(t, outlineEntry{Line: 4, Level: 1, Text: "meta: object (1 keys)"}, res.Outline[2])

	arr := f.addFile("list.json", "application/json", "[{\"a\":1},{\"a\":2}]", true)
	res = callRead(t, f, map[string]interface{}{"file": arr.ID.String(), "outline": true})
	require.Len(t, res.Outline, 1)
	assert.Equal(t, "array of 2 items, first item is object (1 keys)", res.Outline[0].Text)

	csvFile := f.addFile("hp.csv", "text/csv", "name,hp,ac\nGrog,40,15\nVex,30,17\n", true)
	res = callRead(t, f, map[string]interface{}{"file": csvFile.ID.String(), "outline": true})
	require.Len(t, res.Outline, 1)
	assert.Equal(t, "header, 3 columns: name, hp, ac", res.Outline[0].Text)
	assert.Contains(t, res.Note, "2 data rows")

	yml := f.addFile("cfg.yaml", "text/plain", "# comment\nserver:\n  port: 80\nurl: http://x\nlist:\n  - a\n", true)
	res = callRead(t, f, map[string]interface{}{"file": yml.ID.String(), "outline": true})
	var keys []string
	for _, e := range res.Outline {
		keys = append(keys, e.Text)
	}
	assert.Equal(t, []string{"server", "url", "list"}, keys)

	plain := f.addFile("story.txt", "text/plain", "\n\nOnce upon a time\nthe end", true)
	res = callRead(t, f, map[string]interface{}{"file": plain.ID.String(), "outline": true})
	require.Len(t, res.Outline, 2)
	assert.Equal(t, 3, res.Outline[0].Line)
	assert.Contains(t, res.Note, "first non-empty lines")

	brokenJSON := f.addFile("broken.json", "application/json", "{\"a\": ", true)
	res = callRead(t, f, map[string]interface{}{"file": brokenJSON.ID.String(), "outline": true})
	assert.True(t, res.Success, "invalid JSON falls back to the generic outline")
}

func TestGrepFiles_LiteralCaseInsensitiveWithContext(t *testing.T) {
	f := newFileFixture()
	a := f.addFile("a.txt", "text/plain", "alpha\nThe Dragon sleeps\nomega", true)
	f.addFile("b.md", "text/markdown", "no match here", true)
	f.addFile("pic.png", "image/png", "", true)

	res := callGrep(t, f, map[string]interface{}{"pattern": "dragon"})
	require.True(t, res.Success, res.Error)
	require.Len(t, res.Matches, 1)
	m := res.Matches[0]
	assert.Equal(t, a.ID.String(), m.FileID)
	assert.Equal(t, 2, m.Line)
	assert.Equal(t, "The Dragon sleeps", m.Text)
	assert.Equal(t, []string{"alpha"}, m.Before)
	assert.Equal(t, []string{"omega"}, m.After)
	assert.Len(t, res.Searched, 2, "images are not part of the default scope")
	assert.Empty(t, res.Skipped)

	strict := callGrep(t, f, map[string]interface{}{"pattern": "dragon", "case_sensitive": true})
	assert.Empty(t, strict.Matches)
	assert.Equal(t, "No matches.", strict.Note)

	noCtx := callGrep(t, f, map[string]interface{}{"pattern": "dragon", "context_lines": 0})
	assert.Empty(t, noCtx.Matches[0].Before)
	assert.Empty(t, noCtx.Matches[0].After)
}

func TestGrepFiles_RegexAndErrors(t *testing.T) {
	f := newFileFixture()
	f.addFile("log.txt", "text/plain", "ERROR 500 at /a\nINFO ok\nerror 404 at /b", true)

	res := callGrep(t, f, map[string]interface{}{"pattern": `error \d{3}`, "regex": true})
	require.True(t, res.Success)
	assert.Len(t, res.Matches, 2)

	bad := callGrep(t, f, map[string]interface{}{"pattern": "(unclosed", "regex": true})
	assert.False(t, bad.Success)
	assert.Contains(t, bad.Error, "invalid regular expression")

	literalParen := callGrep(t, f, map[string]interface{}{"pattern": "(unclosed"})
	assert.True(t, literalParen.Success, "literal mode never compiles the pattern")

	empty := callGrep(t, f, map[string]interface{}{"pattern": " "})
	assert.False(t, empty.Success)

	long := callGrep(t, f, map[string]interface{}{"pattern": strings.Repeat("a", grepMaxPatternLen+1)})
	assert.False(t, long.Success)
}

func TestGrepFiles_MaxMatchesKeepsCounting(t *testing.T) {
	f := newFileFixture()
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "hit"
	}
	f.addFile("many.txt", "text/plain", strings.Join(lines, "\n"), true)

	res := callGrep(t, f, map[string]interface{}{"pattern": "hit", "max_matches": 5, "context_lines": 0})
	assert.Len(t, res.Matches, 5)
	assert.True(t, res.Truncated)
	require.Len(t, res.Searched, 1)
	assert.Equal(t, 30, res.Searched[0].Matches, "the per-file total counts every match")
}

func TestGrepFiles_ExplicitFilesAndSkips(t *testing.T) {
	f := newFileFixture()
	outside := f.addFile("elsewhere.txt", "text/plain", "needle", false)
	f.addFile("here.txt", "text/plain", "needle too", true)
	f.addFile("photo.png", "image/png", "", false)

	res := callGrep(t, f, map[string]interface{}{
		"pattern": "needle",
		"files":   []string{outside.ID.String(), outside.ID.String(), "photo.png", "nope.txt"},
	})
	require.True(t, res.Success, res.Error)
	require.Len(t, res.Matches, 1, "only the named file is searched, once")
	assert.Equal(t, outside.ID.String(), res.Matches[0].FileID)
	require.Len(t, res.Skipped, 2)
	reasons := res.Skipped[0].Reason + " | " + res.Skipped[1].Reason
	assert.Contains(t, reasons, "not a text file")
	assert.Contains(t, reasons, "no file exactly named")

	tooMany := make([]string, grepMaxFiles+1)
	for i := range tooMany {
		tooMany[i] = "x"
	}
	over := callGrep(t, f, map[string]interface{}{"pattern": "needle", "files": tooMany})
	assert.False(t, over.Success)
}

func TestGrepFiles_EmptyScopeExplains(t *testing.T) {
	f := newFileFixture()
	res := callGrep(t, f, map[string]interface{}{"pattern": "x"})
	assert.True(t, res.Success)
	assert.Contains(t, res.Note, "No files to search")
}

func TestGrepFiles_ResultStaysWithinBudget(t *testing.T) {
	f := newFileFixture()
	wide := strings.Repeat("needle ", 280) // ~2k chars per line
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = wide
	}
	f.addFile("wide.txt", "text/plain", strings.Join(lines, "\n"), true)

	res := callGrep(t, f, map[string]interface{}{"pattern": "needle", "max_matches": 200, "context_lines": 2})
	assert.True(t, res.Truncated)
	total := 0
	for _, m := range res.Matches {
		total += matchCost(m)
	}
	assert.LessOrEqual(t, total, fileToolMaxChars)
}

func TestFileTextCache_EvictsLeastRecentlyUsed(t *testing.T) {
	c := newFileTextCache(10)
	c.put("a", []string{"a"}, 4)
	c.put("b", []string{"b"}, 4)
	_, _ = c.get("a") // a is now most recent
	c.put("c", []string{"c"}, 4)

	_, okA := c.get("a")
	_, okB := c.get("b")
	_, okC := c.get("c")
	assert.True(t, okA)
	assert.False(t, okB, "least recently used entry is evicted")
	assert.True(t, okC)

	c.put("huge", []string{"h"}, 11)
	_, okHuge := c.get("huge")
	assert.False(t, okHuge, "entries larger than the cache are never stored")
}

func TestGrepFiles_ScopeNoteSurvivesNoMatches(t *testing.T) {
	f := newFileFixture()
	for i := 0; i < grepMaxFiles+3; i++ {
		f.addFile(fmt.Sprintf("f%02d.txt", i), "text/plain", "nothing here", true)
	}
	res := callGrep(t, f, map[string]interface{}{"pattern": "absent"})
	assert.Contains(t, res.Note, "Only the 25 most recent text files were searched")
	assert.Contains(t, res.Note, "No matches.", "a later note is appended, not substituted")
}

func TestGrepFiles_OverBudgetStopsAddingMatches(t *testing.T) {
	f := newFileFixture()
	// Each long line costs ~2k after clipping, so the budget runs out partway through; the short
	// match at the end would still fit, but adding it would leave a gap in the results.
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, strings.Repeat("needle ", 400))
	}
	lines = append(lines, "needle small")
	f.addFile("mixed.txt", "text/plain", strings.Join(lines, "\n"), true)

	res := callGrep(t, f, map[string]interface{}{"pattern": "needle", "context_lines": 0})
	require.True(t, res.Truncated)
	for i, m := range res.Matches {
		assert.Equal(t, i+1, m.Line, "returned matches are a contiguous prefix of all matches")
	}
	require.Len(t, res.Searched, 1)
	assert.Equal(t, 21, res.Searched[0].Matches, "the file's total still counts every match")
	assert.Less(t, len(res.Matches), 21)
}

func TestReadFile_RejectsAbsurdLineCounts(t *testing.T) {
	f := newFileFixture()
	fa := f.addFile("newlines.txt", "text/plain", "x"+strings.Repeat("\n", fileToolMaxLines), true)
	res := callRead(t, f, map[string]interface{}{"file": fa.ID.String()})
	assert.False(t, res.Success)
	assert.Contains(t, res.Error, "too many lines")
}
