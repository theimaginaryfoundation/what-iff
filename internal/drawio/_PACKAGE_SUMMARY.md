# Package: `internal/drawio`

## Role

Reads and changes draw.io (`.drawio`) diagrams element by element (ADR 0x049 §3), for the agent's `diagram` tool.
Everything is a pure function over the file's text, with no I/O.

## Responsibilities

- **Reading** (`drawio.go`): `List` gives each page's elements (id, kind, plain-text label, parent, geometry, connector ends, style) and `Outline` a short text picture of them.
  Each `Page` carries its `Number` (from 1), so a page picked out of the others keeps it in the outline.
  `FindPage` is the one way a page reference is read, here and by its callers: an id, else the first page of that name, else a number from 1 (`ErrNoPage` lists the pages).
  A file is an `<mxfile>` of `<diagram>` pages or a bare `<mxGraphModel>`; a cell is an `<mxCell>` or an `<object>`/`<UserObject>` wrapping one (the wrapper holds the id and the label).
  A compressed page is refused with `ErrCompressed` (it is not inflated); anything else unreadable is `ErrNotDiagram`.
- **Changing** (`edit.go`): `AddCell` (node, container, note), `AddEdge`, `SetLabel`, `SetStyle` (merge or replace), `Move` and `Delete` (with a container's contents and the connectors left without an end).
  A new cell with no position goes to the right of what is already in its parent, a container grows to fit a new child, and a connector is filed in the innermost container holding both ends.
- **Checking** (`validate.go`): `Validate` lists unique-id, root-cell (`0`, `1`), parent, connector-end, edge-geometry, parent-loop and style problems.
  Every change is refused when it brings a problem the diagram did not have (problems told apart by what they say and where); `CheckNoNewProblems` applies the same check to a copy changed elsewhere, such as a layout.
- **Laying out** (`layout.go`): `New` turns a `Graph` (nodes, edges, lanes, notes, no positions) into a new file: ranks along the flow by longest path (cycles broken where the depth-first search entered them), nodes inside their lane, lanes stacked, notes after the diagram, `right` or `down`.
- **Styles and shapes** (`style.go`, `cells.go`): `ParseStyle`, `Style.Merge`, `CheckStyle`, the named node shapes (`ShapeNames`) and the default node, edge, note and lane styles, following jgraph's XML reference for generated diagrams.

## Key types and entry points

- `List`, `Outline`, `FindPage`, `Validate`, `CheckNoNewProblems`, `New`, `AddCell`, `AddEdge`, `SetLabel`, `SetStyle`, `Move`, `Delete`.
- `Page`, `Element`, `Geometry`, `Placement`, `NewCell`, `NewEdge`, `Graph`, `Problem`.
- `IsDiagramFile`, `MediaType`: which files are diagrams, the one rule used by the `diagram` tool, `internal/filecomments`, `internal/storage` (preview cleanup) and the preview route.
- Errors: `ErrNotDiagram`, `ErrCompressed`, `ErrNoPage`, `ErrInput` (the request does not fit the diagram), `*InvalidError` (the change would leave new problems).

## Dependencies

- **Inbound:** `internal/agent/tools` (`DiagramTool`), `internal/filecomments`, `internal/storage` and `internal/handlers/fileattachment` (`IsDiagramFile`).
- **Outbound:** standard library, and `internal/officecomments` for `XMLText` (dropping characters XML cannot hold).

## Non-obvious decisions

- **Edits, not rewrites.**
  The XML is edited as text at offsets an `encoding/xml` Decoder finds (`xml.go`), as `internal/officecomments` does; nothing is marshalled back.
  Attribute values are found by scanning the start tag (`scanAttrs`), so prefixes, unknown attributes, quoting and everything untouched pass through byte for byte.
- **Every change is checked.**
  `change` re-parses and validates the result and refuses a change that brings a problem the diagram did not already have; a diagram that already had problems can still be edited.
- **Labels are plain text to callers.**
  `List` strips HTML from labels; `SetLabel` and new cells store text as escaped HTML (`<br>` for line breaks) when the style has `html=1`, and as-is otherwise (line breaks as `&#10;`).
- **New ids** are short (`n1`, `e1`, `lane1`, `note1`) and unused anywhere in the file, not just on the page.

## Testing

- `drawio_test.go` — table tests over a sample file with a prefixed attribute, a single-quoted unknown attribute, an `<object>` wrapper, a second page and a layer.
  Each change is checked to alter only its own bytes; refusals (compressed, not a diagram, too deep, unknown ids, bad styles, used ids), every validation rule, layout ranks, lanes, notes and direction, cycles, and the safety net that refuses a change leaving a new problem.

## Related

- [ADR 0x049](../../docs/adr/0x049-drawio-diagrams.md) — draw.io diagrams; §3 for agents.
- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md).
