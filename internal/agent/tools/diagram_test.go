package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/drawio"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

const userDiagram = `<mxfile host="Electron">
  <diagram id="p1" name="Flow">
    <mxGraphModel>
      <root>
        <mxCell id="0" />
        <mxCell id="1" parent="0" />
        <mxCell id="a" value="Start" style="rounded=1;html=1;" vertex="1" parent="1">
          <mxGeometry x="40" y="40" width="120" height="60" as="geometry" />
        </mxCell>
      </root>
    </mxGraphModel>
  </diagram>
</mxfile>
`

func newDiagramFixture() (*agentFileFixture, *DiagramTool) {
	fx := newAgentFileFixture()
	return fx, NewDiagramTool(fx.tool)
}

func (fx *agentFileFixture) raw(t *testing.T, chat *models.Chat, file string) string {
	t.Helper()
	att, err := fx.tool.resolve(context.Background(), chat, file)
	require.NoError(t, err)
	return string(fx.objects.objs[att.S3Key])
}

func TestDiagram_CreateListAndEdit(t *testing.T) {
	fx, tool := newDiagramFixture()
	chat := fx.chat(uuid.New())

	res := callJSON(t, tool.Run, chat, map[string]any{
		"action": "create", "path": "diagrams/order",
		"nodes": []map[string]any{{"id": "start", "label": "Order", "shape": "start", "lane": "c"}, {"id": "ship", "label": "Ship", "lane": "s"}},
		"edges": []map[string]any{{"source": "start", "target": "ship", "label": "paid"}},
		"lanes": []map[string]any{{"id": "c", "label": "Customer"}, {"id": "s", "label": "Shop"}},
		"notes": []map[string]any{{"id": "memo", "text": "Ships within a day"}},
	})
	requireOK(t, res)
	file := res["file"].(map[string]any)
	require.Equal(t, "diagrams/order.drawio", file["path"], "a path without an extension gets .drawio")
	require.Equal(t, true, file["editable"])
	require.Contains(t, res["outline"], `[start] vertex "Order"`)
	row := fx.store.rows[uuid.MustParse(file["id"].(string))]
	require.Equal(t, "application/vnd.jgraph.mxfile", row.FileType)
	require.Equal(t, models.FileOriginAgent, row.Origin)

	res = callJSON(t, tool.Run, chat, map[string]any{"action": "add_node", "file": "diagrams/order.drawio", "label": "Pack", "parent": "s"})
	requireOK(t, res)
	require.Equal(t, "n1", res["id"])
	res = callJSON(t, tool.Run, chat, map[string]any{"action": "add_edge", "file": "diagrams/order.drawio", "source": "ship", "target": "n1"})
	requireOK(t, res)
	edge := res["id"].(string)
	requireOK(t, callJSON(t, tool.Run, chat, map[string]any{"action": "set_label", "file": "diagrams/order.drawio", "id": "n1", "label": "Pack & label"}))
	requireOK(t, callJSON(t, tool.Run, chat, map[string]any{"action": "set_style", "file": "diagrams/order.drawio", "id": "n1", "style": "fillColor=#dae8fc"}))
	requireOK(t, callJSON(t, tool.Run, chat, map[string]any{"action": "move", "file": "diagrams/order.drawio", "id": "memo", "x": 600, "y": 10}))
	requireOK(t, callJSON(t, tool.Run, chat, map[string]any{"action": "add_container", "file": "diagrams/order.drawio", "container": "group", "label": "G"}))
	requireOK(t, callJSON(t, tool.Run, chat, map[string]any{"action": "add_note", "file": "diagrams/order.drawio", "label": "Later"}))

	res = callJSON(t, tool.Run, chat, map[string]any{"action": "list", "file": "diagrams/order.drawio"})
	requireOK(t, res)
	var pages []drawio.Page
	b, _ := json.Marshal(res["pages"])
	require.NoError(t, json.Unmarshal(b, &pages))
	byID := map[string]drawio.Element{}
	for _, e := range pages[0].Elements {
		byID[e.ID] = e
	}
	require.Equal(t, "Pack & label", byID["n1"].Label)
	require.Equal(t, "s", byID["n1"].Parent)
	require.Contains(t, byID["n1"].Style, "fillColor=#dae8fc")
	require.Equal(t, 600.0, byID["memo"].Geometry.X)
	require.Equal(t, "n1", byID[edge].Target)
	require.Equal(t, drawio.KindNote, byID["note1"].Kind)

	res = callJSON(t, tool.Run, chat, map[string]any{"action": "delete", "file": "diagrams/order.drawio", "id": "s"})
	requireOK(t, res)
	require.ElementsMatch(t, []any{"s", "ship", "n1", "e1", edge}, res["removed"])

	res = callJSON(t, tool.Run, chat, map[string]any{"action": "validate", "file": "diagrams/order.drawio"})
	requireOK(t, res)
	require.Equal(t, true, res["valid"])
}

func TestDiagram_RefusalsLeaveTheFileAlone(t *testing.T) {
	fx, tool := newDiagramFixture()
	chat := fx.chat(uuid.New())
	requireOK(t, callJSON(t, tool.Run, chat, map[string]any{"action": "create", "path": "d.drawio", "nodes": []map[string]any{{"id": "a", "label": "A"}}}))
	before := fx.raw(t, chat, "d.drawio")

	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"action": "add_edge", "file": "d.drawio", "source": "a", "target": "zz"}, `no element "zz"`},
		{map[string]any{"action": "set_style", "file": "d.drawio", "id": "a", "style": "fill color=red"}, "bad style"},
		{map[string]any{"action": "delete", "file": "d.drawio", "id": "1"}, "cannot be deleted"},
		{map[string]any{"action": "add_node", "file": "d.drawio", "id": "a"}, "already used"},
		{map[string]any{"action": "add_node", "file": "d.drawio", "page": "9"}, "no page"},
		{map[string]any{"action": "move", "file": "d.drawio", "id": "a"}, "needs file and id and x, y"},
		{map[string]any{"action": "set_label", "file": "d.drawio", "id": "a"}, "needs file and id and label"},
		{map[string]any{"action": "add_node", "file": "d.drawio", "nodes": []map[string]any{{"id": "x"}}}, `are for "create"`},
		{map[string]any{"action": "explode", "file": "d.drawio"}, "action must be one of"},
		{map[string]any{"action": "create", "path": "x.md", "nodes": []map[string]any{{"id": "a", "label": "A"}}}, "ends in .drawio"},
		{map[string]any{"action": "create", "path": "d.drawio", "nodes": []map[string]any{{"id": "a", "label": "A"}}}, "already at"},
		{map[string]any{"action": "create", "path": "e.drawio", "nodes": []map[string]any{{"id": "a", "label": "A", "lane": "q"}}}, "not in lanes"},
	} {
		requireRefused(t, callJSON(t, tool.Run, chat, tc.args), tc.want)
	}
	require.Equal(t, before, fx.raw(t, chat, "d.drawio"))
}

func TestDiagram_UserDiagramsNeedAGrant(t *testing.T) {
	fx, tool := newDiagramFixture()
	saver := &fakeSaver{store: fx.store, objects: fx.objects}
	fx.tool.WithUserFileSaver(saver)
	chat := fx.chat(uuid.New())
	upload := fx.userUpload("", "flow.drawio", userDiagram)

	// Anyone's diagram can be read.
	res := callJSON(t, tool.Run, chat, map[string]any{"action": "list", "file": "flow.drawio"})
	requireOK(t, res)
	require.Equal(t, false, res["file"].(map[string]any)["editable"])
	require.Contains(t, res["outline"], `[a] vertex "Start"`)

	requireRefused(t, callJSON(t, tool.Run, chat, map[string]any{"action": "set_label", "file": "flow.drawio", "id": "a", "label": "Go"}), "mentioning you in a comment")
	require.Empty(t, saver.saves)

	fx.store.grants = map[[2]uuid.UUID]bool{{chat.ID, upload.ID}: true}
	res = callJSON(t, tool.Run, chat, map[string]any{"action": "set_label", "file": "flow.drawio", "id": "a", "label": "Go"})
	requireOK(t, res)
	require.Equal(t, true, res["file"].(map[string]any)["editable"])
	require.Len(t, saver.saves, 1, "a granted user file is saved as a new version")
	require.Equal(t, strings.Replace(userDiagram, `value="Start"`, `value="Go"`, 1), string(saver.saves[0].Content), "only the label changed")
}

func TestDiagram_ReadRefusals(t *testing.T) {
	fx, tool := newDiagramFixture()
	chat := fx.chat(uuid.New())
	fx.userUpload("", "notes.md", "# Notes\n")
	fx.userUpload("", "packed.drawio", `<mxfile><diagram id="x" name="P">dZHBDoIwDIafhqPJGJcvKsdnEwm4zJ2Lpgnh4nuM</diagram></mxfile>`)
	fx.userUpload("", "broken.drawio", `<mxfile><diagram name="P"><mxGraphModel><root><mxCell id="0"/><mxCell id="v" vertex="1" parent="q"/></root></mxGraphModel></diagram></mxfile>`)

	requireRefused(t, callJSON(t, tool.Run, chat, map[string]any{"action": "list", "file": "notes.md"}), "not a draw.io diagram")
	// Any other .xml is not a diagram here, as it is not to comments, previews or the app.
	fx.userUpload("", "export.xml", userDiagram)
	requireRefused(t, callJSON(t, tool.Run, chat, map[string]any{"action": "list", "file": "export.xml"}), "not a draw.io diagram")
	requireRefused(t, callJSON(t, tool.Run, chat, map[string]any{"action": "list", "file": "packed.drawio"}), "compressed")
	res := callJSON(t, tool.Run, chat, map[string]any{"action": "validate", "file": "broken.drawio"})
	requireOK(t, res)
	require.Equal(t, false, res["valid"])
	require.Len(t, res["problems"], 2)

	sandboxed := fx.chat(uuid.New())
	sandboxed.ContextScope = models.ContextScopeSandbox
	requireRefused(t, callJSON(t, tool.Run, sandboxed, map[string]any{"action": "list", "file": "notes.md"}), "sandboxed")
}

func TestDiagram_ListIsCapped(t *testing.T) {
	pages := []drawio.Page{{Elements: make([]drawio.Element, 3)}, {Elements: make([]drawio.Element, 3)}}
	out, note := capElements(pages, 4)
	require.Len(t, out[0].Elements, 3)
	require.Len(t, out[1].Elements, 1)
	require.Contains(t, note, "2 elements are left out")
	_, note = capElements(pages, 10)
	require.Empty(t, note)
}

func TestDiagram_ListShowsThePreviewOfThisVersion(t *testing.T) {
	fx, tool := newDiagramFixture()
	chat := fx.chat(uuid.New())
	upload := fx.userUpload("", "flow.drawio", userDiagram)
	list := func() (map[string]any, []ResultImage) {
		ctx, shown := WithResultImages(appTurn)
		out, err := tool.Run(ctx, chat, []byte(`{"action":"list","file":"flow.drawio"}`))
		require.NoError(t, err)
		var res map[string]any
		require.NoError(t, json.Unmarshal([]byte(out), &res))
		return res, shown.Images()
	}

	res, images := list()
	require.Contains(t, res["preview"], "No picture of this version yet")
	require.Empty(t, images)

	var pic bytes.Buffer
	require.NoError(t, png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 4, 2))))
	fx.objects.objs[storage.FileKeyForPreview(upload.S3Key)] = pic.Bytes()
	res, images = list()
	require.Contains(t, res["preview"], "A picture of this version")
	require.Equal(t, []ResultImage{{Data: pic.Bytes(), MediaType: "image/png"}}, images)

	// Too large to show as it is: its thumbnail instead.
	pic.Reset()
	require.NoError(t, png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, diagramPictureMaxSide+1, 1))))
	fx.objects.objs[storage.FileKeyForPreview(upload.S3Key)] = pic.Bytes()
	fx.objects.objs[storage.FileKeyForPreviewThumbnail(upload.S3Key)] = []byte("jpeg")
	_, images = list()
	require.Equal(t, []ResultImage{{Data: []byte("jpeg"), MediaType: "image/jpeg"}}, images)
}

func TestDiagram_ListOnePageKeepsItsNumber(t *testing.T) {
	fx, tool := newDiagramFixture()
	chat := fx.chat(uuid.New())
	two := strings.Replace(userDiagram, "</mxfile>", `<diagram id="p2" name="Later"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/></root></mxGraphModel></diagram></mxfile>`, 1)
	fx.userUpload("", "two.drawio", two)
	for _, page := range []string{"Later", "2", "p2"} {
		res := callJSON(t, tool.Run, chat, map[string]any{"action": "list", "file": "two.drawio", "page": page})
		requireOK(t, res)
		require.True(t, strings.HasPrefix(res["outline"].(string), `Page 2 "Later":`), res["outline"])
		require.Equal(t, float64(2), res["pages"].([]any)[0].(map[string]any)["number"])
	}
	requireRefused(t, callJSON(t, tool.Run, chat, map[string]any{"action": "list", "file": "two.drawio", "page": "3"}), `the pages are "Flow", "Later"`)
}
