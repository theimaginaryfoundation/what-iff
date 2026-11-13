package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	pngpkg "image/png"
	"path"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/drawio"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// diagram (ADR 0x049 §3): the agent reads and changes draw.io diagrams by element rather than by
// patching their XML. The XML work is internal/drawio's; reading, ownership, grants and saving are
// AgentFileTool's, so a diagram is changed exactly as agent_file_write changes a file: the agent's
// own file in place, a user's file it was granted (ADR 0x046) as a new saved version.

const (
	diagramToolName = "diagram"
	// diagramListMax caps the elements one list returns; the outline still names every one.
	diagramListMax = 400
	// diagramOutlineMax caps the outline's characters.
	diagramOutlineMax = 20000
)

// DiagramDescription is the agent-facing description of diagram.
var DiagramDescription = fmt.Sprintf(`Create and change draw.io diagrams (.drawio files) by element, instead of writing their XML. Use it for flowcharts, process maps with swimlanes, architecture sketches and anything else with boxes, connectors, lanes and notes the user may move by hand later. The user opens the file in the diagram editor in their files.

Pick the action, then send only its fields:
| action | needs | optional |
|---|---|---|
| list | file | page |
| validate | file | |
| create | path, nodes and/or notes | edges, lanes, direction, page |
| add_node | file | label, shape, parent, x, y, width, height, style, id, page |
| add_edge | file, source, target | label, style, id, page |
| add_container | file | label, container, parent, x, y, width, height, style, id, page |
| add_note | file, label | parent, x, y, width, height, id, page |
| set_label | file, id, label | page |
| set_style | file, id, style | replace_style, page |
| move | file, id, and x, y and/or width, height | page |
| delete | file, id | page |

- "list": every element (id, kind: vertex, edge, container, note or layer; label; parent; x, y, width, height; an edge's source and target; style) and a text outline. Read it before changing a diagram you did not just make.
- "create": a new .drawio file of yours at path, laid out for you: give nodes, edges, lanes and notes with no positions. Nodes are placed in ranks along the flow (each after the nodes that lead to it), inside their lane; lanes are stacked; notes go below. direction "right" (default) or "down". The user can tidy it with the editor's Arrange menu.
- "add_node", "add_container" (a swimlane, or an invisible "group"), "add_note": a new element; without x and y it goes to the right of what is already in its parent. parent is a container's id: the new element goes inside it, at x, y relative to the container, and the container grows to fit. The result gives the new id.
- "add_edge": a connector from source to target (element ids).
- "set_label": the element's text (plain text; line breaks allowed).
- "set_style": draw.io style, "key=value;key=value". Merged into the current style ("fillColor=#dae8fc;strokeColor=#6c8ebf"; "key=" removes a key) unless replace_style is true.
- "move": a new position (x, y) and/or size (width, height). A container's children move with it.
- "delete": the element, everything inside it, and the connectors left without an end.
- "validate": checks ids are unique, the root cells "0" and "1" are there, parents and connector ends exist, and styles are well formed. Every change is checked the same way and refused if it would break the diagram.

shape is one of %s (default rounded). Coordinates are pixels, x to the right and y down. page is a page's name or number (default the first page).

You can change your own diagrams, and a user's diagram they mentioned you in a comment on; every other diagram is read-only (list, validate). file is a file id, or a path such as "diagrams/flow.drawio".`, strings.Join(drawio.ShapeNames(), ", "))

var diagramNumber = func(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "number", "description": desc}
}

// DiagramToolSpec is the shared function-tool shape for diagram.
var DiagramToolSpec = FunctionToolSpec{
	Name:        diagramToolName,
	Description: DiagramDescription,
	Properties: map[string]interface{}{
		"action": map[string]interface{}{
			"type": "string",
			"enum": []string{"list", "validate", "create", "add_node", "add_edge", "add_container", "add_note", "set_label", "set_style", "move", "delete"},
		},
		"file":          map[string]interface{}{"type": "string", "description": "The diagram: its id, or a path such as 'diagrams/flow.drawio'."},
		"path":          map[string]interface{}{"type": "string", "description": "Where a new diagram goes (create), e.g. 'diagrams/flow.drawio'."},
		"page":          map[string]interface{}{"type": "string", "description": "A page's name or number (default the first); for create, the page's name."},
		"id":            map[string]interface{}{"type": "string", "description": "The element (set_label, set_style, move, delete), or the id a new element takes (optional)."},
		"label":         map[string]interface{}{"type": "string", "description": "The element's text."},
		"shape":         map[string]interface{}{"type": "string", "description": "A node's shape (add_node)."},
		"style":         map[string]interface{}{"type": "string", "description": "A draw.io style string, 'key=value;key=value'."},
		"replace_style": map[string]interface{}{"type": "boolean", "description": "set_style: replace the whole style instead of merging."},
		"parent":        map[string]interface{}{"type": "string", "description": "The container the new element goes in (default the page itself)."},
		"container":     map[string]interface{}{"type": "string", "enum": []string{"swimlane", "group"}, "description": "add_container: a titled swimlane (default) or an invisible group."},
		"source":        map[string]interface{}{"type": "string", "description": "add_edge: the element the connector starts at."},
		"target":        map[string]interface{}{"type": "string", "description": "add_edge: the element the connector ends at."},
		"x":             diagramNumber("Left edge, in pixels (relative to the parent container)."),
		"y":             diagramNumber("Top edge, in pixels (relative to the parent container)."),
		"width":         diagramNumber("Width in pixels."),
		"height":        diagramNumber("Height in pixels."),
		"direction":     map[string]interface{}{"type": "string", "enum": []string{"right", "down"}, "description": "create: the way the flow runs (default right)."},
		"nodes": map[string]interface{}{
			"type":        "array",
			"description": "create: the boxes.",
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":    map[string]interface{}{"type": "string", "description": "A short id, unique in the diagram, e.g. 'review'."},
					"label": map[string]interface{}{"type": "string"},
					"shape": map[string]interface{}{"type": "string", "description": "Default rounded."},
					"lane":  map[string]interface{}{"type": "string", "description": "The id of the lane it is in, if any."},
				},
				"required": []string{"id", "label"},
			},
		},
		"edges": map[string]interface{}{
			"type":        "array",
			"description": "create: the connectors, between node or note ids.",
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]interface{}{"type": "string"},
					"target": map[string]interface{}{"type": "string"},
					"label":  map[string]interface{}{"type": "string"},
				},
				"required": []string{"source", "target"},
			},
		},
		"lanes": map[string]interface{}{
			"type":        "array",
			"description": "create: swimlanes, in order, e.g. one per role or system.",
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":    map[string]interface{}{"type": "string"},
					"label": map[string]interface{}{"type": "string"},
				},
				"required": []string{"id", "label"},
			},
		},
		"notes": map[string]interface{}{
			"type":        "array",
			"description": "create: sticky notes; connect one to a node with an edge.",
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":   map[string]interface{}{"type": "string"},
					"text": map[string]interface{}{"type": "string"},
				},
				"required": []string{"id", "text"},
			},
		},
	},
	Required: []string{"action"},
}

type diagramArgs struct {
	Action       string             `json:"action"`
	File         string             `json:"file"`
	Path         string             `json:"path"`
	Mermaid      string             `json:"mermaid"`
	Page         string             `json:"page"`
	ID           string             `json:"id"`
	Label        *string            `json:"label"`
	Shape        string             `json:"shape"`
	Style        string             `json:"style"`
	ReplaceStyle bool               `json:"replace_style"`
	Parent       string             `json:"parent"`
	Container    string             `json:"container"`
	Source       string             `json:"source"`
	Target       string             `json:"target"`
	X            *float64           `json:"x"`
	Y            *float64           `json:"y"`
	Width        *float64           `json:"width"`
	Height       *float64           `json:"height"`
	Direction    string             `json:"direction"`
	Nodes        []drawio.GraphNode `json:"nodes"`
	Edges        []drawio.GraphEdge `json:"edges"`
	Lanes        []drawio.GraphLane `json:"lanes"`
	Notes        []drawio.GraphNote `json:"notes"`
}

func (a diagramArgs) label() string {
	if a.Label == nil {
		return ""
	}
	return *a.Label
}

func (a diagramArgs) placement() drawio.Placement {
	return drawio.Placement{X: a.X, Y: a.Y, Width: a.Width, Height: a.Height}
}

type diagramResult struct {
	Success bool          `json:"success"`
	Action  string        `json:"action,omitempty"`
	File    *agentFileRef `json:"file,omitempty"`
	// ID is the element a change made or changed.
	ID       string           `json:"id,omitempty"`
	Removed  []string         `json:"removed,omitempty"`
	Pages    []drawio.Page    `json:"pages,omitempty"`
	Outline  string           `json:"outline,omitempty"`
	Valid    *bool            `json:"valid,omitempty"`
	Problems []drawio.Problem `json:"problems,omitempty"`
	// Preview says whether a picture of the diagram is shown with the result (list).
	Preview string `json:"preview,omitempty"`
	Note    string `json:"note,omitempty"`
	Error   string `json:"error,omitempty"`
}

// DiagramTool implements diagram over the agent file tools.
type DiagramTool struct {
	files *AgentFileTool
}

// NewDiagramTool is the diagram tool over files, whose reads, ownership checks and saves it uses.
func NewDiagramTool(files *AgentFileTool) *DiagramTool {
	return &DiagramTool{files: files}
}

// Run handles one diagram call.
func (t *DiagramTool) Run(ctx context.Context, chat *models.Chat, input []byte) (string, error) {
	fail := func(msg string) (string, error) {
		return marshalToolResult(diagramResult{Error: msg}, diagramToolName)
	}
	// Diagrams are files in the owner's account-wide file area, outside any sandbox.
	if chat.IsSandboxed() {
		return fail(sandboxedNote("Diagrams"))
	}
	var a diagramArgs
	if err := json.Unmarshal(input, &a); err != nil {
		return fail(fmt.Sprintf("invalid arguments: %v", err))
	}
	if msg := checkDiagramArgs(a); msg != "" {
		return fail(msg)
	}
	res, err := t.run(ctx, chat, a)
	if msg, ok := AsFileRefusal(err); ok {
		return fail(msg)
	}
	if err != nil {
		t.files.logger.Error("diagram failed", zap.String("user_id", chat.UserID.String()), zap.String("action", a.Action), zap.Error(err))
		return fail("could not change the diagram; try again")
	}
	res.Success, res.Action = true, a.Action
	return marshalToolResult(res, diagramToolName)
}

// checkDiagramArgs refuses a call that leaves out what its action needs.
func checkDiagramArgs(a diagramArgs) string {
	if msg, handled := workshopArgsProblem(a); handled {
		return msg
	}
	has := func(s string) bool { return strings.TrimSpace(s) != "" }
	need := func(fields ...string) string {
		return fmt.Sprintf("action %q needs %s", a.Action, strings.Join(fields, " and "))
	}
	switch a.Action {
	case "create":
		if !has(a.Path) || (len(a.Nodes) == 0 && len(a.Notes) == 0) {
			return need("path", "nodes (or notes)")
		}
		if has(a.File) {
			return `action "create" makes a new file at path; leave out file`
		}
		return ""
	case "list", "validate", "add_node", "add_container":
		if !has(a.File) {
			return need("file")
		}
	case "add_edge":
		if !has(a.File) || !has(a.Source) || !has(a.Target) {
			return need("file", "source", "target")
		}
	case "add_note":
		if !has(a.File) || !has(a.label()) {
			return need("file", "label (the note's text)")
		}
	case "set_label":
		if !has(a.File) || !has(a.ID) || a.Label == nil {
			return need("file", "id", "label")
		}
	case "set_style":
		if !has(a.File) || !has(a.ID) || !has(a.Style) {
			return need("file", "id", "style")
		}
	case "move":
		if !has(a.File) || !has(a.ID) || (a.X == nil && a.Y == nil && a.Width == nil && a.Height == nil) {
			return need("file", "id", "x, y and/or width, height")
		}
	case "delete":
		if !has(a.File) || !has(a.ID) {
			return need("file", "id")
		}
	default:
		return `action must be one of "list", "validate", "create", "add_node", "add_edge", "add_container", "add_note", "set_label", "set_style", "move", "delete"`
	}
	if len(a.Nodes)+len(a.Edges)+len(a.Lanes)+len(a.Notes) > 0 {
		return `nodes, edges, lanes and notes are for "create"; add elements one at a time with add_node, add_edge, add_container and add_note`
	}
	return ""
}

func (t *DiagramTool) run(ctx context.Context, chat *models.Chat, a diagramArgs) (diagramResult, error) {
	switch a.Action {
	case "list", "validate":
		return t.read(ctx, chat, a)
	case "create":
		return t.create(ctx, chat, a)
	case "layout", "render":
		return t.workshopAction(ctx, chat, a)
	}
	var res diagramResult
	out, err := t.files.rewrite(ctx, chat, a.File, func(cur string) (string, error) {
		next, err := t.apply(&res, []byte(cur), a)
		if err != nil {
			return "", diagramRefusal(err)
		}
		return string(next), nil
	})
	if err != nil {
		return diagramResult{}, err
	}
	res.File = out.File
	return res, nil
}

// apply makes one change to the diagram's text, noting in res what it made or removed.
func (t *DiagramTool) apply(res *diagramResult, data []byte, a diagramArgs) ([]byte, error) {
	var (
		out []byte
		err error
	)
	switch a.Action {
	case "add_node", "add_container", "add_note":
		kind := map[string]string{"add_node": drawio.KindVertex, "add_container": drawio.KindContainer, "add_note": drawio.KindNote}[a.Action]
		out, res.ID, err = drawio.AddCell(data, a.Page, drawio.NewCell{
			Kind: kind, ID: a.ID, Label: a.label(), Shape: a.Shape, Container: a.Container, Style: a.Style, Parent: a.Parent, Placement: a.placement(),
		})
	case "add_edge":
		out, res.ID, err = drawio.AddEdge(data, a.Page, drawio.NewEdge{ID: a.ID, Source: a.Source, Target: a.Target, Label: a.label(), Style: a.Style})
	case "set_label":
		res.ID = a.ID
		out, err = drawio.SetLabel(data, a.Page, a.ID, a.label())
	case "set_style":
		res.ID = a.ID
		out, err = drawio.SetStyle(data, a.Page, a.ID, a.Style, a.ReplaceStyle)
	case "move":
		res.ID = a.ID
		out, err = drawio.Move(data, a.Page, a.ID, a.placement())
	case "delete":
		res.ID = a.ID
		out, res.Removed, err = drawio.Delete(data, a.Page, a.ID)
	}
	return out, err
}

// diagramRefusal turns what internal/drawio refused into a message for the model: it does no
// I/O, so every error it returns is about the diagram or the request.
func diagramRefusal(err error) error {
	return fileRefusalf("%v", err)
}

func (t *DiagramTool) read(ctx context.Context, chat *models.Chat, a diagramArgs) (diagramResult, error) {
	att, err := t.files.resolve(ctx, chat, a.File)
	if err != nil {
		return diagramResult{}, err
	}
	if !isDiagramFile(att) {
		return diagramResult{}, fileRefusalf("%q is not a draw.io diagram (.drawio)", FilePath(att))
	}
	text, err := t.files.readText(ctx, att)
	if err != nil {
		return diagramResult{}, err
	}
	ref := t.files.refEditable(ctx, chat, att, "")
	res := diagramResult{File: &ref}
	if a.Action == "validate" {
		problems, err := drawio.Validate([]byte(text))
		if err != nil {
			return diagramResult{}, diagramRefusal(err)
		}
		valid := len(problems) == 0
		res.Valid, res.Problems = &valid, problems
		return res, nil
	}
	pages, err := drawio.List([]byte(text))
	if err != nil {
		return diagramResult{}, diagramRefusal(err)
	}
	if strings.TrimSpace(a.Page) != "" {
		if pages, err = pickPage(pages, a.Page); err != nil {
			return diagramResult{}, err
		}
	}
	res.Outline = drawio.Outline(pages)
	if len(res.Outline) > diagramOutlineMax {
		res.Outline = res.Outline[:diagramOutlineMax] + "\n…"
	}
	res.Pages, res.Note = capElements(pages, diagramListMax)
	res.Preview = t.showPreview(ctx, att)
	return res, nil
}

const (
	// diagramPictureMaxBytes and diagramPictureMaxSide bound a diagram's picture shown to the model
	// at full size; a larger one is shown as its JPEG thumbnail.
	diagramPictureMaxBytes = 3 << 20
	diagramPictureMaxSide  = 4096
)

// fitsFullSize reports whether a diagram's PNG may be shown to the model as it is.
func fitsFullSize(png []byte) bool {
	if len(png) > diagramPictureMaxBytes {
		return false
	}
	cfg, err := pngpkg.DecodeConfig(bytes.NewReader(png))
	return err == nil && cfg.Width <= diagramPictureMaxSide && cfg.Height <= diagramPictureMaxSide
}

// addDiagramPicture shows the model a picture of a diagram with the result: the PNG when it fits
// full size, else its thumbnail (none when there is no thumbnail). It reports whether it showed one.
func addDiagramPicture(ctx context.Context, png, thumb []byte) bool {
	switch {
	case len(png) > 0 && fitsFullSize(png):
		AddResultImage(ctx, png, "image/png")
	case len(thumb) > 0:
		AddResultImage(ctx, thumb, "image/jpeg")
	default:
		return false
	}
	return true
}

// showPreview shows the model the diagram's preview (ADR 0x049 §8), the picture the user's editor
// last drew of this version, with the result, and says what it showed.
func (t *DiagramTool) showPreview(ctx context.Context, att *models.FileAttachment) string {
	if t.files.files == nil || att.S3Key == "" {
		return ""
	}
	data, err := t.files.files.DownloadFile(ctx, storage.FileKeyForPreview(att.S3Key))
	var thumb []byte
	if err == nil && len(data) > 0 && !fitsFullSize(data) {
		thumb, err = t.files.files.DownloadFile(ctx, storage.FileKeyForPreviewThumbnail(att.S3Key))
	}
	if err != nil || !addDiagramPicture(ctx, data, thumb) {
		return "No picture of this version yet: one is drawn when the user next opens the diagram."
	}
	return "A picture of this version, as the user's editor drew it, follows this result (if you can see images)."
}

// pickPage is the one page ref names (drawio.FindPage), keeping its number.
func pickPage(pages []drawio.Page, ref string) ([]drawio.Page, error) {
	i, err := drawio.FindPage(pages, ref)
	if err != nil {
		return nil, diagramRefusal(err)
	}
	return pages[i : i+1], nil
}

// capElements keeps the first max elements across the pages, saying how many it left out.
func capElements(pages []drawio.Page, max int) ([]drawio.Page, string) {
	left, cut := max, 0
	out := make([]drawio.Page, len(pages))
	for i, p := range pages {
		out[i] = p
		if len(p.Elements) > left {
			cut += len(p.Elements) - left
			out[i].Elements = p.Elements[:left]
		}
		left -= len(out[i].Elements)
	}
	if cut == 0 {
		return out, ""
	}
	return out, fmt.Sprintf("%d elements are left out of pages (the outline names them all); pass page to list one page", cut)
}

// isDiagramFile reports whether att is a draw.io file, by the rule every other part uses.
func isDiagramFile(att *models.FileAttachment) bool {
	return drawio.IsDiagramFile(att.Name, att.FileType)
}

func (t *DiagramTool) create(ctx context.Context, chat *models.Chat, a diagramArgs) (diagramResult, error) {
	p := strings.TrimSpace(a.Path)
	switch ext := strings.ToLower(path.Ext(p)); ext {
	case "":
		p += ".drawio"
	case ".drawio":
	default:
		return diagramResult{}, fileRefusalf("a diagram's path ends in .drawio, not %s", ext)
	}
	if strings.TrimSpace(a.Mermaid) != "" {
		return t.createFromMermaid(ctx, chat, p, a.Mermaid)
	}
	content, err := drawio.New(drawio.Graph{Page: a.Page, Nodes: a.Nodes, Edges: a.Edges, Lanes: a.Lanes, Notes: a.Notes, Direction: a.Direction})
	if err != nil {
		return diagramResult{}, diagramRefusal(err)
	}
	out, err := t.files.create(ctx, chat, p, string(content))
	if err != nil {
		return diagramResult{}, err
	}
	pages, err := drawio.List(content)
	if err != nil {
		return diagramResult{}, err
	}
	return diagramResult{File: out.File, Outline: drawio.Outline(pages)}, nil
}
