package docx

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

// Namespaces of the parts this package reads.
const (
	nsW      = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsR      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsMC     = "http://schemas.openxmlformats.org/markup-compatibility/2006"
	nsW14    = "http://schemas.microsoft.com/office/word/2010/wordml"
	nsW15    = "http://schemas.microsoft.com/office/word/2012/wordml"
	nsW16CID = "http://schemas.microsoft.com/office/word/2016/wordml/cid"
	nsW16CEX = "http://schemas.microsoft.com/office/word/2018/wordml/cex"
)

// node is an element of a part, with where it is in the part's text, so edits can be made on the
// text itself and leave the rest as written.
type node struct {
	space, local string
	attrs        []xml.Attr
	parent       *node
	children     []*node
	// text is the element's own character data (for w:t and the like).
	text string
	// start..startEnd is the start tag; endStart..end the end tag (empty for a self-closing one).
	start, startEnd, endStart, end int
}

func (n *node) is(space, local string) bool { return n.space == space && n.local == local }

// attr is the value of an attribute by namespace and local name.
func (n *node) attr(space, local string) (string, bool) {
	if n == nil {
		return "", false
	}
	for _, a := range n.attrs {
		if a.Name.Space == space && a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

func (n *node) selfClosing() bool { return n.endStart == n.end }

// walk visits n and its descendants in document order; enter returns false to skip a subtree,
// and leave (if set) runs after a node's children.
func (n *node) walk(enter func(*node) bool, leave func(*node)) {
	if !enter(n) {
		return
	}
	for _, c := range n.children {
		c.walk(enter, leave)
	}
	if leave != nil {
		leave(n)
	}
}

// all is every descendant element with the name, in document order.
func (n *node) all(space, local string) []*node {
	var out []*node
	n.walk(func(x *node) bool {
		if x != n && x.is(space, local) {
			out = append(out, x)
		}
		return true
	}, nil)
	return out
}

// within reports whether n has an ancestor with the name.
func (n *node) within(space, local string) bool {
	for p := n.parent; p != nil; p = p.parent {
		if p.is(space, local) {
			return true
		}
	}
	return false
}

// parseXML reads a part into a tree of its elements with their byte offsets.
func parseXML(data []byte) (*node, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	doc := &node{}
	cur := doc
	depth := 0
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
			if depth++; depth > officecomments.MaxXMLDepth {
				return nil, officecomments.ErrTooLarge
			}
			n := &node{space: t.Name.Space, local: t.Name.Local, attrs: t.Attr, parent: cur, start: off, startEnd: end}
			cur.children = append(cur.children, n)
			cur = n
		case xml.EndElement:
			depth--
			cur.endStart, cur.end = off, end
			cur = cur.parent
		case xml.CharData:
			cur.text += string(t)
		}
	}
	if len(doc.children) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	return doc.children[0], nil
}

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

// escapeXML escapes text for an element or a double-quoted attribute, as the browser's escapeXml.
// Characters XML cannot hold are dropped (officecomments.XMLText).
func escapeXML(s string) string { return escaper.Replace(officecomments.XMLText(s)) }

// edit replaces data[from:to] with text.
type edit struct {
	from, to int
	text     string
}

// applyEdits applies edits that do not overlap; edits at the same place keep their order.
func applyEdits(data []byte, edits []edit) []byte {
	sorted := append([]edit(nil), edits...)
	// Stable insertion sort by position: there are few edits.
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].from < sorted[j-1].from; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	var out bytes.Buffer
	at := 0
	for _, e := range sorted {
		out.Write(data[at:e.from])
		out.WriteString(e.text)
		at = e.to
	}
	out.Write(data[at:])
	return out.Bytes()
}

// withNamespaces adds to a root element's start tag the declarations it lacks, as the browser does.
func withNamespaces(data []byte, root *node, needed [][2]string) []edit {
	tag := string(data[root.start:root.startEnd])
	var add strings.Builder
	for _, ns := range needed {
		if !strings.Contains(tag, " xmlns:"+ns[0]+"=") {
			add.WriteString(` xmlns:` + ns[0] + `="` + ns[1] + `"`)
		}
	}
	if add.Len() == 0 {
		return nil
	}
	at := root.startEnd - 1
	if root.selfClosing() {
		at--
	}
	return []edit{{at, at, add.String()}}
}

// appendChildren inserts xml as the root's last children, opening a self-closing root.
func appendChildren(data []byte, root *node, children string) edit {
	if root.selfClosing() {
		tag := string(data[root.start : root.startEnd-2])
		name := strings.TrimPrefix(strings.Fields(tag)[0], "<")
		return edit{root.startEnd - 2, root.startEnd, ">" + children + "</" + name + ">"}
	}
	return edit{root.endStart, root.endStart, children}
}
