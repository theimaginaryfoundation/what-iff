package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/drawio"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// DiagramWorkshop is what a build that can run draw.io and Graphviz somewhere (a container beside
// the conversation) adds to the diagram tool: laying an existing diagram out, drawing it as a
// picture, and converting Mermaid into native shapes. The public build has none, and the tool then
// offers none of these actions.
type DiagramWorkshop interface {
	// Layout returns the diagram with its elements (on page, or the first page) placed again, the
	// flow running in direction ("right" or "down"); ids, labels and styles are kept.
	Layout(ctx context.Context, chat *models.Chat, diagram []byte, page, direction string) ([]byte, error)
	// Render returns a PNG of the diagram's page (or its first page).
	Render(ctx context.Context, chat *models.Chat, diagram []byte, page string) ([]byte, error)
	// FromMermaid returns a new diagram, in plain draw.io XML, drawn from Mermaid source.
	FromMermaid(ctx context.Context, chat *models.Chat, source string) ([]byte, error)
}

// diagramWorkshop is the registered workshop, or nil.
var diagramWorkshop DiagramWorkshop

// SetDiagramWorkshop registers the build's workshop; called from an init.
func SetDiagramWorkshop(w DiagramWorkshop) { diagramWorkshop = w }

// diagramWorkshopActions are the actions a workshop adds.
var diagramWorkshopActions = []string{"layout", "render"}

const diagramWorkshopDescription = `

Also here:
- "layout": place a diagram's elements again (Graphviz), keeping ids, labels and styles; direction is the way the flow runs. Use it after adding several elements, or to tidy a diagram.
- "render": look at a diagram: a picture of the page follows the result (if you can see images). It also becomes the diagram's preview.
- "create" with mermaid (Mermaid source) instead of nodes: draws the Mermaid as native draw.io shapes the user can move and edit. Flowchart, sequence, class, state, ER, Gantt, pie and mindmap diagrams convert.`

// diagramToolSpecNow is the diagram tool's spec with the workshop's actions when one is registered.
func diagramToolSpecNow() FunctionToolSpec {
	if diagramWorkshop == nil {
		return DiagramToolSpec
	}
	spec := DiagramToolSpec
	spec.Description += diagramWorkshopDescription
	props := make(map[string]interface{}, len(spec.Properties)+1)
	for k, v := range spec.Properties {
		props[k] = v
	}
	action := map[string]interface{}{}
	for k, v := range spec.Properties["action"].(map[string]interface{}) {
		action[k] = v
	}
	action["enum"] = append(append([]string{}, action["enum"].([]string)...), diagramWorkshopActions...)
	props["action"] = action
	props["mermaid"] = map[string]interface{}{"type": "string", "description": "create: Mermaid source to draw as a native diagram, instead of nodes."}
	spec.Properties = props
	return spec
}

// workshopAction runs a workshop action, or says the action is unknown when there is no workshop.
func (t *DiagramTool) workshopAction(ctx context.Context, chat *models.Chat, a diagramArgs) (diagramResult, error) {
	switch a.Action {
	case "layout":
		var res diagramResult
		out, err := t.files.rewrite(ctx, chat, a.File, func(cur string) (string, error) {
			next, err := diagramWorkshop.Layout(ctx, chat, []byte(cur), a.Page, a.Direction)
			if err != nil {
				return "", fileRefusalf("could not lay the diagram out: %v", err)
			}
			if err := noNewProblems([]byte(cur), next); err != nil {
				return "", err
			}
			return string(next), nil
		})
		if err != nil {
			return diagramResult{}, err
		}
		res.File = out.File
		return res, nil
	case "render":
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
		png, err := diagramWorkshop.Render(ctx, chat, []byte(text), a.Page)
		if err != nil {
			return diagramResult{}, fileRefusalf("could not draw the diagram: %v", err)
		}
		AddResultImage(ctx, png, "image/png")
		// The first page, as the editor's preview shows it, is the file's preview too.
		if strings.TrimSpace(a.Page) == "" {
			t.storePreview(ctx, att, png)
		}
		ref := t.files.refEditable(ctx, chat, att, "")
		return diagramResult{File: &ref, Preview: "A picture of the diagram follows this result (if you can see images)."}, nil
	}
	return diagramResult{}, fileRefusalf("unknown action %q", a.Action)
}

// createFromMermaid makes a new diagram at p drawn from Mermaid source.
func (t *DiagramTool) createFromMermaid(ctx context.Context, chat *models.Chat, p, source string) (diagramResult, error) {
	content, err := diagramWorkshop.FromMermaid(ctx, chat, source)
	if err != nil {
		return diagramResult{}, fileRefusalf("could not draw the Mermaid: %v", err)
	}
	pages, err := drawio.List(content)
	if err != nil {
		return diagramResult{}, fileRefusalf("the Mermaid did not become a diagram: %v", err)
	}
	out, err := t.files.create(ctx, chat, p, string(content))
	if err != nil {
		return diagramResult{}, err
	}
	return diagramResult{File: out.File, Outline: drawio.Outline(pages)}, nil
}

// storePreview keeps png as the preview of the file's current object (ADR 0x049 §8). Best effort.
func (t *DiagramTool) storePreview(ctx context.Context, att *models.FileAttachment, png []byte) {
	if t.files.files == nil || att.S3Key == "" {
		return
	}
	if err := t.files.files.UploadFile(ctx, storage.FileKeyForPreview(att.S3Key), png, "image/png"); err != nil {
		t.files.logger.Warn("diagram: preview upload failed", zap.Error(err))
		return
	}
	if thumb, err := imageutil.GenerateThumbnail(png, imageutil.DefaultThumbnailMaxPx); err == nil {
		_ = t.files.files.UploadFile(ctx, storage.FileKeyForPreviewThumbnail(att.S3Key), thumb, "image/jpeg")
	}
}

// noNewProblems refuses a workshop result that leaves the diagram with problems it did not have.
func noNewProblems(before, after []byte) error {
	was, err := drawio.Validate(before)
	if err != nil {
		return diagramRefusal(err)
	}
	now, err := drawio.Validate(after)
	if err != nil {
		return fileRefusalf("the laid-out diagram could not be read: %v", err)
	}
	if len(now) > len(was) {
		return fileRefusalf("laying out would leave the diagram with new problems (%s); it was left as it was", now[len(now)-1].Message)
	}
	return nil
}

// workshopArgsProblem is checkDiagramArgs for the workshop's actions and arguments.
func workshopArgsProblem(a diagramArgs) (string, bool) {
	has := func(s string) bool { return strings.TrimSpace(s) != "" }
	switch a.Action {
	case "layout", "render":
		if diagramWorkshop == nil {
			return fmt.Sprintf("action %q is not available here", a.Action), true
		}
		if !has(a.File) {
			return fmt.Sprintf("action %q needs file", a.Action), true
		}
		return "", true
	case "create":
		if !has(a.Mermaid) {
			return "", false
		}
		if diagramWorkshop == nil {
			return "creating a diagram from Mermaid is not available here; give nodes and edges instead", true
		}
		if !has(a.Path) {
			return `action "create" needs path`, true
		}
		if len(a.Nodes)+len(a.Edges)+len(a.Lanes)+len(a.Notes) > 0 {
			return "give either mermaid or nodes, edges, lanes and notes, not both", true
		}
		return "", true
	}
	return "", false
}
