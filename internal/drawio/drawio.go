// Package drawio reads and changes draw.io (.drawio) diagrams element by element (ADR 0x049): it
// lists a file's pages and cells, adds, relabels, restyles, moves and deletes cells, checks a
// diagram is well formed, and lays out a new diagram from a graph with no positions. Everything is
// a pure function over the file's text; changes are made as text where the decoder found each
// element, so whatever is not changed passes through byte for byte.
package drawio

import (
	"errors"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// MediaType is a .drawio file's media type.
const MediaType = "application/vnd.jgraph.mxfile"

// IsDiagramFile reports whether a file with this name and media type is a draw.io diagram: a
// .drawio or .drawio.xml file, or one stored with draw.io's media type.
func IsDiagramFile(name, mediaType string) bool {
	if strings.EqualFold(strings.TrimSpace(mediaType), MediaType) {
		return true
	}
	name = strings.ToLower(name)
	return strings.HasSuffix(name, ".drawio") || strings.HasSuffix(name, ".drawio.xml")
}

// Errors a caller can turn into a message for the model.
var (
	// ErrNotDiagram means the text is not a draw.io file.
	ErrNotDiagram = errors.New("not a draw.io diagram")
	// ErrCompressed means a page is saved in draw.io's compressed form.
	ErrCompressed = errors.New("the diagram is saved compressed; open it in the editor and save it again (diagrams are saved as plain XML), then retry")
)

// Kinds of element.
const (
	KindVertex    = "vertex"
	KindEdge      = "edge"
	KindContainer = "container"
	KindNote      = "note"
	KindLayer     = "layer"
)

// Geometry is a cell's position and size. Children of a container are placed relative to it.
type Geometry struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width,omitempty"`
	Height float64 `json:"height,omitempty"`
}

// Element is one cell of a page as the model sees it.
type Element struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Label    string    `json:"label,omitempty"`
	Parent   string    `json:"parent,omitempty"`
	Geometry *Geometry `json:"geometry,omitempty"`
	Source   string    `json:"source,omitempty"`
	Target   string    `json:"target,omitempty"`
	Style    string    `json:"style,omitempty"`
}

// Page is one page (a <diagram>) of the file.
type Page struct {
	ID       string    `json:"id,omitempty"`
	Name     string    `json:"name,omitempty"`
	Elements []Element `json:"elements"`
}

// doc is a parsed file: its text and the cells of each page with their places in the text.
type doc struct {
	data  []byte
	root  *node
	pages []*page
}

type page struct {
	id, name string
	// rootEl is the model's <root>, where new cells go.
	rootEl *node
	cells  []*cell
	byID   map[string]*cell
}

// cell is an mxCell, or an <object>/<UserObject> wrapping one (which then holds the id and the
// label).
type cell struct {
	outer *node // the element to remove: the wrapper, or the mxCell itself
	mx    *node // the mxCell
	geom  *node // its mxGeometry, or nil

	id, label, style, parent, source, target string
	vertex, edge                             bool
}

// labelNode is where the label lives: the wrapper's label attribute, or the mxCell's value.
func (c *cell) labelNode() (*node, string) {
	if c.outer != c.mx {
		return c.outer, "label"
	}
	return c.mx, "value"
}

func (c *cell) geometry() *Geometry {
	if c.geom == nil {
		return nil
	}
	num := func(k string) float64 {
		f, _ := strconv.ParseFloat(c.geom.attrOr(k), 64)
		return f
	}
	return &Geometry{X: num("x"), Y: num("y"), Width: num("width"), Height: num("height")}
}

func (c *cell) kind() string {
	st := ParseStyle(c.style)
	switch {
	case c.edge:
		return KindEdge
	case !c.vertex && c.parent == "0":
		return KindLayer
	case st.Get("shape") == "note":
		return KindNote
	case st.Has("swimlane") || st.Has("group") || st.Get("container") == "1" || st.Get("shape") == "swimlane":
		return KindContainer
	}
	return KindVertex
}

// isContainer reports whether c holds other cells (a layer, a lane or a group).
func (c *cell) isContainer() bool {
	k := c.kind()
	return k == KindContainer || k == KindLayer
}

func (c *cell) element() Element {
	e := Element{ID: c.id, Kind: c.kind(), Label: PlainLabel(c.label), Parent: c.parent, Style: c.style}
	if c.vertex {
		e.Geometry = c.geometry()
	}
	if c.edge {
		e.Source, e.Target = c.source, c.target
	}
	return e
}

// parse reads a draw.io file: an <mxfile> of <diagram> pages, or a bare <mxGraphModel>.
func parse(data []byte) (*doc, error) {
	root, err := parseXML(data)
	if err != nil {
		if errors.Is(err, errDepth) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrNotDiagram, err)
	}
	d := &doc{data: data, root: root}
	switch root.local {
	case "mxGraphModel":
		p, err := readModel(root)
		if err != nil {
			return nil, err
		}
		d.pages = append(d.pages, p)
	case "mxfile":
		for _, dg := range root.children {
			if dg.local != "diagram" {
				continue
			}
			model := dg.child("mxGraphModel")
			if model == nil {
				if dg.text != "" {
					return nil, ErrCompressed
				}
				return nil, fmt.Errorf("%w: page %q has no mxGraphModel", ErrNotDiagram, dg.attrOr("name"))
			}
			p, err := readModel(model)
			if err != nil {
				return nil, err
			}
			p.id, p.name = dg.attrOr("id"), dg.attrOr("name")
			d.pages = append(d.pages, p)
		}
		if len(d.pages) == 0 {
			return nil, fmt.Errorf("%w: the file has no pages", ErrNotDiagram)
		}
	default:
		return nil, fmt.Errorf("%w: the root element is <%s>, not <mxfile>", ErrNotDiagram, root.local)
	}
	return d, nil
}

func readModel(model *node) (*page, error) {
	r := model.child("root")
	if r == nil {
		return nil, fmt.Errorf("%w: a page has no <root>", ErrNotDiagram)
	}
	p := &page{rootEl: r, byID: map[string]*cell{}}
	for _, n := range r.children {
		c := &cell{outer: n, mx: n}
		switch n.local {
		case "mxCell":
			c.id, c.label = n.attrOr("id"), n.attrOr("value")
		case "object", "UserObject":
			c.mx = n.child("mxCell")
			if c.mx == nil {
				continue
			}
			c.id, c.label = n.attrOr("id"), n.attrOr("label")
		default:
			continue
		}
		mx := c.mx
		c.style, c.parent = mx.attrOr("style"), mx.attrOr("parent")
		c.source, c.target = mx.attrOr("source"), mx.attrOr("target")
		c.vertex, c.edge = mx.attrOr("vertex") == "1", mx.attrOr("edge") == "1"
		for _, g := range mx.children {
			if g.local == "mxGeometry" && g.attrOr("as") == "geometry" {
				c.geom = g
				break
			}
		}
		p.cells = append(p.cells, c)
		if _, dup := p.byID[c.id]; !dup {
			p.byID[c.id] = c
		}
	}
	return p, nil
}

// pageAt picks a page by its name, id or 1-based number; "" is the first page.
func (d *doc) pageAt(ref string) (*page, int, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return d.pages[0], 0, nil
	}
	for i, p := range d.pages {
		if p.name == ref || p.id == ref {
			return p, i, nil
		}
	}
	if n, err := strconv.Atoi(ref); err == nil && n >= 1 && n <= len(d.pages) {
		return d.pages[n-1], n - 1, nil
	}
	names := make([]string, len(d.pages))
	for i, p := range d.pages {
		names[i] = strconv.Quote(p.name)
	}
	return nil, 0, fmt.Errorf("no page %q; the pages are %s (or pass their number, from 1)", ref, strings.Join(names, ", "))
}

// List is the file's pages and their elements, the reserved root cells left out.
func List(data []byte) ([]Page, error) {
	d, err := parse(data)
	if err != nil {
		return nil, err
	}
	out := make([]Page, 0, len(d.pages))
	for _, p := range d.pages {
		pg := Page{ID: p.id, Name: p.name, Elements: []Element{}}
		for _, c := range p.cells {
			if c.id == "0" || (c.kind() == KindLayer && c.id == "1") {
				continue
			}
			pg.Elements = append(pg.Elements, c.element())
		}
		out = append(out, pg)
	}
	return out, nil
}

// Outline is a short text picture of the pages: containers with what is in them, then the
// connectors, one per line.
func Outline(pages []Page) string {
	var b strings.Builder
	for i, p := range pages {
		name := p.Name
		if name == "" {
			name = fmt.Sprintf("Page %d", i+1)
		}
		fmt.Fprintf(&b, "Page %d %q:\n", i+1, name)
		kids := map[string][]Element{}
		ids := map[string]bool{}
		for _, e := range p.Elements {
			ids[e.ID] = true
		}
		var edges []Element
		for _, e := range p.Elements {
			if e.Kind == KindEdge {
				edges = append(edges, e)
				continue
			}
			parent := e.Parent
			if !ids[parent] {
				parent = ""
			}
			kids[parent] = append(kids[parent], e)
		}
		labels := map[string]string{}
		for _, e := range p.Elements {
			labels[e.ID] = e.Label
		}
		var walk func(parent string, depth int)
		walk = func(parent string, depth int) {
			for _, e := range kids[parent] {
				fmt.Fprintf(&b, "%s- [%s] %s", strings.Repeat("  ", depth+1), e.ID, e.Kind)
				if e.Label != "" {
					fmt.Fprintf(&b, " %q", e.Label)
				}
				b.WriteString("\n")
				walk(e.ID, depth+1)
			}
		}
		walk("", 0)
		for _, e := range edges {
			fmt.Fprintf(&b, "  - [%s] %s -> %s", e.ID, endName(e.Source, labels), endName(e.Target, labels))
			if e.Label != "" {
				fmt.Fprintf(&b, " %q", e.Label)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func endName(id string, labels map[string]string) string {
	if id == "" {
		return "(loose end)"
	}
	if l := labels[id]; l != "" {
		return fmt.Sprintf("%s %q", id, l)
	}
	return id
}

var (
	lineBreakTag = regexp.MustCompile(`(?i)<br\s*/?>|</(div|p|li)>`)
	htmlTag      = regexp.MustCompile(`<[^>]*>`)
)

// PlainLabel is a label as plain text: HTML tags dropped (line breaks kept) and entities decoded.
func PlainLabel(label string) string {
	if !strings.ContainsAny(label, "<&") {
		return strings.TrimSpace(label)
	}
	s := lineBreakTag.ReplaceAllString(label, "\n")
	s = htmlTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\u00a0", " ")
	lines := strings.Split(s, "\n")
	out := lines[:0]
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// labelValue is how plain text is stored as a cell's label: as HTML (escaped, line breaks as
// <br>) when the style says the label is HTML, else as it is.
func labelValue(text, style string) string {
	if ParseStyle(style).Get("html") != "1" {
		return text
	}
	return strings.ReplaceAll(html.EscapeString(text), "\n", "<br>")
}
