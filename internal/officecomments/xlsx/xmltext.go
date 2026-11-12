package xlsx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
)

// node is an element of an XML part with where it is in the part's bytes, so a change can be made
// on the text itself and leave the rest of the part as it was.
type node struct {
	name     xml.Name
	attr     []xml.Attr
	start    int // the '<' of the start tag
	tagEnd   int // just after the start tag
	endStart int // the '<' of the end tag (tagEnd for an empty element)
	end      int // just after the element
	children []*node
	text     string
}

// selfClosing is true for an element written as <a/>.
func (n *node) selfClosing() bool { return n.end == n.tagEnd }

func (n *node) get(local string) string {
	for _, a := range n.attr {
		if a.Name.Space == "" && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// relID is the element's r:id, whatever prefix the relationships namespace has.
func (n *node) relID() string {
	for _, a := range n.attr {
		if a.Name.Local == "id" && strings.HasSuffix(a.Name.Space, "relationships") {
			return a.Value
		}
	}
	return ""
}

func (n *node) kids(local string) []*node {
	var out []*node
	for _, c := range n.children {
		if c.name.Local == local {
			out = append(out, c)
		}
	}
	return out
}

func (n *node) kid(local string) *node {
	for _, c := range n.children {
		if c.name.Local == local {
			return c
		}
	}
	return nil
}

// scan reads a part's elements down to maxDepth (the root is depth 0) with their offsets.
func scan(data []byte, maxDepth int) (*node, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var root *node
	var stack []*node
	for {
		off := int(d.InputOffset())
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{name: t.Name, start: off, tagEnd: int(d.InputOffset())}
			depth := len(stack)
			if depth <= maxDepth {
				n.attr = t.Attr
				if depth > 0 {
					parent := stack[depth-1]
					parent.children = append(parent.children, n)
				} else if root == nil {
					root = n
				}
			}
			stack = append(stack, n)
		case xml.EndElement:
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			n.endStart = off
			n.end = int(d.InputOffset())
		case xml.CharData:
			if len(stack) > 0 && len(stack) <= maxDepth+1 {
				stack[len(stack)-1].text += string(t)
			}
		}
	}
	if root == nil {
		return nil, errors.New("no root element")
	}
	return root, nil
}

// prefix is the "p:" an element is written with in the part, or "".
func prefix(data []byte, n *node) string {
	tag := data[n.start+1 : n.tagEnd]
	end := bytes.IndexAny(tag, " \t\r\n/>")
	if end < 0 {
		end = len(tag)
	}
	qname := string(tag[:end])
	if i := strings.IndexByte(qname, ':'); i >= 0 {
		return qname[:i+1]
	}
	return ""
}

// qname is the element's name as written in the part.
func qname(data []byte, n *node) string {
	return prefix(data, n) + n.name.Local
}

type edit struct {
	at, end int
	text    string
}

func apply(data []byte, edits []edit) []byte {
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].at < edits[j].at })
	var out bytes.Buffer
	last := 0
	for _, e := range edits {
		out.Write(data[last:e.at])
		out.WriteString(e.text)
		last = e.end
	}
	out.Write(data[last:])
	return out.Bytes()
}

// appendChild puts fragment at the end of n's content, opening n up when it is empty (<a/>).
func appendChild(data []byte, n *node, fragment string) edit {
	if n.selfClosing() {
		tag := strings.TrimRight(strings.TrimSuffix(strings.TrimRight(string(data[n.start:n.tagEnd]), " \t\r\n"), "/>"), " \t\r\n")
		return edit{n.start, n.end, tag + ">" + fragment + "</" + qname(data, n) + ">"}
	}
	return edit{n.endStart, n.endStart, fragment}
}

// setAttr sets an unprefixed attribute in a start tag's text.
func setAttr(tag, name, value string) string {
	re := regexp.MustCompile(`\s` + regexp.QuoteMeta(name) + `\s*=\s*("[^"]*"|'[^']*')`)
	attr := ` ` + name + `="` + escapeXML(value) + `"`
	if re.MatchString(tag) {
		return re.ReplaceAllLiteralString(tag, attr)
	}
	if strings.HasSuffix(tag, "/>") {
		return strings.TrimRight(strings.TrimSuffix(tag, "/>"), " \t\r\n") + attr + "/>"
	}
	return strings.TrimSuffix(tag, ">") + attr + ">"
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

// escapeXML escapes text as the browser does, dropping characters XML cannot hold.
func escapeXML(s string) string {
	return xmlEscaper.Replace(strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r == 0x7f || r == 0xfffe || r == 0xffff {
			return -1
		}
		return r
	}, s))
}
