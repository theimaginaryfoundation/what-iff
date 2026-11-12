package drawio

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Default styles for what the package draws, following jgraph's reference for generated diagrams.
const (
	EdgeStyle      = "edgeStyle=orthogonalEdgeStyle;rounded=1;orthogonalLoop=1;jettySize=auto;html=1;"
	NoteStyle      = "shape=note;whiteSpace=wrap;html=1;backgroundOutline=1;size=14;fillColor=#fff2cc;strokeColor=#d6b656;align=left;verticalAlign=top;spacingLeft=6;spacingTop=4;"
	GroupStyle     = "group;"
	noteEdgeExtras = "dashed=1;endArrow=none;"
	// laneHeader is the size of a lane's title band.
	laneHeader = 30
)

// shapeDef is a node shape the tools offer by name, with its style and default size.
type shapeDef struct {
	style         string
	width, height float64
}

var shapes = map[string]shapeDef{
	"rounded":       {"rounded=1;whiteSpace=wrap;html=1;", 140, 60},
	"rectangle":     {"rounded=0;whiteSpace=wrap;html=1;", 140, 60},
	"ellipse":       {"ellipse;whiteSpace=wrap;html=1;", 120, 70},
	"circle":        {"ellipse;whiteSpace=wrap;html=1;aspect=fixed;", 70, 70},
	"rhombus":       {"rhombus;whiteSpace=wrap;html=1;", 160, 90},
	"terminator":    {"rounded=1;whiteSpace=wrap;html=1;arcSize=50;", 140, 50},
	"cylinder":      {"shape=cylinder3;whiteSpace=wrap;html=1;boundedLbl=1;backgroundOutline=1;size=15;", 110, 80},
	"document":      {"shape=mxgraph.flowchart.document;whiteSpace=wrap;html=1;boundedLbl=1;", 140, 70},
	"cloud":         {"ellipse;shape=cloud;whiteSpace=wrap;html=1;", 150, 90},
	"actor":         {"shape=umlActor;verticalLabelPosition=bottom;verticalAlign=top;html=1;outlineConnect=0;", 30, 60},
	"hexagon":       {"shape=hexagon;perimeter=hexagonPerimeter2;whiteSpace=wrap;html=1;fixedSize=1;", 140, 70},
	"parallelogram": {"shape=parallelogram;perimeter=parallelogramPerimeter;whiteSpace=wrap;html=1;fixedSize=1;", 150, 60},
	"text":          {"text;html=1;align=center;verticalAlign=middle;whiteSpace=wrap;", 140, 30},
}

// shapeAliases are other names models use for the shapes.
var shapeAliases = map[string]string{
	"": "rounded", "box": "rectangle", "rect": "rectangle", "process": "rounded", "oval": "ellipse",
	"diamond": "rhombus", "decision": "rhombus", "database": "cylinder", "db": "cylinder",
	"start": "terminator", "end": "terminator", "stadium": "terminator", "person": "actor", "user": "actor",
	"data": "parallelogram", "io": "parallelogram", "label": "text",
}

// ShapeNames lists the shape names nodes take.
func ShapeNames() []string {
	out := make([]string, 0, len(shapes))
	for k := range shapes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func shapeFor(name string) (shapeDef, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if a, ok := shapeAliases[name]; ok {
		name = a
	}
	s, ok := shapes[name]
	if !ok {
		return shapeDef{}, fmt.Errorf("unknown shape %q; use one of %s", name, strings.Join(ShapeNames(), ", "))
	}
	return s, nil
}

// laneColors are the pastel fill and stroke pairs lanes take in turn.
var laneColors = [][2]string{
	{"#dae8fc", "#6c8ebf"}, {"#d5e8d4", "#82b366"}, {"#ffe6cc", "#d79b00"},
	{"#e1d5e7", "#9673a6"}, {"#f8cecc", "#b85450"}, {"#f5f5f5", "#666666"},
}

// laneStyle is a swimlane's style; horizontal lanes (stacked top to bottom) have their title on
// the left.
func laneStyle(i int, titleLeft bool) string {
	c := laneColors[i%len(laneColors)]
	s := "swimlane;whiteSpace=wrap;html=1;startSize=" + strconv.Itoa(laneHeader) + ";"
	if titleLeft {
		s += "horizontal=0;"
	}
	return s + "fillColor=" + c[0] + ";strokeColor=" + c[1] + ";container=1;collapsible=0;pointerEvents=0;"
}

// labelSize grows a shape's default size to fit its label, roughly (draw.io's default font is
// 12px, about 7px a character).
func labelSize(label string, w, h float64) (float64, float64) {
	longest, lines := 0, 0
	for _, l := range strings.Split(label, "\n") {
		lines++
		longest = max(longest, len([]rune(l)))
	}
	need := float64(longest)*7 + 24
	width := min(max(w, need), 240)
	perLine := max(1, int((width-16)/7))
	wrapped := 0
	for _, l := range strings.Split(label, "\n") {
		wrapped += max(1, (len([]rune(l))+perLine-1)/perLine)
	}
	return ceil10(width), ceil10(max(h, float64(wrapped)*18+24))
}

// ceil10 rounds up to draw.io's 10px grid.
func ceil10(f float64) float64 { return math.Ceil(f/10) * 10 }

// cellSpec is a new cell to write.
type cellSpec struct {
	id, value, style, parent, source, target string
	vertex, edge, connectable0               bool
	geom                                     *Geometry
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// xml is the cell as draw.io writes it, on several lines joined by "\n" (re-indented by the
// caller).
func (c cellSpec) xml() string {
	var b strings.Builder
	b.WriteString(`<mxCell id="` + escapeAttr(c.id) + `" value="` + escapeAttr(c.value) + `" style="` + escapeAttr(c.style) + `"`)
	if c.vertex {
		b.WriteString(` vertex="1"`)
	}
	if c.connectable0 {
		b.WriteString(` connectable="0"`)
	}
	if c.edge {
		b.WriteString(` edge="1"`)
	}
	b.WriteString(` parent="` + escapeAttr(c.parent) + `"`)
	if c.source != "" {
		b.WriteString(` source="` + escapeAttr(c.source) + `"`)
	}
	if c.target != "" {
		b.WriteString(` target="` + escapeAttr(c.target) + `"`)
	}
	b.WriteString(">\n  ")
	if c.edge {
		b.WriteString(`<mxGeometry relative="1" as="geometry" />`)
	} else {
		g := c.geom
		b.WriteString(`<mxGeometry x="` + num(g.X) + `" y="` + num(g.Y) + `" width="` + num(g.Width) + `" height="` + num(g.Height) + `" as="geometry" />`)
	}
	b.WriteString("\n</mxCell>")
	return b.String()
}
