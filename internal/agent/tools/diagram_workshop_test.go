package tools

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

// fakeWorkshop lays out by moving every vertex, draws a 2×2 PNG and turns any Mermaid into one box.
type fakeWorkshop struct{ calls []string }

func (w *fakeWorkshop) Layout(_ context.Context, _ *models.Chat, d []byte, page, direction string) ([]byte, error) {
	w.calls = append(w.calls, "layout:"+direction)
	return bytes.ReplaceAll(d, []byte(`x="40"`), []byte(`x="400"`)), nil
}

func (w *fakeWorkshop) Render(context.Context, *models.Chat, []byte, string) ([]byte, error) {
	w.calls = append(w.calls, "render")
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	return buf.Bytes(), nil
}

func (w *fakeWorkshop) FromMermaid(_ context.Context, _ *models.Chat, source string) ([]byte, error) {
	w.calls = append(w.calls, "mermaid")
	return []byte(`<mxfile><diagram id="p" name="Page-1"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/>` +
		`<mxCell id="A" value="Start" style="rounded=1;" vertex="1" parent="1"><mxGeometry x="40" y="40" width="120" height="60" as="geometry"/></mxCell>` +
		`</root></mxGraphModel></diagram></mxfile>`), nil
}

func withWorkshop(t *testing.T) *fakeWorkshop {
	t.Helper()
	w := &fakeWorkshop{}
	SetDiagramWorkshop(w)
	t.Cleanup(func() { SetDiagramWorkshop(nil) })
	return w
}

func TestDiagramWorkshop_NotOfferedWithoutOne(t *testing.T) {
	fx, tool := newDiagramFixture()
	chat := fx.chat(uuid.New())
	requireRefused(t, callJSON(t, tool.Run, chat, map[string]any{"action": "layout", "file": "x.drawio"}), "not available")
	requireRefused(t, callJSON(t, tool.Run, chat, map[string]any{"action": "create", "path": "m", "mermaid": "flowchart LR\n A-->B"}), "not available")
	spec := diagramToolSpecNow()
	require.NotContains(t, spec.Properties["action"].(map[string]interface{})["enum"], "render")
	require.NotContains(t, spec.Properties, "mermaid")
}

func TestDiagramWorkshop_CreateFromMermaidLayoutAndRender(t *testing.T) {
	w := withWorkshop(t)
	fx, tool := newDiagramFixture()
	chat := fx.chat(uuid.New())

	spec := diagramToolSpecNow()
	require.Contains(t, spec.Properties["action"].(map[string]interface{})["enum"], "render")
	require.Contains(t, spec.Properties, "mermaid")
	require.NotContains(t, DiagramToolSpec.Properties["action"].(map[string]interface{})["enum"], "render", "the shared spec is not changed")

	res := callJSON(t, tool.Run, chat, map[string]any{"action": "create", "path": "diagrams/m", "mermaid": "flowchart LR\n A[Start]"})
	requireOK(t, res)
	require.Equal(t, "diagrams/m.drawio", res["file"].(map[string]any)["path"])
	require.Contains(t, res["outline"], `[A] vertex "Start"`)

	requireOK(t, callJSON(t, tool.Run, chat, map[string]any{"action": "layout", "file": "diagrams/m.drawio", "direction": "down"}))
	require.Contains(t, fx.raw(t, chat, "diagrams/m.drawio"), `x="400"`, "the laid-out diagram is saved")

	ctx, shown := WithResultImages(appTurn)
	out, err := tool.Run(ctx, chat, []byte(`{"action":"render","file":"diagrams/m.drawio"}`))
	require.NoError(t, err)
	require.Contains(t, out, `"success":true`)
	require.Len(t, shown.Images(), 1)
	att, err := fx.tool.resolve(context.Background(), chat, "diagrams/m.drawio")
	require.NoError(t, err)
	require.NotEmpty(t, fx.objects.objs[storage.FileKeyForPreview(att.S3Key)], "the picture is the diagram's preview")
	require.Equal(t, []string{"mermaid", "layout:down", "render"}, w.calls)

	requireRefused(t, callJSON(t, tool.Run, chat, map[string]any{"action": "create", "path": "n", "mermaid": "x", "nodes": []map[string]any{{"id": "a", "label": "A"}}}), "not both")
}

func TestDiagramWorkshop_LayoutThatBreaksTheDiagramIsRefused(t *testing.T) {
	withWorkshop(t)
	SetDiagramWorkshop(&breakingWorkshop{})
	fx, tool := newDiagramFixture()
	chat := fx.chat(uuid.New())
	requireOK(t, callJSON(t, tool.Run, chat, map[string]any{"action": "create", "path": "d", "nodes": []map[string]any{{"id": "a", "label": "A"}}}))
	before := fx.raw(t, chat, "d.drawio")
	requireRefused(t, callJSON(t, tool.Run, chat, map[string]any{"action": "layout", "file": "d.drawio"}), "new problems")
	require.Equal(t, before, fx.raw(t, chat, "d.drawio"))
}

// breakingWorkshop's layout points the node at a parent that does not exist.
type breakingWorkshop struct{ fakeWorkshop }

func (*breakingWorkshop) Layout(_ context.Context, _ *models.Chat, d []byte, _, _ string) ([]byte, error) {
	return []byte(strings.Replace(string(d), `parent="1"`, `parent="nowhere"`, 2)), nil
}
