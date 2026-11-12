package drawio

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// sample is a diagram as draw.io saves it, with things the package must leave alone: a prefixed
// attribute, an unknown one, single quotes, an <object> wrapper, a second page and a layer.
const sample = `<?xml version="1.0" encoding="UTF-8"?>
<mxfile host="Electron" xmlns:x="urn:example" x:keep="yes">
  <diagram id="p1" name="Flow">
    <mxGraphModel dx="1000" dy="800" grid="1">
      <root>
        <mxCell id="0" />
        <mxCell id="1" parent="0" />
        <mxCell id="lane" value="Team" style="swimlane;horizontal=0;startSize=30;" vertex="1" parent="1">
          <mxGeometry x="40" y="40" width="300" height="120" as="geometry" />
        </mxCell>
        <mxCell id="a" value="&lt;b&gt;Start&lt;/b&gt;&lt;br&gt;here" style="rounded=1;html=1;" vertex="1" parent="lane" custom='kept'>
          <mxGeometry x="60" y="30" width="120" height="60" as="geometry" />
        </mxCell>
        <object label="Wrapped" id="b" tooltip="tip">
          <mxCell style="ellipse;whiteSpace=wrap;" vertex="1" parent="1">
            <mxGeometry x="400" y="60" width="80" height="80" as="geometry" />
          </mxCell>
        </object>
        <mxCell id="e1" value="go" style="edgeStyle=orthogonalEdgeStyle;" edge="1" parent="1" source="a" target="b">
          <mxGeometry relative="1" as="geometry" />
        </mxCell>
        <mxCell id="note" value="Remember" style="shape=note;" vertex="1" parent="1">
          <mxGeometry x="40" y="200" width="160" height="80" as="geometry" />
        </mxCell>
      </root>
    </mxGraphModel>
  </diagram>
  <diagram id="p2" name="Second">
    <mxGraphModel>
      <root>
        <mxCell id="0" />
        <mxCell id="1" parent="0" />
        <mxCell id="L2" value="Overlay" parent="0" />
        <mxCell id="x" value="X" style="" vertex="1" parent="L2">
          <mxGeometry x="10" y="10" width="40" height="40" as="geometry" />
        </mxCell>
      </root>
    </mxGraphModel>
  </diagram>
</mxfile>
`

func fptr(f float64) *float64 { return &f }

func TestList(t *testing.T) {
	pages, err := List([]byte(sample))
	require.NoError(t, err)
	require.Len(t, pages, 2)
	require.Equal(t, "Flow", pages[0].Name)
	byID := map[string]Element{}
	for _, e := range pages[0].Elements {
		byID[e.ID] = e
	}
	require.NotContains(t, byID, "0", "the reserved root cells are left out")
	require.NotContains(t, byID, "1")
	require.Equal(t, KindContainer, byID["lane"].Kind)
	require.Equal(t, Element{ID: "a", Kind: KindVertex, Label: "Start\nhere", Parent: "lane", Style: "rounded=1;html=1;",
		Geometry: &Geometry{X: 60, Y: 30, Width: 120, Height: 60}}, byID["a"])
	require.Equal(t, "Wrapped", byID["b"].Label, "a wrapper holds the id and the label")
	require.Equal(t, KindVertex, byID["b"].Kind)
	require.Equal(t, KindEdge, byID["e1"].Kind)
	require.Equal(t, "a", byID["e1"].Source)
	require.Equal(t, "b", byID["e1"].Target)
	require.Nil(t, byID["e1"].Geometry)
	require.Equal(t, KindNote, byID["note"].Kind)

	require.Equal(t, KindLayer, pages[1].Elements[0].Kind, "a second layer is listed")
	outline := Outline(pages)
	require.Contains(t, outline, "Page 1 \"Flow\":\n  - [lane] container \"Team\"\n    - [a] vertex \"Start\\nhere\"\n")
	require.Contains(t, outline, `[e1] a "Start\nhere" -> b "Wrapped" "go"`)
	require.Contains(t, outline, "Page 2 \"Second\":\n  - [L2] layer \"Overlay\"\n    - [x] vertex \"X\"\n")
}

func TestParseRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want error
	}{
		"compressed": {`<mxfile><diagram id="a" name="P">7VZNb5tAEP01HBuxQIhzbGicNlKlqj70vIEJrLKwaBnbuL++szDLp53mo+dJlmSel3fszs6+0vY+qfcrP8PNRNqUz0lB7aAb6wN6mT6aZoAeYBDJUUjE91L0tC21Oo8pY</diagram></mxfile>`, ErrCompressed},
		"not xml":    {`hello`, ErrNotDiagram},
		"other root": {`<svg></svg>`, ErrNotDiagram},
		"no pages":   {`<mxfile></mxfile>`, ErrNotDiagram},
		"no root":    {`<mxfile><diagram><mxGraphModel/></diagram></mxfile>`, ErrNotDiagram},
		"empty":      {``, ErrNotDiagram},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := List([]byte(tc.in))
			require.ErrorIs(t, err, tc.want)
			_, err = SetLabel([]byte(tc.in), "", "a", "x")
			require.ErrorIs(t, err, tc.want, "writes refuse it too")
		})
	}
	deep := strings.Repeat("<a>", maxDepth+1) + strings.Repeat("</a>", maxDepth+1)
	_, err := List([]byte(deep))
	require.ErrorIs(t, err, errDepth)
}

func TestBareGraphModel(t *testing.T) {
	in := `<mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="v" value="V" vertex="1" parent="1"/></root></mxGraphModel>`
	pages, err := List([]byte(in))
	require.NoError(t, err)
	require.Len(t, pages, 1)
	require.Equal(t, "v", pages[0].Elements[0].ID)
	out, err := Move([]byte(in), "", "v", Placement{X: fptr(5), Y: fptr(6)})
	require.NoError(t, err)
	require.Contains(t, string(out), `<mxCell id="v" value="V" vertex="1" parent="1"><mxGeometry x="5" y="6" as="geometry" /></mxCell>`, "a self-closing cell is opened for its geometry")
}

// onlyChanged checks out is in with exactly one substring replaced: everything else byte for byte.
func onlyChanged(t *testing.T, in, out []byte, from, to string) {
	t.Helper()
	require.Equal(t, 1, strings.Count(string(in), from), "test setup: %q must be unique", from)
	require.Equal(t, strings.Replace(string(in), from, to, 1), string(out))
}

func TestSetLabel(t *testing.T) {
	in := []byte(sample)
	out, err := SetLabel(in, "", "a", "Begin <now> & go\nnext")
	require.NoError(t, err)
	onlyChanged(t, in, out, `value="&lt;b&gt;Start&lt;/b&gt;&lt;br&gt;here"`, `value="Begin &amp;lt;now&amp;gt; &amp;amp; go&lt;br&gt;next"`)
	pages, _ := List(out)
	require.Equal(t, "Begin <now> & go\nnext", find(pages[0], "a").Label, "an HTML label reads back as the text set")

	out, err = SetLabel(in, "", "b", "Re\"named")
	require.NoError(t, err)
	onlyChanged(t, in, out, `label="Wrapped"`, `label="Re&quot;named"`)

	out, err = SetLabel(in, "", "note", "two\nlines\x0b")
	require.NoError(t, err)
	onlyChanged(t, in, out, `value="Remember"`, `value="two&#10;lines"`)

	out, err = SetLabel(in, "Second", "x", "Y")
	require.NoError(t, err)
	onlyChanged(t, in, out, `value="X"`, `value="Y"`)

	// A cell with no value attribute gets one.
	out, err = SetLabel(in, "", "lane", "")
	require.NoError(t, err)
	require.Contains(t, string(out), `id="lane" value="" style`)
	noValue := strings.Replace(sample, `<mxCell id="note" value="Remember" style`, `<mxCell id="note" style`, 1)
	out, err = SetLabel([]byte(noValue), "", "note", "Hi")
	require.NoError(t, err)
	require.Contains(t, string(out), `<mxCell id="note" style="shape=note;" vertex="1" parent="1" value="Hi">`)

	_, err = SetLabel(in, "", "nope", "x")
	require.ErrorIs(t, err, ErrInput)
	_, err = SetLabel(in, "Third", "a", "x")
	require.ErrorIs(t, err, ErrInput)
	require.Contains(t, err.Error(), `"Flow", "Second"`)
}

func find(p Page, id string) Element {
	for _, e := range p.Elements {
		if e.ID == id {
			return e
		}
	}
	return Element{}
}

func TestSetStyle(t *testing.T) {
	in := []byte(sample)
	for name, tc := range map[string]struct {
		id, style string
		replace   bool
		want      string
	}{
		"merge sets and adds": {"a", "rounded=0;fillColor=#dae8fc", false, `style="rounded=0;html=1;fillColor=#dae8fc;"`},
		"empty value removes": {"a", "rounded=", false, `style="html=1;"`},
		"bare name added":     {"a", "dashed", false, `style="rounded=1;html=1;dashed;"`},
		"bare name removed":   {"b", "-ellipse", false, `style="whiteSpace=wrap;"`},
		"replace":             {"a", "text;html=1", true, `style="text;html=1;"`},
		"data uri value":      {"a", "image=data:image/png,AAA=", true, `style="image=data:image/png,AAA=;"`},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := SetStyle(in, "", tc.id, tc.style, tc.replace)
			require.NoError(t, err)
			require.Contains(t, string(out), tc.want)
		})
	}
	for _, bad := range []string{"=1", "fill color=red", "a b"} {
		_, err := SetStyle(in, "", "a", bad, false)
		require.ErrorIs(t, err, ErrInput, bad)
	}
}

func TestMove(t *testing.T) {
	in := []byte(sample)
	out, err := Move(in, "", "a", Placement{X: fptr(70), Width: fptr(150.5)})
	require.NoError(t, err)
	onlyChanged(t, in, out, `<mxGeometry x="60" y="30" width="120" height="60"`, `<mxGeometry x="70" y="30" width="150.5" height="60"`)

	_, err = Move(in, "", "e1", Placement{X: fptr(1)})
	require.ErrorIs(t, err, ErrInput, "edges have no position")
	_, err = Move(in, "", "a", Placement{})
	require.ErrorIs(t, err, ErrInput)
	_, err = Move(in, "", "a", Placement{Width: fptr(0)})
	require.ErrorIs(t, err, ErrInput)
}

func TestAddCell(t *testing.T) {
	in := []byte(sample)
	out, id, err := AddCell(in, "", NewCell{Label: "Next"})
	require.NoError(t, err)
	require.Equal(t, "n1", id)
	pages, err := List(out)
	require.NoError(t, err)
	got := find(pages[0], "n1")
	require.Equal(t, "1", got.Parent)
	require.Equal(t, &Geometry{X: 540, Y: 60, Width: 140, Height: 60}, got.Geometry, "right of the rightmost cell in the layer, level with it")
	require.True(t, strings.HasPrefix(string(out), sample[:strings.Index(sample, "      </root>")-1]), "everything before the new cell is unchanged")
	require.Contains(t, string(out), "        </mxCell>\n        <mxCell id=\"n1\" value=\"Next\" style=\"rounded=1;whiteSpace=wrap;html=1;\" vertex=\"1\" parent=\"1\">\n          <mxGeometry", "indented like its siblings")

	// In a lane: placed past what is there, and the lane grows to fit.
	out, id, err = AddCell(in, "", NewCell{Label: "In lane", Parent: "lane", Shape: "decision"})
	require.NoError(t, err)
	pages, _ = List(out)
	got = find(pages[0], id)
	require.Equal(t, &Geometry{X: 240, Y: 30, Width: 160, Height: 90}, got.Geometry)
	require.Equal(t, &Geometry{X: 40, Y: 40, Width: 420, Height: 140}, find(pages[0], "lane").Geometry)

	out, id, err = AddCell(in, "", NewCell{Kind: KindNote, Label: "Hi", X: fptr(0), Y: fptr(0)})
	require.NoError(t, err)
	require.Equal(t, "note1", id)
	pages, _ = List(out)
	require.Equal(t, KindNote, find(pages[0], id).Kind)
	require.Equal(t, 0.0, find(pages[0], id).Geometry.X)

	out, id, err = AddCell(in, "", NewCell{Kind: KindContainer, Container: "group", ID: "g"})
	require.NoError(t, err)
	require.Equal(t, "g", id)
	require.Contains(t, string(out), `style="group;" vertex="1" connectable="0" parent="1"`)

	out, id, err = AddCell(in, "", NewCell{Label: "Styled", Style: "ellipse;fillColor=#f00"})
	require.NoError(t, err)
	require.Contains(t, string(out), `id="`+id+`" value="Styled" style="ellipse;fillColor=#f00"`, "the label is plain when the style is not HTML")

	for name, nc := range map[string]NewCell{
		"used id":         {ID: "a"},
		"unknown parent":  {Parent: "zzz"},
		"parent a shape":  {Parent: "a"},
		"unknown shape":   {Shape: "blob"},
		"bad style":       {Style: "a b"},
		"bad container":   {Kind: KindContainer, Container: "box"},
		"unknown kind":    {Kind: "edge"},
		"other page's id": {ID: "x"},
	} {
		_, _, err := AddCell(in, "", nc)
		require.ErrorIs(t, err, ErrInput, name)
	}
}

func TestAddEdge(t *testing.T) {
	in := []byte(sample)
	out, id, err := AddEdge(in, "", NewEdge{Source: "b", Target: "note", Label: "see"})
	require.NoError(t, err)
	require.Equal(t, "e2", id)
	require.Contains(t, string(out), `<mxCell id="e2" value="see" style="`+EdgeStyle+`dashed=1;endArrow=none;" edge="1" parent="1" source="b" target="note">`+"\n"+`          <mxGeometry relative="1" as="geometry" />`)

	// Both ends in the lane: the edge is filed in the lane.
	out, id, err = AddCell(in, "", NewCell{Parent: "lane", ID: "c"})
	require.NoError(t, err)
	out, _, err = AddEdge(out, "", NewEdge{Source: "a", Target: id})
	require.NoError(t, err)
	require.Contains(t, string(out), `edge="1" parent="lane" source="a" target="c"`)

	for name, ne := range map[string]NewEdge{
		"unknown source": {Source: "zz", Target: "a"},
		"unknown target": {Source: "a", Target: "zz"},
		"layer end":      {Source: "1", Target: "a"},
		"used id":        {ID: "a", Source: "a", Target: "b"},
	} {
		_, _, err := AddEdge(in, "", ne)
		require.ErrorIs(t, err, ErrInput, name)
	}
}

func TestDelete(t *testing.T) {
	in := []byte(sample)
	out, removed, err := Delete(in, "", "lane")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"lane", "a", "e1"}, removed, "its child, and the edge left without an end")
	pages, err := List(out)
	require.NoError(t, err)
	var ids []string
	for _, e := range pages[0].Elements {
		ids = append(ids, e.ID)
	}
	require.Equal(t, []string{"b", "note"}, ids)
	require.Contains(t, string(out), "<mxCell id=\"1\" parent=\"0\" />\n        <object label=\"Wrapped\"", "removed with their lines")

	out, removed, err = Delete(in, "", "b")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"b", "e1"}, removed, "a wrapped cell goes with its wrapper")
	require.NotContains(t, string(out), "<object")

	for _, id := range []string{"0", "1", "zz"} {
		_, _, err := Delete(in, "", id)
		require.ErrorIs(t, err, ErrInput, id)
	}
}

func TestValidate(t *testing.T) {
	wrap := func(cells string) string {
		return `<mxfile><diagram name="P"><mxGraphModel><root>` + cells + `</root></mxGraphModel></diagram></mxfile>`
	}
	base := `<mxCell id="0"/><mxCell id="1" parent="0"/>`
	geo := `<mxGeometry relative="1" as="geometry"/>`
	for name, tc := range map[string]struct {
		cells, want string
	}{
		"fine":            {base + `<mxCell id="a" vertex="1" parent="1"/>`, ""},
		"duplicate id":    {base + `<mxCell id="a" vertex="1" parent="1"/><mxCell id="a" vertex="1" parent="1"/>`, "used more than once"},
		"no id":           {base + `<mxCell vertex="1" parent="1"/>`, "has no id"},
		"no root":         {`<mxCell id="1" parent="0"/>`, `root cell "0" is missing`},
		"no layer":        {`<mxCell id="0"/>`, `default layer cell "1" is missing`},
		"layer parent":    {`<mxCell id="0"/><mxCell id="1" parent="x"/>`, `must have parent "0"`},
		"missing parent":  {base + `<mxCell id="a" vertex="1" parent="q"/>`, `parent "q" does not exist`},
		"no parent":       {base + `<mxCell id="a" vertex="1"/>`, "has no parent"},
		"missing target":  {base + `<mxCell id="e" edge="1" parent="1" target="z">` + geo + `</mxCell>`, `target "z" does not exist`},
		"edge no geo":     {base + `<mxCell id="a" vertex="1" parent="1"/><mxCell id="e" edge="1" parent="1" source="a"/>`, "needs a <mxGeometry"},
		"bad style":       {base + `<mxCell id="a" style="fill color=red" vertex="1" parent="1"/>`, "bad style"},
		"parents loop":    {base + `<mxCell id="a" vertex="1" parent="b"/><mxCell id="b" vertex="1" parent="a"/>`, "parents loop"},
		"vertex and edge": {base + `<mxCell id="a" vertex="1" edge="1" parent="1">` + geo + `</mxCell>`, "both a vertex and an edge"},
	} {
		t.Run(name, func(t *testing.T) {
			problems, err := Validate([]byte(wrap(tc.cells)))
			require.NoError(t, err)
			if tc.want == "" {
				require.Empty(t, problems)
				return
			}
			require.NotEmpty(t, problems)
			require.Contains(t, problems[0].String(), tc.want)
			require.Contains(t, problems[0].String(), `page "P"`)
		})
	}
	problems, err := Validate([]byte(sample))
	require.NoError(t, err)
	require.Empty(t, problems)
}

func TestChangesKeepOldProblemsButRefuseNewOnes(t *testing.T) {
	broken := strings.Replace(sample, `parent="lane" custom`, `parent="gone" custom`, 1)
	out, err := SetLabel([]byte(broken), "", "note", "Still editable")
	require.NoError(t, err, "a problem the diagram already had does not block other changes")
	require.Contains(t, string(out), "Still editable")

	// The safety net under every change: an edit that would leave a new problem is refused, and
	// one that would break the XML is too.
	_, err = change([]byte(sample), "", func(d *doc, p *page) ([]edit, error) {
		return []edit{setAttr(d.data, p.byID["note"].mx, "parent", "nowhere")}, nil
	})
	var invalid *InvalidError
	require.True(t, errors.As(err, &invalid))
	require.Contains(t, err.Error(), `cell "note": the parent "nowhere" does not exist`)
	_, err = change([]byte(sample), "", func(d *doc, p *page) ([]edit, error) {
		return []edit{{p.rootEl.start, p.rootEl.start, "<oops>"}}, nil
	})
	require.ErrorContains(t, err, "would break the file")
}

func TestStyleHelpers(t *testing.T) {
	require.Equal(t, "rounded=1;html=1;", ParseStyle(" rounded=1 ; ;html=1").String())
	require.True(t, ParseStyle("swimlane;startSize=30").Has("swimlane"))
	require.Equal(t, "30", ParseStyle("swimlane;startSize=30").Get("startSize"))
	require.NoError(t, CheckStyle(""))
	require.NoError(t, CheckStyle("ellipse;shape=mxgraph.flowchart.document;image=data:a,b==;"))
	require.Error(t, CheckStyle("=x"))
	require.Error(t, CheckStyle("a b=1"))
}

func TestPlainLabel(t *testing.T) {
	for in, want := range map[string]string{
		"Plain":                        "Plain",
		"<div>One</div><div>Two</div>": "One\nTwo",
		"a<br/>b<br>c":                 "a\nb\nc",
		"<span style=\"x\">5 &lt; 6 &amp;&nbsp;</span>": "5 < 6 &",
		"  ": "",
	} {
		require.Equal(t, want, PlainLabel(in), in)
	}
}

func TestNew(t *testing.T) {
	g := Graph{
		Nodes: []GraphNode{
			{ID: "start", Label: "Order placed", Shape: "start", Lane: "cust"},
			{ID: "check", Label: "In stock?", Shape: "decision", Lane: "shop"},
			{ID: "ship", Label: "Ship", Lane: "shop"},
			{ID: "refund", Label: "Refund", Lane: "cust"},
			{ID: "loose", Label: "Audit"},
		},
		Edges: []GraphEdge{{Source: "start", Target: "check"}, {Source: "check", Target: "ship", Label: "yes"}, {Source: "check", Target: "refund", Label: "no"}, {Source: "memo", Target: "check"}},
		Lanes: []GraphLane{{ID: "cust", Label: "Customer"}, {ID: "shop", Label: "Shop"}},
		Notes: []GraphNote{{ID: "memo", Text: "Stock is checked nightly"}},
	}
	out, err := New(g)
	require.NoError(t, err)
	problems, err := Validate(out)
	require.NoError(t, err)
	require.Empty(t, problems)
	pages, err := List(out)
	require.NoError(t, err)
	p := pages[0]
	require.Equal(t, "Page-1", p.Name)
	el := func(id string) Element { return find(p, id) }

	require.Equal(t, "cust", el("start").Parent)
	require.Equal(t, "shop", el("check").Parent)
	require.Equal(t, "1", el("loose").Parent)
	require.Equal(t, KindContainer, el("cust").Kind)
	require.Equal(t, KindNote, el("memo").Kind)
	// Ranks run left to right: start, then check, then ship and refund level with each other.
	abs := func(id string) Geometry {
		g := *el(id).Geometry
		if par := el(id).Parent; par != "1" {
			pg := el(par).Geometry
			g.X += pg.X
			g.Y += pg.Y
		}
		return g
	}
	center := func(id string) float64 { g := abs(id); return g.X + g.Width/2 }
	require.Less(t, center("start"), center("check"))
	require.Less(t, center("check"), center("ship"))
	require.Equal(t, center("ship"), center("refund"))
	require.Equal(t, center("loose"), center("start"), "nodes outside lanes line up with the ranks in them")
	// Lanes are stacked, each holding its nodes.
	cust, shop := el("cust").Geometry, el("shop").Geometry
	require.Equal(t, cust.Y+cust.Height, shop.Y)
	require.Equal(t, cust.Width, shop.Width)
	for _, id := range []string{"start", "refund"} {
		g := el(id).Geometry
		require.LessOrEqual(t, g.X+g.Width, cust.Width)
		require.LessOrEqual(t, g.Y+g.Height, cust.Height)
		require.GreaterOrEqual(t, g.X, float64(laneHeader), "past the lane's title")
	}
	require.Greater(t, abs("memo").Y, shop.Y+shop.Height, "notes go after the diagram")
	// Edges: in the lane when both ends are in it; to a note dashed, with no arrow.
	edges := map[string]Element{}
	for _, e := range p.Elements {
		if e.Kind == KindEdge {
			edges[e.Source+">"+e.Target] = e
		}
	}
	require.Equal(t, "shop", edges["check>ship"].Parent)
	require.Equal(t, "1", edges["start>check"].Parent)
	require.Equal(t, "yes", edges["check>ship"].Label)
	require.Contains(t, edges["memo>check"].Style, "dashed=1")

	// Down: ranks top to bottom, lanes side by side.
	g.Direction = "down"
	out, err = New(g)
	require.NoError(t, err)
	pages, _ = List(out)
	p = pages[0]
	require.Less(t, abs("start").Y, abs("check").Y)
	cust, shop = el("cust").Geometry, el("shop").Geometry
	require.Equal(t, cust.X+cust.Width, shop.X)
	require.NotContains(t, el("cust").Style, "horizontal=0", "side-by-side lanes have their title on top")
}

func TestNewCyclesAndChecks(t *testing.T) {
	out, err := New(Graph{
		Page:  "Loop",
		Nodes: []GraphNode{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}, {ID: "c", Label: "C"}},
		Edges: []GraphEdge{{Source: "a", Target: "b"}, {Source: "b", Target: "c"}, {Source: "c", Target: "a"}, {Source: "b", Target: "b"}},
	})
	require.NoError(t, err)
	pages, _ := List(out)
	require.Equal(t, "Loop", pages[0].Name)
	x := func(id string) float64 { return find(pages[0], id).Geometry.X }
	require.Less(t, x("a"), x("b"), "the cycle is broken where it was entered")
	require.Less(t, x("b"), x("c"))

	// Edge ids skip ids the nodes use.
	out, err = New(Graph{Nodes: []GraphNode{{ID: "e1"}, {ID: "n"}}, Edges: []GraphEdge{{Source: "e1", Target: "n"}}})
	require.NoError(t, err)
	require.Contains(t, string(out), `<mxCell id="e2" value="" style="`+EdgeStyle)

	for name, g := range map[string]Graph{
		"empty":         {},
		"no id":         {Nodes: []GraphNode{{Label: "x"}}},
		"duplicate":     {Nodes: []GraphNode{{ID: "a"}}, Notes: []GraphNote{{ID: "a"}}},
		"reserved":      {Nodes: []GraphNode{{ID: "1"}}},
		"unknown lane":  {Nodes: []GraphNode{{ID: "a", Lane: "x"}}},
		"unknown shape": {Nodes: []GraphNode{{ID: "a", Shape: "blob"}}},
		"unknown end":   {Nodes: []GraphNode{{ID: "a"}}, Edges: []GraphEdge{{Source: "a", Target: "z"}}},
		"lane end":      {Nodes: []GraphNode{{ID: "a", Lane: "l"}}, Lanes: []GraphLane{{ID: "l"}}, Edges: []GraphEdge{{Source: "a", Target: "l"}}},
	} {
		_, err := New(g)
		require.ErrorIs(t, err, ErrInput, name)
	}
}
