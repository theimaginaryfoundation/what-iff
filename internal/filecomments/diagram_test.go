package filecomments

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func diagramXML(cells string) string {
	return `<mxfile><diagram id="p1" name="Page-1"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/>` +
		cells + `</root></mxGraphModel></diagram>` +
		`<diagram id="p2" name="Page-2"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/>` +
		`<mxCell id="a" value="Other page" vertex="1" parent="1"><mxGeometry width="80" height="40" as="geometry"/></mxCell>` +
		`</root></mxGraphModel></diagram></mxfile>`
}

const (
	userCell  = `<mxCell id="a" value="User" vertex="1" parent="1"><mxGeometry x="40" y="40" width="120" height="60" as="geometry"/></mxCell>`
	agentCell = `<mxCell id="b" value="&lt;b&gt;Agent&lt;/b&gt;" style="html=1;" vertex="1" parent="1"><mxGeometry x="260" y="40" width="120" height="60" as="geometry"/></mxCell>`
)

func onCell(id, cell, page string) models.FileCommentThread {
	t := thread(id, 0, 3)
	t.Cell = &models.FileCommentCell{ID: cell, Page: page}
	return t
}

func TestADiagramsThreadsAreOnItsElements(t *testing.T) {
	ctx := context.Background()
	files := memFiles{"d1": []byte(diagramXML(userCell + agentCell))}
	store := &fakeStore{file: &models.FileAttachment{ID: uuid.New(), S3Key: "d1", Name: "flow.drawio", FileType: "application/vnd.jgraph.mxfile", ContentVersion: 1}}
	svc := New(store, files)

	// On an element: no range of the XML, the element's label as the quote, its page filled in.
	// An element that is not there puts its thread on the whole file.
	got, err := svc.Write(ctx, uuid.New(), store.file.ID, []models.FileCommentThread{
		onCell("t1", "a", "p1"), onCell("t2", "b", ""), onCell("t3", "nope", "p1"), thread("t4", 0, 3),
	}, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		cell  *models.FileCommentCell
		quote string
	}{
		{&models.FileCommentCell{ID: "a", Page: "p1"}, "User"},
		{&models.FileCommentCell{ID: "b", Page: "p1"}, "Agent"},
		{nil, "Hmm"},
		{nil, ""},
	}
	for i, w := range want {
		th := got.Threads[i]
		if th.Anchor != nil || (th.Cell == nil) != (w.cell == nil) || (w.cell != nil && *th.Cell != *w.cell) {
			t.Fatalf("thread %d placed %+v", i, th)
		}
		if w.cell != nil && th.Quote != w.quote {
			t.Fatalf("thread %d quoted %q", i, th.Quote)
		}
	}
	if store.set.BaselineKey != "d1" || store.set.BaselineText != "" {
		t.Fatalf("stored %+v", store.set)
	}

	// The diagram changes elsewhere (an agent): the User box is relabelled, the Agent box deleted.
	files["d2"] = []byte(diagramXML(`<mxCell id="a" value="Customer" vertex="1" parent="1"><mxGeometry x="40" y="40" width="120" height="60" as="geometry"/></mxCell>`))
	store.file.S3Key, store.file.ContentVersion = "d2", 2
	got, err = svc.Read(ctx, uuid.New(), store.file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c := got.Threads[0].Cell; c == nil || c.ID != "a" || got.Threads[0].Quote != "Customer" {
		t.Fatalf("relabelled: %+v", got.Threads[0])
	}
	if got.Threads[1].Cell != nil || got.Threads[1].Quote != "Agent" {
		t.Fatalf("deleted element: %+v", got.Threads[1])
	}
	if store.puts != 2 || store.set.BaselineKey != "d2" {
		t.Fatalf("stored %+v after %d puts", store.set, store.puts)
	}
}

func TestATextFilesThreadsAreNeverOnElements(t *testing.T) {
	svc, store, _ := setup(line)
	got, err := svc.Write(context.Background(), uuid.New(), store.file.ID, []models.FileCommentThread{onCell("t1", "a", "p1")}, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Threads[0].Cell != nil || got.Threads[0].Anchor == nil {
		t.Fatalf("placed %+v", got.Threads[0])
	}
}

func TestADiagramThatCannotBeReadKeepsItsThreadsElements(t *testing.T) {
	threads := OnCells([]models.FileCommentThread{onCell("t1", "a", "p1")}, "")
	if c := threads[0].Cell; c == nil || c.ID != "a" || threads[0].Anchor != nil {
		t.Fatalf("unread diagram: %+v", threads[0])
	}
}
