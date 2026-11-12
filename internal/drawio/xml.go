package drawio

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

// maxDepth is the deepest the file's elements may nest; draw.io's own files stay far below it.
const maxDepth = 256

// node is an element of the file, with where it is in the file's text, so edits are made on the
// text itself and leave the rest as written (as internal/officecomments does).
type node struct {
	local    string
	attrs    []xml.Attr
	parent   *node
	children []*node
	// text is the element's own character data, trimmed (a compressed <diagram> holds its model
	// as text).
	text string
	// start..startEnd is the start tag; endStart..end the end tag (empty for a self-closing one).
	start, startEnd, endStart, end int
}

// attr is the value of the attribute with the local name, and whether it is there.
func (n *node) attr(local string) (string, bool) {
	for _, a := range n.attrs {
		if a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

// attrOr is the attribute's value, or "" when it is not there.
func (n *node) attrOr(local string) string {
	v, _ := n.attr(local)
	return v
}

func (n *node) selfClosing() bool { return n.endStart == n.end }

// child is the first child element with the local name, or nil.
func (n *node) child(local string) *node {
	for _, c := range n.children {
		if c.local == local {
			return c
		}
	}
	return nil
}

// errDepth means the file nests past maxDepth.
var errDepth = errors.New("the file nests too deeply")

// parseXML reads the file into a tree of its elements with their byte offsets.
func parseXML(data []byte) (*node, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	doc := &node{}
	cur := doc
	depth := 0
	var text strings.Builder
	for {
		off := int(dec.InputOffset())
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		end := int(dec.InputOffset())
		switch t := tok.(type) {
		case xml.StartElement:
			if depth++; depth > maxDepth {
				return nil, errDepth
			}
			n := &node{local: t.Name.Local, attrs: t.Attr, parent: cur, start: off, startEnd: end}
			cur.children = append(cur.children, n)
			cur = n
			text.Reset()
		case xml.EndElement:
			depth--
			cur.endStart, cur.end = off, end
			if len(cur.children) == 0 {
				cur.text = strings.TrimSpace(text.String())
			}
			text.Reset()
			cur = cur.parent
		case xml.CharData:
			text.Write(t)
		}
	}
	if len(doc.children) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	return doc.children[0], nil
}

// attrSpan is where one attribute's value is in a start tag: data[valStart:valEnd], between quotes.
type attrSpan struct {
	name             string
	valStart, valEnd int
}

// scanAttrs finds the attributes of the start tag data[n.start:n.startEnd] with their offsets in
// data. The decoder has already checked the tag, so this only has to find the quotes.
func scanAttrs(data []byte, n *node) []attrSpan {
	tag := data[n.start:n.startEnd]
	i := 1
	for i < len(tag) && !isSpace(tag[i]) && tag[i] != '>' && tag[i] != '/' {
		i++
	}
	var out []attrSpan
	for i < len(tag) {
		for i < len(tag) && isSpace(tag[i]) {
			i++
		}
		if i >= len(tag) || tag[i] == '>' || tag[i] == '/' {
			break
		}
		nameStart := i
		for i < len(tag) && tag[i] != '=' && !isSpace(tag[i]) {
			i++
		}
		name := string(tag[nameStart:i])
		if c := strings.IndexByte(name, ':'); c >= 0 {
			name = name[c+1:]
		}
		for i < len(tag) && (isSpace(tag[i]) || tag[i] == '=') {
			i++
		}
		if i >= len(tag) {
			break
		}
		quote := tag[i]
		i++
		valStart := i
		for i < len(tag) && tag[i] != quote {
			i++
		}
		out = append(out, attrSpan{name: name, valStart: n.start + valStart, valEnd: n.start + i})
		i++
	}
	return out
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// tagName is the element's name as written, prefix included.
func tagName(data []byte, n *node) string {
	tag := data[n.start+1 : n.startEnd]
	i := 0
	for i < len(tag) && !isSpace(tag[i]) && tag[i] != '>' && tag[i] != '/' {
		i++
	}
	return string(tag[:i])
}

var attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;",
	"\n", "&#10;", "\r", "&#13;", "\t", "&#9;")

// escapeAttr escapes s for a quoted attribute, keeping line breaks and tabs as character
// references (a literal one would be read back as a space). Characters XML cannot hold are dropped.
func escapeAttr(s string) string { return attrEscaper.Replace(officecomments.XMLText(s)) }

// edit replaces data[from:to] with text.
type edit struct {
	from, to int
	text     string
}

// applyEdits applies edits that do not overlap; edits at the same place keep their order.
func applyEdits(data []byte, edits []edit) []byte {
	sorted := append([]edit(nil), edits...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].from < sorted[j-1].from; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	var out bytes.Buffer
	at := 0
	for _, e := range sorted {
		if e.from < at {
			// Overlapping edits (a removal inside a removed element): the outer one already won.
			if e.to > at {
				at = e.to
			}
			continue
		}
		out.Write(data[at:e.from])
		out.WriteString(e.text)
		at = e.to
	}
	out.Write(data[at:])
	return out.Bytes()
}

// setAttr is the edit that sets n's attribute to value: in place when it is there, else added at
// the end of the start tag.
func setAttr(data []byte, n *node, name, value string) edit {
	for _, a := range scanAttrs(data, n) {
		if a.name == name {
			return edit{a.valStart, a.valEnd, escapeAttr(value)}
		}
	}
	at := n.startEnd - 1
	if n.selfClosing() {
		at = n.startEnd - 2
	}
	return edit{at, at, " " + name + `="` + escapeAttr(value) + `"`}
}

// appendChild is the edit that inserts xml (lines joined by "\n") as n's last child, indented as
// its last child is, opening a self-closing n.
func appendChild(data []byte, n *node, xml string) edit {
	if n.selfClosing() {
		return edit{n.startEnd - 2, n.startEnd, ">" + xml + "</" + tagName(data, n) + ">"}
	}
	if len(n.children) == 0 {
		return edit{n.endStart, n.endStart, xml}
	}
	last := n.children[len(n.children)-1]
	indent := lineIndent(data, last.start)
	if indent == "" && (last.start == 0 || data[last.start-1] != '\n') {
		return edit{last.end, last.end, xml}
	}
	return edit{last.end, last.end, "\n" + indent + strings.ReplaceAll(xml, "\n", "\n"+indent)}
}

// lineIndent is the spaces and tabs before offset on its line, or "" when anything else is there.
func lineIndent(data []byte, offset int) string {
	ls := offset
	for ls > 0 && (data[ls-1] == ' ' || data[ls-1] == '\t') {
		ls--
	}
	if ls > 0 && data[ls-1] != '\n' {
		return ""
	}
	return string(data[ls:offset])
}

// removeNode is the edit that removes n, with the indentation and line break it sits on when it
// is alone on its line.
func removeNode(data []byte, n *node) edit {
	from, to := n.start, n.end
	ls := from
	for ls > 0 && (data[ls-1] == ' ' || data[ls-1] == '\t') {
		ls--
	}
	le := to
	for le < len(data) && (data[le] == ' ' || data[le] == '\t' || data[le] == '\r') {
		le++
	}
	if (ls == 0 || data[ls-1] == '\n') && le < len(data) && data[le] == '\n' {
		return edit{ls, le + 1, ""}
	}
	return edit{from, to, ""}
}
