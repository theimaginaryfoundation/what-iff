package pptx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
)

// node is an element of a part, with where it is in the part's text, so a change can be made to
// the text itself and the rest of the part keeps its bytes.
type node struct {
	Name     xml.Name
	Attr     []xml.Attr
	Parent   *node
	Children []*node
	// Start and End are the element's bytes; StartTagEnd is the end of its start tag and
	// EndTagStart the start of its end tag (both End for an empty element written "<x/>").
	Start, StartTagEnd, EndTagStart, End int
	// Text is the character data directly inside the element.
	Text string
	// SelfClosing is an empty element written "<x/>".
	SelfClosing bool
	// prefixes are the namespace prefixes the element declares, by namespace.
	prefixes map[string]string
}

// parseXML reads a part into a tree of its elements, returning the root.
func parseXML(data []byte) (*node, error) {
	base := 0
	if bytes.HasPrefix(data, []byte("\xEF\xBB\xBF")) {
		base = 3
	}
	dec := xml.NewDecoder(bytes.NewReader(data[base:]))
	var root, cur *node
	for {
		start := int(dec.InputOffset()) + base
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		end := int(dec.InputOffset()) + base
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{Name: t.Name, Attr: t.Attr, Parent: cur, Start: start, StartTagEnd: end}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" {
					if n.prefixes == nil {
						n.prefixes = map[string]string{}
					}
					n.prefixes[a.Value] = a.Name.Local
				}
			}
			n.SelfClosing = bytes.HasSuffix(bytes.TrimRight(data[start:end], " \t\r\n"), []byte("/>"))
			if cur != nil {
				cur.Children = append(cur.Children, n)
			} else if root == nil {
				root = n
			}
			cur = n
		case xml.EndElement:
			if cur == nil {
				return nil, errors.New("unbalanced xml")
			}
			if cur.SelfClosing {
				cur.EndTagStart, cur.End = cur.StartTagEnd, cur.StartTagEnd
			} else {
				cur.EndTagStart, cur.End = start, end
			}
			cur = cur.Parent
		case xml.CharData:
			if cur != nil {
				cur.Text += string(t)
			}
		}
	}
	if root == nil {
		return nil, errors.New("no root element")
	}
	return root, nil
}

func (n *node) is(space, local string) bool {
	return n != nil && n.Name.Space == space && n.Name.Local == local
}

// attr is an attribute without a namespace, or "".
func (n *node) attr(local string) string {
	v, _ := n.lookupAttr(local)
	return v
}

func (n *node) lookupAttr(local string) (string, bool) {
	if n == nil {
		return "", false
	}
	for _, a := range n.Attr {
		if a.Name.Space == "" && a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

func (n *node) attrNS(space, local string) string {
	for _, a := range n.Attr {
		if a.Name.Space == space && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// kids are the direct children with the name.
func (n *node) kids(space, local string) []*node {
	if n == nil {
		return nil
	}
	var out []*node
	for _, c := range n.Children {
		if c.is(space, local) {
			out = append(out, c)
		}
	}
	return out
}

func (n *node) kid(space, local string) *node {
	k := n.kids(space, local)
	if len(k) == 0 {
		return nil
	}
	return k[0]
}

// walk visits the element's descendants in document order.
func (n *node) walk(visit func(*node)) {
	for _, c := range n.Children {
		visit(c)
		c.walk(visit)
	}
}

// first is the first descendant with the name.
func (n *node) first(space, local string) *node {
	var found *node
	n.walk(func(c *node) {
		if found == nil && c.is(space, local) {
			found = c
		}
	})
	return found
}

// prefix is the prefix the element has in scope for a namespace, and whether it has one.
func (n *node) prefix(space string) (string, bool) {
	for e := n; e != nil; e = e.Parent {
		if p, ok := e.prefixes[space]; ok {
			return p, true
		}
	}
	return "", false
}

// edit replaces data[Start:End] with Text.
type edit struct {
	Start, End int
	Text       string
}

// applyEdits makes the edits, which must not overlap; edits at one place go in the order given.
func applyEdits(data []byte, edits []edit) []byte {
	sorted := append([]edit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	var out bytes.Buffer
	at := 0
	for _, e := range sorted {
		out.Write(data[at:e.Start])
		out.WriteString(e.Text)
		at = e.End
	}
	out.Write(data[at:])
	return out.Bytes()
}

// appendChild is the edit that adds xml as the element's last child, opening "<x/>" if need be.
func appendChild(data []byte, n *node, xmlText string) edit {
	if !n.SelfClosing {
		return edit{Start: n.EndTagStart, End: n.EndTagStart, Text: xmlText}
	}
	tag := string(data[n.Start:n.StartTagEnd])
	trimmed := strings.TrimRight(tag, " \t\r\n")
	open := strings.TrimRight(strings.TrimSuffix(trimmed, "/>"), " \t\r\n") + ">"
	return edit{Start: n.Start, End: n.End, Text: open + xmlText + "</" + qname(data, n) + ">"}
}

var tagName = regexp.MustCompile(`^<([^\s/>]+)`)

// qname is the element's name as written, with its prefix.
func qname(data []byte, n *node) string {
	m := tagName.FindSubmatch(data[n.Start:n.StartTagEnd])
	if m == nil {
		return n.Name.Local
	}
	return string(m[1])
}

// setAttr is the edit that sets an unprefixed attribute on the element's start tag, or removes it
// when value is nil.
func setAttr(data []byte, n *node, name string, value *string) edit {
	tag := string(data[n.Start:n.StartTagEnd])
	re := regexp.MustCompile(`\s` + regexp.QuoteMeta(name) + `\s*=\s*("[^"]*"|'[^']*')`)
	if loc := re.FindStringIndex(tag); loc != nil {
		text := ""
		if value != nil {
			text = " " + name + `="` + escapeXML(*value) + `"`
		}
		return edit{Start: n.Start + loc[0], End: n.Start + loc[1], Text: text}
	}
	if value == nil {
		return edit{Start: n.Start, End: n.Start}
	}
	// Before the closing ">" or "/>".
	end := len(strings.TrimRight(tag, " \t\r\n"))
	end -= len(">")
	if n.SelfClosing {
		end -= len("/")
	}
	at := n.Start + end
	return edit{Start: at, End: at, Text: " " + name + `="` + escapeXML(*value) + `"`}
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

// escapeXML escapes text as the browser's escapeXml does.
func escapeXML(s string) string { return xmlEscaper.Replace(s) }
