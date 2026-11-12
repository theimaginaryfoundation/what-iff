package drawio

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Placement is a position and size to set; nil fields are left as they are (or picked, for a new
// cell).
type Placement struct {
	X, Y, Width, Height *float64
}

// NewCell is a vertex to add: a node, a container (a lane or a group) or a note.
type NewCell struct {
	// Kind is KindVertex (default), KindContainer or KindNote.
	Kind string
	// ID is the new cell's id; empty picks a short unused one.
	ID    string
	Label string
	// Shape names a node shape (ShapeNames); Container is "swimlane" (default) or "group".
	Shape     string
	Container string
	// Style replaces the default style the kind and shape give, when set.
	Style string
	// Parent is the container or layer to put it in; empty is the page's default layer.
	Parent string
	Placement
}

// NewEdge is a connector to add between two cells.
type NewEdge struct {
	ID, Source, Target, Label string
	// Style replaces the default connector style, when set.
	Style string
}

// ErrInput is a change asked for something that does not fit the diagram (an unknown id, a bad
// style); the message says what.
var ErrInput = errors.New("invalid change")

func inputErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInput, fmt.Sprintf(format, args...))
}

// change parses data, picks the page, lets fn compute edits, applies them, and checks the result
// brings no new problems. A diagram that already had problems can still be changed.
func change(data []byte, pageRef string, fn func(d *doc, p *page) ([]edit, error)) ([]byte, error) {
	d, err := parse(data)
	if err != nil {
		return nil, err
	}
	p, _, err := d.pageAt(pageRef)
	if err != nil {
		return nil, inputErr("%v", err)
	}
	edits, err := fn(d, p)
	if err != nil {
		return nil, err
	}
	out := applyEdits(data, edits)
	after, err := parse(out)
	if err != nil {
		return nil, fmt.Errorf("the change would break the file: %w", err)
	}
	before := map[string]bool{}
	for _, pr := range d.validate() {
		before[pr.String()] = true
	}
	var fresh []Problem
	for _, pr := range after.validate() {
		if !before[pr.String()] {
			fresh = append(fresh, pr)
		}
	}
	if err := problemsError(fresh); err != nil {
		return nil, err
	}
	return out, nil
}

// newID is an id no cell in the file has: prefix and the lowest free number.
func (d *doc) newID(prefix string) string {
	for n := 1; ; n++ {
		id := prefix + strconv.Itoa(n)
		if !d.hasID(id) {
			return id
		}
	}
}

func (d *doc) hasID(id string) bool {
	for _, p := range d.pages {
		if p.byID[id] != nil {
			return true
		}
	}
	return false
}

// pickID is the asked-for id when free, or a new one.
func (d *doc) pickID(asked, prefix string) (string, error) {
	asked = strings.TrimSpace(asked)
	if asked == "" {
		return d.newID(prefix), nil
	}
	if d.hasID(asked) {
		return "", inputErr("the id %q is already used", asked)
	}
	return asked, nil
}

// defaultLayer is the layer new cells go in: "1", or else the first layer.
func (p *page) defaultLayer() string {
	if c := p.byID["1"]; c != nil {
		return "1"
	}
	for _, c := range p.cells {
		if c.kind() == KindLayer {
			return c.id
		}
	}
	return "1"
}

func (p *page) find(id string) (*cell, error) {
	c := p.byID[strings.TrimSpace(id)]
	if c == nil {
		return nil, inputErr("no element %q on this page; list the diagram to see the ids", id)
	}
	return c, nil
}

// container is the cell a new child goes in: a layer or a container.
func (p *page) container(id string) (*cell, error) {
	if strings.TrimSpace(id) == "" {
		id = p.defaultLayer()
	}
	c, err := p.find(id)
	if err != nil {
		return nil, err
	}
	if !c.isContainer() {
		return nil, inputErr("%q is a %s, not a container or layer", id, c.kind())
	}
	return c, nil
}

// inset is where a container's children start: past its title band, with a margin.
func inset(c *cell) (x, y float64) {
	if c.kind() != KindContainer {
		return 40, 40
	}
	st := ParseStyle(c.style)
	if !st.Has("swimlane") && st.Get("shape") != "swimlane" {
		return 0, 0
	}
	start := 23.0
	if v, err := strconv.ParseFloat(st.Get("startSize"), 64); err == nil {
		start = v
	}
	if st.Get("horizontal") == "0" {
		return start + 20, 20
	}
	return 20, start + 20
}

// AddCell adds a node, container or note, returning the new file and the cell's id. Without a
// position it goes to the right of what is already in its parent.
func AddCell(data []byte, pageRef string, nc NewCell) ([]byte, string, error) {
	var id string
	out, err := change(data, pageRef, func(d *doc, p *page) ([]edit, error) {
		parent, err := p.container(nc.Parent)
		if err != nil {
			return nil, err
		}
		var (
			style  string
			w, h   float64
			prefix string
		)
		switch nc.Kind {
		case "", KindVertex:
			s, err := shapeFor(nc.Shape)
			if err != nil {
				return nil, inputErr("%v", err)
			}
			style, prefix = s.style, "n"
			w, h = labelSize(nc.Label, s.width, s.height)
		case KindNote:
			style, prefix = NoteStyle, "note"
			w, h = labelSize(nc.Label, 180, 80)
		case KindContainer:
			switch strings.ToLower(strings.TrimSpace(nc.Container)) {
			case "", "swimlane", "lane":
				style, prefix = laneStyle(p.countKind(KindContainer), false), "lane"
			case "group":
				style, prefix = GroupStyle, "group"
			default:
				return nil, inputErr("container must be \"swimlane\" or \"group\"")
			}
			w, h = 400, 200
		default:
			return nil, inputErr("unknown kind %q", nc.Kind)
		}
		if s := strings.TrimSpace(nc.Style); s != "" {
			if err := CheckStyle(s); err != nil {
				return nil, inputErr("bad style: %v", err)
			}
			style = s
		}
		if id, err = d.pickID(nc.ID, prefix); err != nil {
			return nil, err
		}
		g := p.place(parent, nc.Placement, w, h)
		spec := cellSpec{id: id, value: labelValue(nc.Label, style), style: style, parent: parent.id, vertex: true, geom: &g}
		if nc.Kind == KindContainer && ParseStyle(style).Has("group") {
			spec.connectable0 = true
		}
		edits := []edit{appendChild(d.data, p.rootEl, spec.xml())}
		return append(edits, growToFit(d.data, parent, g)...), nil
	})
	if err != nil {
		return nil, "", err
	}
	return out, id, nil
}

func (p *page) countKind(kind string) int {
	n := 0
	for _, c := range p.cells {
		if c.kind() == kind {
			n++
		}
	}
	return n
}

// place is a new cell's geometry in parent: the asked-for placement, else to the right of the
// rightmost cell already there, level with it.
func (p *page) place(parent *cell, pl Placement, w, h float64) Geometry {
	g := Geometry{Width: w, Height: h}
	if pl.Width != nil && *pl.Width > 0 {
		g.Width = *pl.Width
	}
	if pl.Height != nil && *pl.Height > 0 {
		g.Height = *pl.Height
	}
	x, y := inset(parent)
	var right *Geometry
	for _, c := range p.cells {
		if c.parent != parent.id || !c.vertex {
			continue
		}
		if cg := c.geometry(); cg != nil && (right == nil || cg.X+cg.Width > right.X+right.Width) {
			right = cg
		}
	}
	if right != nil {
		x, y = right.X+right.Width+60, right.Y
	}
	if pl.X != nil {
		x = *pl.X
	}
	if pl.Y != nil {
		y = *pl.Y
	}
	g.X, g.Y = x, y
	return g
}

// growToFit widens or heightens a container so a child placed at g fits inside it.
func growToFit(data []byte, parent *cell, g Geometry) []edit {
	if parent.kind() != KindContainer || parent.geom == nil {
		return nil
	}
	pg := parent.geometry()
	var edits []edit
	if need := g.X + g.Width + 20; need > pg.Width {
		edits = append(edits, setAttr(data, parent.geom, "width", num(need)))
	}
	if need := g.Y + g.Height + 20; need > pg.Height {
		edits = append(edits, setAttr(data, parent.geom, "height", num(need)))
	}
	return edits
}

// AddEdge adds a connector, returning the new file and its id. It is filed in the innermost
// container that holds both ends.
func AddEdge(data []byte, pageRef string, ne NewEdge) ([]byte, string, error) {
	var id string
	out, err := change(data, pageRef, func(d *doc, p *page) ([]edit, error) {
		src, err := p.find(ne.Source)
		if err != nil {
			return nil, err
		}
		dst, err := p.find(ne.Target)
		if err != nil {
			return nil, err
		}
		for _, c := range []*cell{src, dst} {
			if c.kind() == KindLayer || c.id == "0" {
				return nil, inputErr("%q is a layer; connect elements on it", c.id)
			}
		}
		style := EdgeStyle
		if src.kind() == KindNote || dst.kind() == KindNote {
			style += noteEdgeExtras
		}
		if s := strings.TrimSpace(ne.Style); s != "" {
			if err := CheckStyle(s); err != nil {
				return nil, inputErr("bad style: %v", err)
			}
			style = s
		}
		if id, err = d.pickID(ne.ID, "e"); err != nil {
			return nil, err
		}
		spec := cellSpec{id: id, value: labelValue(ne.Label, style), style: style, parent: p.commonContainer(src, dst), edge: true, source: src.id, target: dst.id}
		return []edit{appendChild(d.data, p.rootEl, spec.xml())}, nil
	})
	if err != nil {
		return nil, "", err
	}
	return out, id, nil
}

// commonContainer is the innermost container or layer both cells are in.
func (p *page) commonContainer(a, b *cell) string {
	above := map[string]bool{}
	for c := p.byID[a.parent]; c != nil; c = p.byID[c.parent] {
		above[c.id] = true
		if c.parent == "" || c.parent == c.id {
			break
		}
	}
	for c := p.byID[b.parent]; c != nil; c = p.byID[c.parent] {
		if above[c.id] && c.isContainer() {
			return c.id
		}
		if c.parent == "" || c.parent == c.id {
			break
		}
	}
	return p.defaultLayer()
}

// SetLabel sets an element's label (plain text; stored as HTML when its style says so).
func SetLabel(data []byte, pageRef, id, label string) ([]byte, error) {
	return change(data, pageRef, func(d *doc, p *page) ([]edit, error) {
		c, err := p.find(id)
		if err != nil {
			return nil, err
		}
		n, attr := c.labelNode()
		return []edit{setAttr(d.data, n, attr, labelValue(label, c.style))}, nil
	})
}

// SetStyle changes an element's style: merged into the current one ("key=value" sets, "key="
// removes, a bare name is added, "-name" removes it), or replacing it.
func SetStyle(data []byte, pageRef, id, style string, replace bool) ([]byte, error) {
	if err := CheckStyle(style); err != nil {
		return nil, inputErr("bad style: %v", err)
	}
	return change(data, pageRef, func(d *doc, p *page) ([]edit, error) {
		c, err := p.find(id)
		if err != nil {
			return nil, err
		}
		next := ParseStyle(style).String()
		if !replace {
			next = ParseStyle(c.style).Merge(ParseStyle(style)).String()
		}
		return []edit{setAttr(d.data, c.mx, "style", next)}, nil
	})
}

// Move sets an element's position and/or size; a container's children move with it.
func Move(data []byte, pageRef, id string, pl Placement) ([]byte, error) {
	if pl.X == nil && pl.Y == nil && pl.Width == nil && pl.Height == nil {
		return nil, inputErr("give x and y, and/or width and height")
	}
	for _, v := range []*float64{pl.Width, pl.Height} {
		if v != nil && *v <= 0 {
			return nil, inputErr("width and height must be more than 0")
		}
	}
	return change(data, pageRef, func(d *doc, p *page) ([]edit, error) {
		c, err := p.find(id)
		if err != nil {
			return nil, err
		}
		if !c.vertex {
			return nil, inputErr("%q is a %s; only shapes, containers and notes have a position", id, c.kind())
		}
		vals := []struct {
			name string
			v    *float64
		}{{"x", pl.X}, {"y", pl.Y}, {"width", pl.Width}, {"height", pl.Height}}
		if c.geom == nil {
			var b strings.Builder
			b.WriteString("<mxGeometry")
			for _, a := range vals {
				if a.v != nil {
					b.WriteString(" " + a.name + `="` + num(*a.v) + `"`)
				}
			}
			b.WriteString(` as="geometry" />`)
			return []edit{appendChild(d.data, c.mx, b.String())}, nil
		}
		var edits []edit
		for _, a := range vals {
			if a.v != nil {
				edits = append(edits, setAttr(d.data, c.geom, a.name, num(*a.v)))
			}
		}
		return edits, nil
	})
}

// Delete removes an element, everything inside it (a container's children, an edge's labels) and
// the connectors left without an end. It returns the new file and the ids removed.
func Delete(data []byte, pageRef, id string) ([]byte, []string, error) {
	var removed []string
	out, err := change(data, pageRef, func(d *doc, p *page) ([]edit, error) {
		c, err := p.find(id)
		if err != nil {
			return nil, err
		}
		if c.id == "0" || c.id == p.defaultLayer() {
			return nil, inputErr("%q is the page's root or default layer and cannot be deleted", c.id)
		}
		gone := map[string]bool{c.id: true}
		for grew := true; grew; {
			grew = false
			for _, x := range p.cells {
				if gone[x.id] {
					continue
				}
				if gone[x.parent] || (x.edge && (gone[x.source] || gone[x.target])) {
					gone[x.id] = true
					grew = true
				}
			}
		}
		var edits []edit
		for _, x := range p.cells {
			if gone[x.id] {
				removed = append(removed, x.id)
				edits = append(edits, removeNode(d.data, x.outer))
			}
		}
		return edits, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return out, removed, nil
}
