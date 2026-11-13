package drawio

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Graph is a diagram with no positions, for New to lay out.
type Graph struct {
	// Page names the page; empty is "Page-1".
	Page  string
	Nodes []GraphNode
	Edges []GraphEdge
	Lanes []GraphLane
	Notes []GraphNote
	// Direction is "right" (default: ranks left to right, lanes stacked top to bottom) or "down"
	// (ranks top to bottom, lanes side by side).
	Direction string
}

// GraphNode is a node: its id, label, shape (ShapeNames) and lane (a GraphLane's id, or none).
type GraphNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Shape string `json:"shape,omitempty"`
	Lane  string `json:"lane,omitempty"`
}

// GraphEdge connects two nodes or notes.
type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
}

// GraphLane is a swimlane holding the nodes that name it.
type GraphLane struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// GraphNote is a sticky note.
type GraphNote struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Spacing of the layout.
const (
	margin   = 40.0
	rankGap  = 80.0 // between ranks, along the flow
	slotGap  = 40.0 // between nodes of one rank in one band
	bandPad  = 30.0 // inside a lane, around its nodes
	bandGap  = 0.0  // lanes touch, as draw.io's pools do
	noteGap  = 60.0
	maxCells = 2000
)

// New lays out g as a new diagram file: nodes in ranks along the flow (each after the nodes that
// lead to it), in their lanes, lanes stacked across the flow, notes after the diagram.
func New(g Graph) ([]byte, error) {
	g = g.trimmed()
	if err := g.check(); err != nil {
		return nil, err
	}
	down := strings.EqualFold(strings.TrimSpace(g.Direction), "down")

	rank := g.ranks()
	nodes := map[string]*placed{}
	for i, n := range g.Nodes {
		s, _ := shapeFor(n.Shape)
		w, h := labelSize(n.Label, s.width, s.height)
		if s == shapes["actor"] {
			// A stick figure keeps its size; its label is under it.
			w, h = s.width, s.height
		}
		nodes[n.ID] = &placed{node: n, style: s.style, order: i, rank: rank[n.ID], w: w, h: h}
	}

	// Bands across the flow: nodes with no lane first, then each lane.
	bands := []string{""}
	for _, l := range g.Lanes {
		bands = append(bands, l.ID)
	}
	maxRank := 0
	for _, p := range nodes {
		maxRank = max(maxRank, p.rank)
	}
	// along(p) is a node's extent along the flow, across(p) across it.
	along := func(p *placed) float64 {
		if down {
			return p.h
		}
		return p.w
	}
	across := func(p *placed) float64 {
		if down {
			return p.w
		}
		return p.h
	}
	rankSize := make([]float64, maxRank+1)
	cellsIn := map[string][][]*placed{}
	for _, b := range bands {
		cellsIn[b] = make([][]*placed, maxRank+1)
	}
	for _, n := range g.Nodes {
		p := nodes[n.ID]
		rankSize[p.rank] = max(rankSize[p.rank], along(p))
		cellsIn[n.Lane][p.rank] = append(cellsIn[n.Lane][p.rank], p)
	}
	orderByPredecessors(g, nodes, cellsIn, bands, maxRank)

	// laneStart is the lane's title band; alignAlong puts nodes outside lanes level with those in
	// them.
	laneStart, alignAlong := 0.0, 0.0
	if len(g.Lanes) > 0 {
		laneStart, alignAlong = laneHeader, laneHeader+bandPad
	}
	rankAt := make([]float64, maxRank+1)
	at := 0.0
	for r := range rankSize {
		rankAt[r] = at
		at += rankSize[r] + rankGap
	}
	flowLen := at - rankGap

	var (
		cells   []cellSpec
		acrossY = margin // where the next band starts across the flow
	)
	for bi, b := range bands {
		// The band's size across the flow: its fullest rank.
		size := 0.0
		for _, col := range cellsIn[b] {
			s := 0.0
			for i, p := range col {
				if i > 0 {
					s += slotGap
				}
				s += across(p)
			}
			size = max(size, s)
		}
		if b == "" && size == 0 {
			continue
		}
		parent, off0, offAlong := "1", acrossY, margin+alignAlong
		if b != "" {
			size = max(size, 60) + 2*bandPad
			lane := g.Lanes[bi-1]
			lw, lh := flowLen+laneStart+2*bandPad, size
			lg := Geometry{X: margin, Y: acrossY, Width: lw, Height: lh}
			if down {
				lg = Geometry{X: acrossY, Y: margin, Width: lh, Height: lw}
			}
			cells = append(cells, cellSpec{id: lane.ID, value: labelValue(lane.Label, "html=1"), style: laneStyle(bi-1, !down), parent: "1", vertex: true, geom: &lg})
			parent, off0, offAlong = lane.ID, bandPad, alignAlong
		}
		for r, col := range cellsIn[b] {
			s := 0.0
			for i, p := range col {
				if i > 0 {
					s += slotGap
				}
				s += across(p)
			}
			pos := off0 + math.Round((size-2*bandPad*boolf(b != "")-s)/2)
			for _, p := range col {
				a := offAlong + rankAt[r] + math.Round((rankSize[r]-along(p))/2)
				geo := Geometry{X: a, Y: pos, Width: p.w, Height: p.h}
				if down {
					geo = Geometry{X: pos, Y: a, Width: p.w, Height: p.h}
				}
				p.parent, p.geo = parent, geo
				pos += across(p) + slotGap
			}
		}
		acrossY += size + bandGap
		if b == "" {
			acrossY += slotGap
		}
	}
	for _, n := range g.Nodes {
		p := nodes[n.ID]
		geo := p.geo
		cells = append(cells, cellSpec{id: n.ID, value: labelValue(n.Label, p.style), style: p.style, parent: p.parent, vertex: true, geom: &geo})
	}

	// Notes in a row after the diagram, across from where the flow starts.
	noteAt := margin
	for _, n := range g.Notes {
		w, h := labelSize(n.Text, 180, 80)
		geo := Geometry{X: noteAt, Y: acrossY + noteGap, Width: w, Height: h}
		if down {
			geo = Geometry{X: acrossY + noteGap, Y: noteAt, Width: w, Height: h}
			noteAt += h + slotGap
		} else {
			noteAt += w + slotGap
		}
		cells = append(cells, cellSpec{id: n.ID, value: labelValue(n.Text, NoteStyle), style: NoteStyle, parent: "1", vertex: true, geom: &geo})
	}

	lane := map[string]string{}
	notes := map[string]bool{}
	for _, n := range g.Nodes {
		lane[n.ID] = n.Lane
	}
	for _, n := range g.Notes {
		notes[n.ID] = true
	}
	ids := g.edgeIDs()
	for i, e := range g.Edges {
		style := EdgeStyle
		if notes[e.Source] || notes[e.Target] {
			style += noteEdgeExtras
		}
		parent := "1"
		if l := lane[e.Source]; l != "" && l == lane[e.Target] && !notes[e.Source] && !notes[e.Target] {
			parent = l
		}
		cells = append(cells, cellSpec{id: ids[i], value: labelValue(e.Label, style), style: style, parent: parent, edge: true, source: e.Source, target: e.Target})
	}

	page := strings.TrimSpace(g.Page)
	if page == "" {
		page = "Page-1"
	}
	var b strings.Builder
	b.WriteString(`<mxfile host="WhatIff">` + "\n")
	b.WriteString(`  <diagram id="page-1" name="` + escapeAttr(page) + `">` + "\n")
	b.WriteString(`    <mxGraphModel grid="1" gridSize="10" guides="1" tooltips="1" connect="1" arrows="1" fold="1" page="1" pageScale="1" pageWidth="850" pageHeight="1100" math="0" shadow="0">` + "\n")
	b.WriteString("      <root>\n")
	b.WriteString(`        <mxCell id="0" />` + "\n")
	b.WriteString(`        <mxCell id="1" parent="0" />` + "\n")
	for _, c := range cells {
		b.WriteString("        " + strings.ReplaceAll(c.xml(), "\n", "\n        ") + "\n")
	}
	b.WriteString("      </root>\n    </mxGraphModel>\n  </diagram>\n</mxfile>\n")
	out := []byte(b.String())
	if problems, err := Validate(out); err != nil {
		return nil, err
	} else if err := problemsError(problems); err != nil {
		return nil, err
	}
	return out, nil
}

func boolf(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// placed is a node with its rank and where it was put.
type placed struct {
	node   GraphNode
	style  string
	order  int
	rank   int
	w, h   float64
	parent string
	geo    Geometry
}

// edgeIDs are the edges' ids: "e1", "e2", ..., skipping ids nodes, notes and lanes have.
func (g Graph) edgeIDs() []string {
	used := map[string]bool{}
	for _, n := range g.Nodes {
		used[n.ID] = true
	}
	for _, n := range g.Notes {
		used[n.ID] = true
	}
	for _, l := range g.Lanes {
		used[l.ID] = true
	}
	out := make([]string, len(g.Edges))
	next := 1
	for i := range g.Edges {
		id := fmt.Sprintf("e%d", next)
		for used[id] {
			next++
			id = fmt.Sprintf("e%d", next)
		}
		used[id] = true
		out[i] = id
		next++
	}
	return out
}

// trimmed is g with surrounding spaces taken off every id and every reference to one, so an id
// the model wrote as " a" is the same cell as "a" everywhere it is used.
func (g Graph) trimmed() Graph {
	t := strings.TrimSpace
	g.Nodes = append([]GraphNode(nil), g.Nodes...)
	for i := range g.Nodes {
		g.Nodes[i].ID, g.Nodes[i].Lane = t(g.Nodes[i].ID), t(g.Nodes[i].Lane)
	}
	g.Edges = append([]GraphEdge(nil), g.Edges...)
	for i := range g.Edges {
		g.Edges[i].Source, g.Edges[i].Target = t(g.Edges[i].Source), t(g.Edges[i].Target)
	}
	g.Lanes = append([]GraphLane(nil), g.Lanes...)
	for i := range g.Lanes {
		g.Lanes[i].ID = t(g.Lanes[i].ID)
	}
	g.Notes = append([]GraphNote(nil), g.Notes...)
	for i := range g.Notes {
		g.Notes[i].ID = t(g.Notes[i].ID)
	}
	return g
}

// check refuses a graph that cannot be drawn: missing or repeated ids, unknown shapes, lanes or
// edge ends.
func (g Graph) check() error {
	if len(g.Nodes) == 0 && len(g.Notes) == 0 {
		return inputErr("give at least one node or note")
	}
	if n := len(g.Nodes) + len(g.Edges) + len(g.Lanes) + len(g.Notes); n > maxCells {
		return inputErr("%d elements is too many for one call; at most %d", n, maxCells)
	}
	ids := map[string]string{"0": "reserved", "1": "reserved"}
	claim := func(id, what string) error {
		if id == "" {
			return inputErr("every %s needs an id", what)
		}
		if prev, ok := ids[id]; ok {
			if prev == "reserved" {
				return inputErr("the id %q is reserved; pick another", id)
			}
			return inputErr("the id %q is used by more than one %s/%s", id, prev, what)
		}
		ids[id] = what
		return nil
	}
	for _, l := range g.Lanes {
		if err := claim(l.ID, "lane"); err != nil {
			return err
		}
	}
	for _, n := range g.Nodes {
		if err := claim(n.ID, "node"); err != nil {
			return err
		}
		if _, err := shapeFor(n.Shape); err != nil {
			return inputErr("node %q: %v", n.ID, err)
		}
		if n.Lane != "" && ids[n.Lane] != "lane" {
			return inputErr("node %q is in lane %q, which is not in lanes", n.ID, n.Lane)
		}
	}
	for _, n := range g.Notes {
		if err := claim(n.ID, "note"); err != nil {
			return err
		}
	}
	for _, e := range g.Edges {
		for _, end := range []string{e.Source, e.Target} {
			if k := ids[end]; k != "node" && k != "note" {
				return inputErr("edge %s -> %s: %q is not a node or note", e.Source, e.Target, end)
			}
		}
	}
	return nil
}

// ranks is each node's rank along the flow: one more than the highest rank of the nodes with an
// edge into it, ignoring the edges that close a cycle (found by a depth-first search in the order
// the nodes were given).
func (g Graph) ranks() map[string]int {
	isNode := map[string]bool{}
	for _, n := range g.Nodes {
		isNode[n.ID] = true
	}
	out := map[string][]string{}
	for _, e := range g.Edges {
		if isNode[e.Source] && isNode[e.Target] && e.Source != e.Target {
			out[e.Source] = append(out[e.Source], e.Target)
		}
	}
	// Depth-first order; an edge to a node still on the stack closes a cycle and is dropped.
	const (
		unseen = iota
		onStack
		done
	)
	state := map[string]int{}
	var post []string
	kept := map[string][]string{}
	var visit func(id string)
	visit = func(id string) {
		state[id] = onStack
		for _, t := range out[id] {
			switch state[t] {
			case onStack:
				continue
			case unseen:
				visit(t)
			}
			kept[id] = append(kept[id], t)
		}
		state[id] = done
		post = append(post, id)
	}
	// Start from the nodes nothing leads to, so a cycle breaks at its entry, then the rest.
	hasIn := map[string]bool{}
	for _, ts := range out {
		for _, t := range ts {
			hasIn[t] = true
		}
	}
	for _, n := range g.Nodes {
		if !hasIn[n.ID] && state[n.ID] == unseen {
			visit(n.ID)
		}
	}
	for _, n := range g.Nodes {
		if state[n.ID] == unseen {
			visit(n.ID)
		}
	}
	rank := map[string]int{}
	// Reverse post-order is a topological order of the kept edges.
	for i := len(post) - 1; i >= 0; i-- {
		id := post[i]
		for _, t := range kept[id] {
			rank[t] = max(rank[t], rank[id]+1)
		}
	}
	return rank
}

// orderByPredecessors sorts each rank's nodes in a band by where the nodes leading to them sit
// (their average slot), so edges cross less; ties keep the order given.
func orderByPredecessors(g Graph, nodes map[string]*placed, cellsIn map[string][][]*placed, bands []string, maxRank int) {
	preds := map[string][]string{}
	for _, e := range g.Edges {
		if nodes[e.Source] != nil && nodes[e.Target] != nil {
			preds[e.Target] = append(preds[e.Target], e.Source)
		}
	}
	slot := map[string]float64{}
	for r := 0; r <= maxRank; r++ {
		for _, b := range bands {
			col := cellsIn[b][r]
			key := map[string]float64{}
			for _, p := range col {
				sum, n := 0.0, 0
				for _, s := range preds[p.node.ID] {
					if v, ok := slot[s]; ok && nodes[s].rank < r {
						sum += v
						n++
					}
				}
				key[p.node.ID] = float64(p.order) + 1e6 // no placed predecessor: after, in given order
				if n > 0 {
					key[p.node.ID] = sum / float64(n)
				}
			}
			sort.SliceStable(col, func(i, j int) bool { return key[col[i].node.ID] < key[col[j].node.ID] })
			for i, p := range col {
				slot[p.node.ID] = float64(i)
			}
		}
	}
}
