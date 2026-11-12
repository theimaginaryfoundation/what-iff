// Package docx reads and writes the comments of Word files as the browser's editor does
// (web/app/src/app/features/office/docx/docx-comments.ts), editing the parts' text in place so
// everything else in the file stays as it was.
package docx

import (
	"fmt"
	"sort"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

// Format is Word's officecomments.Format.
type Format struct{}

// New returns the Word format.
func New() Format { return Format{} }

var _ officecomments.Format = Format{}

func open(data []byte) (*comments, error) {
	p, err := openPkg(data)
	if err != nil {
		return nil, err
	}
	return readComments(p)
}

// List returns the file's threads in the order the panel shows them: by story, then by where
// they start. Threads with no place in the text come last.
func (Format) List(data []byte) ([]officecomments.Thread, error) {
	c, err := open(data)
	if err != nil {
		return nil, err
	}
	list, threadOf := c.threads()
	stories, err := c.stories()
	if err != nil {
		return nil, err
	}
	type placed struct {
		t            officecomments.Thread
		story, start int
	}
	var out []placed
	seen := map[*thread]bool{}
	for si, s := range stories {
		text, ranges := s.textAndRanges()
		for _, r := range ranges {
			t := threadOf[r.wordID]
			if t == nil || seen[t] {
				continue
			}
			seen[t] = true
			th := c.toThread(t)
			th.On = quoteOf(text, r.start, r.end)
			th.Where = fmt.Sprintf("paragraph %d", r.paragraph)
			if s.kind != "body" {
				th.Where = s.kind + ", " + th.Where
			}
			out = append(out, placed{th, si, r.start})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].story != out[j].story {
			return out[i].story < out[j].story
		}
		return out[i].start < out[j].start
	})
	threads := make([]officecomments.Thread, 0, len(list))
	for _, p := range out {
		threads = append(threads, p.t)
	}
	for _, t := range list {
		if !seen[t] {
			th := c.toThread(t)
			th.Where = "not marked in the text"
			threads = append(threads, th)
		}
	}
	return threads, nil
}

// Add marks the quoted text in the body with a new thread and returns the thread's id.
func (Format) Add(data []byte, at officecomments.Anchor, first officecomments.Comment) ([]byte, string, error) {
	if at.Quote == "" {
		return nil, "", officecomments.ErrBadAnchor
	}
	c, err := open(data)
	if err != nil {
		return nil, "", err
	}
	stories, err := c.stories()
	if err != nil {
		return nil, "", err
	}
	body := stories[0]

	// Every occurrence, in document order, within one paragraph each.
	type match struct {
		segs       []segment
		start, end int
	}
	var matches []match
	for _, segs := range body.paragraphSegments() {
		text := segmentsText(segs)
		for from := 0; ; {
			i := strings.Index(text[from:], at.Quote)
			if i < 0 {
				break
			}
			matches = append(matches, match{segs, from + i, from + i + len(at.Quote)})
			from += i + len(at.Quote)
		}
	}
	switch {
	case len(matches) == 0 || at.Occurrence > len(matches) || at.Occurrence < 0:
		return nil, "", officecomments.ErrQuoteNotFound
	case at.Occurrence == 0 && len(matches) > 1:
		return nil, "", officecomments.ErrQuoteAmbiguous
	}
	m := matches[max(at.Occurrence, 1)-1]

	w, err := c.newComment(first, "", false)
	if err != nil {
		return nil, "", err
	}
	startXML := `<w:commentRangeStart w:id="` + w.wordID + `"/>`
	endXML := `<w:commentRangeEnd w:id="` + w.wordID + `"/>` + c.referenceRun(w.wordID)

	var startCut, endCut cut
	for _, sg := range m.segs {
		if m.start >= sg.start && m.start < sg.end {
			startCut = cut{el: sg.el, offset: m.start - sg.start, xml: startXML}
			if sg.el.local != "t" {
				startCut.offset = 0
			}
		}
		if m.end > sg.start && m.end <= sg.end {
			endCut = cut{el: sg.el, offset: m.end - sg.start, xml: endXML}
			if sg.el.local != "t" {
				endCut.offset = 1
			}
		}
	}
	startRun, endRun := startCut.el.parent, endCut.el.parent
	var edits []edit
	if startRun == endRun {
		edits = append(edits, edit{startRun.start, startRun.end, rewriteRun(body.data, startRun, []cut{startCut, endCut})})
	} else {
		edits = append(edits,
			edit{startRun.start, startRun.end, rewriteRun(body.data, startRun, []cut{startCut})},
			edit{endRun.start, endRun.end, rewriteRun(body.data, endRun, []cut{endCut})})
	}
	c.p.write(body.part, applyEdits(body.data, edits))
	out, err := c.p.bytes()
	if err != nil {
		return nil, "", err
	}
	return out, w.durableID, nil
}

// Reply adds a comment to the thread, marked over the same text as the thread's first comment,
// as Word marks replies.
func (Format) Reply(data []byte, threadID string, cm officecomments.Comment) ([]byte, error) {
	c, err := open(data)
	if err != nil {
		return nil, err
	}
	t := c.thread(threadID)
	if t == nil {
		return nil, officecomments.ErrNoThread
	}
	root := t.comments[0]
	if root.lastParaID == "" {
		return nil, officecomments.ErrOldComment
	}
	w, err := c.newComment(cm, root.lastParaID, t.resolved)
	if err != nil {
		return nil, err
	}
	stories, err := c.stories()
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, wc := range t.comments {
		ids[wc.wordID] = true
	}
	for _, s := range stories {
		var lastStart, lastEnd, lastRef *node
		s.root.walk(func(n *node) bool {
			if n.space != nsW {
				return true
			}
			id, _ := n.attr(nsW, "id")
			if !ids[id] {
				return true
			}
			switch n.local {
			case "commentRangeStart":
				lastStart = n
			case "commentRangeEnd":
				lastEnd = n
			case "commentReference":
				if n.parent != nil && n.parent.is(nsW, "r") {
					lastRef = n.parent
				}
			}
			return true
		}, nil)
		if lastStart == nil && lastEnd == nil && lastRef == nil {
			continue
		}
		var edits []edit
		if lastStart != nil {
			edits = append(edits, edit{lastStart.end, lastStart.end, `<w:commentRangeStart w:id="` + w.wordID + `"/>`})
		}
		endXML := `<w:commentRangeEnd w:id="` + w.wordID + `"/>`
		refXML := c.referenceRun(w.wordID)
		switch {
		case lastRef != nil && (lastEnd == nil || lastRef.end > lastEnd.end && lastRef.parent == lastEnd.parent):
			// The usual case: the end marker, then the reference run, as the browser writes them.
			if lastEnd == nil {
				endXML = ""
			}
			edits = append(edits, edit{lastRef.end, lastRef.end, endXML + refXML})
		case lastRef != nil:
			edits = append(edits, edit{lastEnd.end, lastEnd.end, endXML}, edit{lastRef.end, lastRef.end, refXML})
		case lastEnd != nil:
			edits = append(edits, edit{lastEnd.end, lastEnd.end, endXML})
		}
		c.p.write(s.part, applyEdits(s.data, edits))
		break
	}
	return c.p.bytes()
}

// Resolve marks the thread resolved or open again: done on every comment of it, as the browser
// writes it.
func (Format) Resolve(data []byte, threadID string, resolved bool) ([]byte, error) {
	c, err := open(data)
	if err != nil {
		return nil, err
	}
	t := c.thread(threadID)
	if t == nil {
		return nil, officecomments.ErrNoThread
	}
	if t.comments[0].lastParaID == "" {
		return nil, officecomments.ErrOldComment
	}
	if err := c.setDone(t, resolved); err != nil {
		return nil, err
	}
	return c.p.bytes()
}

func (c *comments) thread(id string) *thread {
	list, _ := c.threads()
	for _, t := range list {
		if t.id == id {
			return t
		}
	}
	return nil
}
