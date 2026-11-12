// Package xlsx reads and writes the threaded comments of Excel workbooks (ADR 0x046), as the
// browser's spreadsheet editor does (web/app/src/app/features/office/xlsx/xlsx-threaded-comments.ts):
// a threadedComments part per sheet, the people in a persons part, and for each thread a legacy
// note with placeholder text and its VML shape, which Excel needs to open it.
package xlsx

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

// Format is the Excel comments format.
type Format struct{}

// New returns the Excel comments format.
func New() Format { return Format{} }

var _ officecomments.Format = Format{}

// List returns every thread on a cell, sheet by sheet in row then column order, as the panel shows
// them. On is "Sheet1!B3"; the panel itself shows "B3" on the open sheet.
func (Format) List(data []byte) ([]officecomments.Thread, error) {
	b, err := load(data)
	if err != nil {
		return nil, err
	}
	var out []officecomments.Thread
	for _, s := range b.sheets {
		threads := b.threaded(s).threads()
		sort.SliceStable(threads, func(i, j int) bool {
			a, c := threads[i], threads[j]
			return a.row < c.row || a.row == c.row && a.col < c.col
		})
		for _, th := range threads {
			t := officecomments.Thread{
				ID:       th.root.id,
				On:       sheetRef(s.name, th.row, th.col),
				Where:    fmt.Sprintf("sheet %s, cell %s", s.name, cellA1(th.row, th.col)),
				Resolved: th.root.done,
			}
			for _, c := range th.comments {
				id, name := b.author(c.personID)
				t.Comments = append(t.Comments, officecomments.Comment{ID: c.id, AuthorID: id, AuthorName: name, CreatedAt: parseDate(c.dT), Text: c.text})
			}
			out = append(out, t)
		}
	}
	if b.p.err != nil {
		return nil, b.p.err
	}
	return out, nil
}

// Add starts a thread on a cell. A cell has one thread at most; one with a thread or a note is
// ErrBadAnchor.
func (Format) Add(data []byte, at officecomments.Anchor, first officecomments.Comment) ([]byte, string, error) {
	b, err := load(data)
	if err != nil {
		return nil, "", err
	}
	if at.Cell == "" {
		return nil, "", fmt.Errorf("%w: an Excel comment needs a cell", officecomments.ErrBadAnchor)
	}
	row, col, ok := parseCell(at.Cell)
	if !ok {
		return nil, "", fmt.Errorf("%w: %q is not a cell", officecomments.ErrBadAnchor, at.Cell)
	}
	s, ok := b.sheetNamed(at.Sheet)
	if !ok {
		return nil, "", fmt.Errorf("%w: no sheet %q", officecomments.ErrBadAnchor, at.Sheet)
	}
	ref := cellA1(row, col)
	t := b.threaded(s)
	for _, th := range t.threads() {
		if th.row == row && th.col == col {
			return nil, "", fmt.Errorf("%w: cell %s already has thread %s; reply to it instead", officecomments.ErrBadAnchor, ref, th.root.id)
		}
	}
	if b.hasNote(s, ref) {
		return nil, "", fmt.Errorf("%w: cell %s has a note", officecomments.ErrBadAnchor, ref)
	}
	id := b.commentID(first.ID, t)
	text := cleanText(first.Text)
	el := commentXML(ref, first, b.personFor(first), id, "", true)
	// The legacy note first, so the sheet's relationships come in the browser's order.
	b.putLegacy(s, ref, id, legacyText([]string{text}))
	b.appendThreaded(s, t, el)
	out, err := b.p.save()
	return out, id, err
}

// Reply adds a comment at the end of a thread and brings its legacy note up to date.
func (Format) Reply(data []byte, threadID string, c officecomments.Comment) ([]byte, error) {
	b, err := load(data)
	if err != nil {
		return nil, err
	}
	s, t, th := b.find(threadID)
	if th == nil {
		return nil, officecomments.ErrNoThread
	}
	ref := cellA1(th.row, th.col)
	el := withPrefix(commentXML(ref, c, b.personFor(c), b.commentID(c.ID, t), th.root.id, false), prefix(t.data, t.root))
	last := th.comments[len(th.comments)-1].n
	t.data = apply(t.data, []edit{{last.end, last.end, el}})
	b.p.put(t.path, t.data)
	texts := make([]string, 0, len(th.comments)+1)
	for _, c := range th.comments {
		texts = append(texts, c.text)
	}
	texts = append(texts, cleanText(c.Text))
	b.putLegacy(s, ref, th.root.id, legacyText(texts))
	return b.p.save()
}

// Resolve marks a thread resolved (done="1") or open again (done="0").
func (Format) Resolve(data []byte, threadID string, resolved bool) ([]byte, error) {
	b, err := load(data)
	if err != nil {
		return nil, err
	}
	_, t, th := b.find(threadID)
	if th == nil {
		return nil, officecomments.ErrNoThread
	}
	done := "0"
	if resolved {
		done = "1"
	}
	n := th.root.n
	tag := setAttr(string(t.data[n.start:n.tagEnd]), "done", done)
	b.p.put(t.path, apply(t.data, []edit{{n.start, n.tagEnd, tag}}))
	return b.p.save()
}

// find is the thread with the id, on whichever sheet it is.
func (b *book) find(id string) (sheet, *threaded, *thread) {
	for _, s := range b.sheets {
		t := b.threaded(s)
		for _, th := range t.threads() {
			if th.root.id == id {
				return s, t, th
			}
		}
	}
	for _, s := range b.sheets {
		t := b.threaded(s)
		for _, th := range t.threads() {
			if strings.EqualFold(th.root.id, id) {
				return s, t, th
			}
		}
	}
	return sheet{}, nil, nil
}

// commentID is the caller's id when it is a GUID not in use, else a new one, as Excel needs.
func (b *book) commentID(given string, t *threaded) string {
	if guidRe.MatchString(given) && !t.used(given) {
		return given
	}
	return newGUID()
}

// commentXML is a threadedComment as the browser writes it: a root with done, a reply with parentId.
func commentXML(ref string, c officecomments.Comment, personID, id, parentID string, root bool) string {
	attrs := `ref="` + escapeXML(ref) + `" dT="` + excelDate(c.CreatedAt) + `" personId="` + personID + `" id="` + id + `"`
	if parentID != "" {
		attrs += ` parentId="` + escapeXML(parentID) + `"`
	}
	if root {
		attrs += ` done="0"`
	}
	return `<threadedComment ` + attrs + `><text>` + escapeXML(cleanText(c.Text)) + `</text></threadedComment>`
}

// appendThreaded adds a thread at the end of the sheet's threadedComments part, making the part
// when the sheet has none.
func (b *book) appendThreaded(s sheet, t *threaded, el string) {
	if t.root != nil {
		el = withPrefix(el, prefix(t.data, t.root))
		b.p.put(t.path, apply(t.data, []edit{appendChild(t.data, t.root, el)}))
		return
	}
	part := `<ThreadedComments xmlns="` + nsThreaded + `" xmlns:x="` + nsMain + `">` + el + `</ThreadedComments>`
	if t.path != "" && b.p.has(t.path) {
		// A part that is there but unreadable is replaced.
		b.p.put(t.path, []byte(xmlHead+part))
		return
	}
	t.path = b.p.unused(path.Join(path.Dir(b.wbPath), "threadedComments/threadedComment%d.xml"))
	b.p.put(t.path, []byte(xmlHead+part))
	b.p.addOverride(t.path, ctThreaded)
	b.p.addRel(s.path, relThreaded, t.path)
}

// withPrefix writes a threadedComment's elements with the part's prefix, when it has one.
func withPrefix(el, pfx string) string {
	if pfx == "" {
		return el
	}
	return strings.NewReplacer("<threadedComment ", "<"+pfx+"threadedComment ", "</threadedComment>", "</"+pfx+"threadedComment>", "<text>", "<"+pfx+"text>", "</text>", "</"+pfx+"text>").Replace(el)
}
