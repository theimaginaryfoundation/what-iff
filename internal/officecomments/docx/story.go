package docx

import (
	"fmt"
	"regexp"
	"strings"
)

// story is a part with text comments can be in: the body, a header or a footer.
type story struct {
	part string
	kind string // "body", "header" or "footer"
	data []byte
	root *node
}

// stories are the main document and the headers and footers it relates to, in that order.
func (c *comments) stories() ([]*story, error) {
	open := func(part, kind string) (*story, error) {
		data, err := c.p.read(part)
		if err != nil || data == nil {
			return nil, err
		}
		root, err := parseXML(data)
		if err != nil {
			return nil, fmt.Errorf("docx: read %s: %w", part, err)
		}
		return &story{part: part, kind: kind, data: data, root: root}, nil
	}
	body, err := open(c.mainPath, "body")
	if err != nil {
		return nil, err
	}
	if body == nil {
		return nil, fmt.Errorf("docx: no main document")
	}
	out := []*story{body}
	rels, err := c.p.rels(c.mainPath)
	if err != nil {
		return nil, err
	}
	for _, kind := range []string{"header", "footer"} {
		typ := map[string]string{"header": relHeader, "footer": relFooter}[kind]
		for _, r := range rels {
			if r.typ != typ || r.external {
				continue
			}
			s, err := open(resolveTarget(c.mainPath, r.target), kind)
			if err != nil {
				return nil, err
			}
			if s != nil {
				out = append(out, s)
			}
		}
	}
	return out, nil
}

// skipped is content the editor does not show in a story's text: text boxes and the fallback
// copies of alternate content.
func skipped(n *node) bool {
	return n.is(nsW, "txbxContent") || n.is(nsMC, "Fallback")
}

// commentRange is where a comment is in a story's text.
type commentRange struct {
	wordID     string
	start, end int // in the story's text, end exclusive
	paragraph  int // 1-based paragraph the range starts in
}

// textAndRanges is a story's text as the editor's data stream has it (paragraphs ended by \r)
// and the comment ranges in it, in the order the editor reads them.
func (s *story) textAndRanges() (string, []commentRange) {
	var b strings.Builder
	paragraphs := 0
	open := map[string][2]int{}
	closed := map[string]bool{}
	var ranges []commentRange
	s.root.walk(func(n *node) bool {
		if skipped(n) {
			return false
		}
		if n.space != nsW {
			return true
		}
		switch n.local {
		case "t":
			b.WriteString(n.text)
		case "tab":
			if n.parent != nil && n.parent.is(nsW, "r") {
				b.WriteString("\t")
			}
		case "br", "cr":
			b.WriteString("\n")
		case "commentRangeStart":
			id, _ := n.attr(nsW, "id")
			open[id] = [2]int{b.Len(), paragraphs + 1}
		case "commentRangeEnd":
			id, _ := n.attr(nsW, "id")
			at, ok := open[id]
			if !ok {
				break
			}
			delete(open, id)
			closed[id] = true
			end := max(b.Len(), at[0]+1)
			ranges = append(ranges, commentRange{id, at[0], end, at[1]})
		case "commentReference":
			id, _ := n.attr(nsW, "id")
			if _, ok := open[id]; ok || closed[id] {
				break
			}
			at := max(0, b.Len()-1)
			ranges = append(ranges, commentRange{id, at, at + 1, paragraphs + 1})
		}
		return true
	}, func(n *node) {
		if n.is(nsW, "p") {
			b.WriteString("\r")
			paragraphs++
		}
	})
	return b.String(), ranges
}

var (
	controlChars = regexp.MustCompile(`[\x00-\x1f]`)
	whitespace   = regexp.MustCompile(`\s+`)
)

// quoteOf is the text a range covers, as the panel quotes it.
func quoteOf(text string, start, end int) string {
	end = min(end, len(text))
	if start >= end {
		return ""
	}
	q := controlChars.ReplaceAllString(text[start:end], " ")
	return strings.TrimSpace(whitespace.ReplaceAllString(q, " "))
}

// segment is a piece of a paragraph's text and the run element it comes from.
type segment struct {
	el         *node // w:t, w:tab, w:br or w:cr
	run        *node
	start, end int // in the paragraph's text
}

// paragraphSegments are a story's paragraphs as their text and the runs it is in.
func (s *story) paragraphSegments() [][]segment {
	var out [][]segment
	var cur []segment
	text := 0
	s.root.walk(func(n *node) bool {
		if skipped(n) {
			return false
		}
		if n.is(nsW, "p") {
			cur, text = nil, 0
		}
		if n.space != nsW || n.parent == nil || !n.parent.is(nsW, "r") {
			return true
		}
		var t string
		switch n.local {
		case "t":
			t = n.text
		case "tab":
			t = "\t"
		case "br", "cr":
			t = "\n"
		default:
			return true
		}
		cur = append(cur, segment{el: n, run: n.parent, start: text, end: text + len(t)})
		text += len(t)
		return true
	}, func(n *node) {
		if n.is(nsW, "p") {
			out = append(out, cur)
			cur = nil
		}
	})
	return out
}

func segmentsText(segs []segment) string {
	var b strings.Builder
	for _, sg := range segs {
		if sg.el.local == "t" {
			b.WriteString(sg.el.text)
		} else {
			b.WriteString(map[string]string{"tab": "\t", "br": "\n", "cr": "\n"}[sg.el.local])
		}
	}
	return b.String()
}

// cut is a place inside a run to put markup at: before byte offset of el's text (for a w:t),
// or before (0) or after (1) another element.
type cut struct {
	el     *node
	offset int
	xml    string
}

// rewriteRun is the run with each cut's markup put at its place, the run split there into runs
// that keep its properties. A run that would be left empty is dropped.
func rewriteRun(data []byte, r *node, cuts []cut) string {
	prefix := elementPrefix(data, r)
	startTag := string(data[r.start:r.startEnd])
	endTag := string(data[r.endStart:r.end])
	rPr := ""
	var out, cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out.WriteString(startTag + rPr + cur.String() + endTag)
			cur.Reset()
		}
	}
	for _, child := range r.children {
		if child.is(nsW, "rPr") {
			rPr = string(data[child.start:child.end])
			continue
		}
		var mine []cut
		for _, ct := range cuts {
			if ct.el == child {
				mine = append(mine, ct)
			}
		}
		if len(mine) == 0 {
			cur.Write(data[child.start:child.end])
			continue
		}
		// Another element, or a w:t cut only at its ends, is kept as written.
		isText := child.is(nsW, "t")
		whole := true
		for _, ct := range mine {
			whole = whole && (!isText || ct.offset == 0 || ct.offset == len(child.text))
		}
		if whole {
			after := func(ct cut) bool {
				if isText {
					return ct.offset > 0 && ct.offset == len(child.text)
				}
				return ct.offset > 0
			}
			for _, ct := range mine {
				if !after(ct) {
					flush()
					out.WriteString(ct.xml)
				}
			}
			cur.Write(data[child.start:child.end])
			for _, ct := range mine {
				if after(ct) {
					flush()
					out.WriteString(ct.xml)
				}
			}
			continue
		}
		text := child.text
		at := 0
		for _, ct := range mine {
			if part := text[at:ct.offset]; part != "" {
				cur.WriteString(textElement(prefix, part))
			}
			flush()
			out.WriteString(ct.xml)
			at = ct.offset
		}
		if part := text[at:]; part != "" {
			cur.WriteString(textElement(prefix, part))
		}
	}
	flush()
	return out.String()
}

func textElement(prefix, text string) string {
	return "<" + prefix + `t xml:space="preserve">` + escapeXML(text) + "</" + prefix + "t>"
}

// elementPrefix is the prefix an element is written with, with its colon ("w:"), or "".
func elementPrefix(data []byte, n *node) string {
	tag := string(data[n.start+1 : n.startEnd])
	name := strings.FieldsFunc(tag, func(r rune) bool { return r == ' ' || r == '>' || r == '/' || r == '\t' || r == '\n' || r == '\r' })[0]
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[:i+1]
	}
	return ""
}
