package drawio

import (
	"fmt"
	"strings"
)

// Problem is one thing wrong with a diagram.
type Problem struct {
	Page    string `json:"page,omitempty"`
	ID      string `json:"id,omitempty"`
	Message string `json:"message"`
}

func (p Problem) String() string {
	s := p.Message
	if p.ID != "" {
		s = fmt.Sprintf("cell %q: %s", p.ID, s)
	}
	if p.Page != "" {
		s = fmt.Sprintf("page %q: %s", p.Page, s)
	}
	return s
}

// Validate checks every page of a diagram: unique ids, the reserved root cells "0" and "1", every
// parent there, connector ends there, edges with a geometry, and well-formed styles. A file it
// cannot read at all is an error; a file it can read but that is wrong is a list of problems.
func Validate(data []byte) ([]Problem, error) {
	d, err := parse(data)
	if err != nil {
		return nil, err
	}
	return d.validate(), nil
}

func (d *doc) validate() []Problem {
	var out []Problem
	for i, p := range d.pages {
		name := p.name
		if name == "" && len(d.pages) > 1 {
			name = fmt.Sprintf("%d", i+1)
		}
		bad := func(id, format string, args ...any) {
			out = append(out, Problem{Page: name, ID: id, Message: fmt.Sprintf(format, args...)})
		}
		seen := map[string]bool{}
		for _, c := range p.cells {
			switch {
			case c.id == "":
				bad("", "a cell has no id")
			case seen[c.id]:
				bad(c.id, "the id is used more than once")
			}
			seen[c.id] = true
		}
		if r := p.byID["0"]; r == nil {
			bad("", `the root cell "0" is missing`)
		} else if r.parent != "" {
			bad("0", "the root cell must have no parent")
		}
		if l := p.byID["1"]; l == nil {
			bad("", `the default layer cell "1" is missing`)
		} else if l.parent != "0" {
			bad("1", `the default layer must have parent "0"`)
		}
		for _, c := range p.cells {
			if c.id == "0" || c.id == "" {
				continue
			}
			switch {
			case c.parent == "":
				bad(c.id, "the cell has no parent")
			case c.parent == c.id:
				bad(c.id, "the cell is its own parent")
			case p.byID[c.parent] == nil:
				bad(c.id, "the parent %q does not exist", c.parent)
			}
			if c.vertex && c.edge {
				bad(c.id, "a cell cannot be both a vertex and an edge")
			}
			if c.edge {
				for _, end := range []struct{ name, id string }{{"source", c.source}, {"target", c.target}} {
					if end.id != "" && p.byID[end.id] == nil {
						bad(c.id, "the %s %q does not exist", end.name, end.id)
					}
				}
				if c.geom == nil {
					bad(c.id, `an edge needs a <mxGeometry relative="1" as="geometry"/> child`)
				}
			}
			if err := CheckStyle(c.style); err != nil {
				bad(c.id, "bad style: %v", err)
			}
		}
		// A parent chain that loops would hang the editor.
		for _, c := range p.cells {
			steps, at := 0, c
			for at != nil && at.parent != "" && steps <= len(p.cells) {
				at = p.byID[at.parent]
				steps++
			}
			if steps > len(p.cells) {
				bad(c.id, "the cell's parents loop")
			}
		}
	}
	return out
}

// problemsError joins problems into one error.
func problemsError(problems []Problem) error {
	if len(problems) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(problems))
	for i, p := range problems {
		if i == 5 {
			msgs = append(msgs, fmt.Sprintf("and %d more", len(problems)-5))
			break
		}
		msgs = append(msgs, p.String())
	}
	return &InvalidError{Problems: problems, msg: strings.Join(msgs, "; ")}
}

// InvalidError is a change that would leave the diagram invalid; nothing is changed.
type InvalidError struct {
	Problems []Problem
	msg      string
}

func (e *InvalidError) Error() string { return "the change would leave the diagram invalid: " + e.msg }
