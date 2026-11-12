package xlsx

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

func TestList(t *testing.T) {
	threads, err := New().List(build(t, commentedBook()))
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 2 {
		t.Fatalf("got %d threads", len(threads))
	}
	a1, b3 := threads[0], threads[1]
	if a1.ID != tID3 || a1.On != "Sheet1!A1" || a1.Where != "sheet Sheet1, cell A1" || !a1.Resolved {
		t.Fatalf("A1 thread: %+v", a1)
	}
	if len(a1.Comments) != 1 || a1.Comments[0].AuthorID != "author:Pat" || a1.Comments[0].AuthorName != "Pat" {
		t.Fatalf("A1 comments: %+v", a1.Comments)
	}
	if b3.ID != tID1 || b3.On != "Sheet1!B3" || b3.Resolved || len(b3.Comments) != 2 {
		t.Fatalf("B3 thread: %+v", b3)
	}
	first, reply := b3.Comments[0], b3.Comments[1]
	if first.ID != tID1 || first.AuthorID != "user:u-1" || first.AuthorName != "Ann" || first.Text != "Is this & that right?\nLine two" {
		t.Fatalf("first: %+v", first)
	}
	if !first.CreatedAt.Equal(time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("created at %v", first.CreatedAt)
	}
	if reply.ID != tID2 || reply.AuthorID != "agent:a-1" || reply.AuthorName != "Helper" || reply.Text != "Yes" {
		t.Fatalf("reply: %+v", reply)
	}
	if !reply.CreatedAt.Equal(time.Date(2026, 11, 1, 10, 0, 0, 500e6, time.UTC)) {
		t.Fatalf("reply created at %v", reply.CreatedAt)
	}
}

func TestListEmpty(t *testing.T) {
	threads, err := New().List(build(t, plainBook()))
	if err != nil || len(threads) != 0 {
		t.Fatalf("got %v, %v", threads, err)
	}
}

func TestNotOffice(t *testing.T) {
	for _, data := range [][]byte{[]byte("not a zip"), build(t, []entry{{"a.txt", "x"}})} {
		if _, err := New().List(data); !errors.Is(err, officecomments.ErrNotOffice) {
			t.Fatalf("got %v", err)
		}
		if _, _, err := New().Add(data, officecomments.Anchor{Cell: "A1"}, officecomments.Comment{Text: "x"}); !errors.Is(err, officecomments.ErrNotOffice) {
			t.Fatalf("add got %v", err)
		}
	}
}

var when = time.Date(2026, 11, 12, 14, 5, 6, 780e6, time.UTC)

func TestAddFirstSheetOfPlainBook(t *testing.T) {
	in := build(t, plainBook())
	const id = "{DDDDDDDD-DDDD-4DDD-8DDD-DDDDDDDDDDDD}"
	out, got, err := New().Add(in, officecomments.Anchor{Cell: "$c$5"}, officecomments.Comment{ID: id, AuthorID: "user:u-7", AuthorName: "Lee <L>", CreatedAt: when, Text: "Check \"this\"\r\nplease"})
	if err != nil {
		t.Fatal(err)
	}
	if got != id {
		t.Fatalf("id %s", got)
	}
	p, names := parts(t, out)
	pid := regexp.MustCompile(`id="(\{[^"]+\})"`).FindStringSubmatch(p["xl/persons/person.xml"])
	if pid == nil {
		t.Fatalf("persons: %s", p["xl/persons/person.xml"])
	}
	want := map[string]string{
		"xl/threadedComments/threadedComment1.xml": xmlHead + `<ThreadedComments xmlns="` + nsThreaded + `" xmlns:x="` + nsMain + `"><threadedComment ref="C5" dT="2026-11-12T14:05:06.78" personId="` + pid[1] + `" id="` + id + `" done="0"><text>Check &quot;this&quot;
please</text></threadedComment></ThreadedComments>`,
		"xl/persons/person.xml": xmlHead + `<personList xmlns="` + nsThreaded + `" xmlns:x="` + nsMain + `"><person displayName="Lee &lt;L&gt;" id="` + pid[1] + `" userId="u-7" providerId="WhatIff"/></personList>`,
		"xl/comments1.xml": xmlHead + "\n" + `<comments` + revisionAttrs + ` xmlns="` + nsMain + `"><authors><author>Author</author><author>tc=` + id + `</author></authors><commentList><comment ref="C5" authorId="1" xr:uid="` + id + `"><text><r><t xml:space="preserve">` +
			legacyText([]string{"Check &quot;this&quot;\nplease"}) + `</t></r></text></comment></commentList></comments>`,
		"xl/drawings/vmlDrawing1.vml":         xmlHead + "\n" + `<xml xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel"><o:shapelayout v:ext="edit"><o:idmap v:ext="edit" data="1"/></o:shapelayout>` + vmlShapeType + vmlShape(1025, 4, 2) + `</xml>`,
		"xl/worksheets/_rels/sheet1.xml.rels": xmlHead + `<Relationships xmlns="` + nsRels + `"><Relationship Id="rId1" Type="` + relComments + `" Target="../comments1.xml"/><Relationship Id="rId2" Type="` + relVML + `" Target="../drawings/vmlDrawing1.vml"/><Relationship Id="rId3" Type="` + relThreaded + `" Target="../threadedComments/threadedComment1.xml"/></Relationships>`,
	}
	for name, body := range want {
		if p[name] != body {
			t.Errorf("%s:\n got %s\nwant %s", name, p[name], body)
		}
	}
	// ExcelJS's default anchor for C5, as it wrote it in the browser.
	if !strings.Contains(p["xl/drawings/vmlDrawing1.vml"], `<x:Anchor>3, 6, 3, 14, 5, 2, 7, 16</x:Anchor>`) {
		t.Error("vml anchor")
	}
	if !strings.Contains(p["xl/worksheets/sheet1.xml"], `<pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/><legacyDrawing r:id="rId2"/></worksheet>`) {
		t.Errorf("sheet: %s", p["xl/worksheets/sheet1.xml"])
	}
	if !strings.HasSuffix(p["xl/_rels/workbook.xml.rels"], `<Relationship Id="rId4" Type="`+relPerson+`" Target="persons/person.xml"/></Relationships>`) {
		t.Errorf("workbook rels: %s", p["xl/_rels/workbook.xml.rels"])
	}
	types := p["[Content_Types].xml"]
	for _, s := range []string{
		`<Override PartName="/xl/threadedComments/threadedComment1.xml" ContentType="` + ctThreaded + `"/>`,
		`<Override PartName="/xl/persons/person.xml" ContentType="` + ctPerson + `"/>`,
		`<Override PartName="/xl/comments1.xml" ContentType="` + ctComments + `"/>`,
		`<Default Extension="vml" ContentType="` + ctVML + `"/>`,
	} {
		if strings.Count(types, s) != 1 {
			t.Errorf("content types lack %s", s)
		}
	}
	// Untouched parts keep their bytes and the entries their order.
	before, after := raw(t, in), raw(t, out)
	for _, name := range []string{"_rels/.rels", "xl/workbook.xml", "xl/worksheets/sheet2.xml", "xl/styles.xml"} {
		if !bytes.Equal(before[name], after[name]) {
			t.Errorf("%s changed", name)
		}
	}
	_, inNames := parts(t, in)
	if strings.Join(names[:len(inNames)], ",") != strings.Join(inNames, ",") {
		t.Errorf("order %v", names)
	}
	threads, err := New().List(out)
	if err != nil || len(threads) != 1 || threads[0].ID != id || threads[0].Comments[0].AuthorID != "user:u-7" || threads[0].Comments[0].Text != "Check \"this\"\nplease" {
		t.Fatalf("list after add: %+v %v", threads, err)
	}
}

func TestAddNamedSheet(t *testing.T) {
	out, id, err := New().Add(build(t, plainBook()), officecomments.Anchor{Sheet: "my data", Cell: "A1"}, officecomments.Comment{AuthorID: "author:Sam", Text: "Hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !guidRe.MatchString(id) {
		t.Fatalf("id %s", id)
	}
	p, _ := parts(t, out)
	// legacyDrawing goes before tableParts and extLst.
	if !strings.Contains(p["xl/worksheets/sheet2.xml"], `<legacyDrawing r:id="rId2"/><tableParts count="0"/><extLst>`) {
		t.Errorf("sheet2: %s", p["xl/worksheets/sheet2.xml"])
	}
	if _, ok := p["xl/worksheets/_rels/sheet1.xml.rels"]; ok {
		t.Error("sheet1 got rels")
	}
	if !strings.Contains(p["xl/persons/person.xml"], `displayName="Sam" id="`) || !strings.Contains(p["xl/persons/person.xml"], `userId="Sam" providerId="None"/>`) {
		t.Errorf("persons: %s", p["xl/persons/person.xml"])
	}
	// The row-0 anchor starts at the top, as ExcelJS's does.
	if !strings.Contains(p["xl/drawings/vmlDrawing1.vml"], `<x:Anchor>1, 6, 0, 14, 3, 2, 4, 16</x:Anchor>`) {
		t.Error("vml anchor")
	}
	threads, err := New().List(out)
	if err != nil || len(threads) != 1 || threads[0].On != "'My Data'!A1" || threads[0].Where != "sheet My Data, cell A1" || threads[0].Comments[0].AuthorID != "author:Sam" {
		t.Fatalf("list: %+v %v", threads, err)
	}
	if threads[0].Comments[0].CreatedAt.IsZero() || time.Since(threads[0].Comments[0].CreatedAt) > time.Minute {
		t.Fatalf("created at %v", threads[0].Comments[0].CreatedAt)
	}
}

func TestAddBesideExistingComments(t *testing.T) {
	in := build(t, commentedBook())
	out, id, err := New().Add(in, officecomments.Anchor{Sheet: "Sheet1", Cell: "D4"}, officecomments.Comment{AuthorID: "user:u-1", AuthorName: "Ann B", CreatedAt: when, Text: "New"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := parts(t, out)
	// Ann is known by her user id, so no one is added.
	if strings.Count(p["xl/persons/person.xml"], "<person ") != 3 {
		t.Errorf("persons: %s", p["xl/persons/person.xml"])
	}
	if !strings.HasSuffix(p["xl/threadedComments/threadedComment1.xml"], `<threadedComment ref="D4" dT="2026-11-12T14:05:06.78" personId="`+pAnn+`" id="`+id+`" done="0"><text>New</text></threadedComment></ThreadedComments>`) {
		t.Errorf("threaded: %s", p["xl/threadedComments/threadedComment1.xml"])
	}
	if !strings.Contains(p["xl/comments1.xml"], `<author>tc=`+tID3+`</author><author>tc=`+id+`</author></authors>`) ||
		!strings.Contains(p["xl/comments1.xml"], `<comment ref="D4" authorId="3" xr:uid="`+id+`"><text><r><t xml:space="preserve">`+legacyText([]string{"New"})+`</t></r></text></comment></commentList>`) {
		t.Errorf("legacy: %s", p["xl/comments1.xml"])
	}
	if strings.Count(p["xl/comments1.xml"], "xmlns:xr=") != 1 {
		t.Error("xr declared twice")
	}
	if !strings.HasSuffix(p["xl/drawings/vmlDrawing1.vml"], vmlShape(1027, 3, 3)+`</xml>`) || strings.Count(p["xl/drawings/vmlDrawing1.vml"], `id="_x0000_t202"`) != 1 {
		t.Errorf("vml: %s", p["xl/drawings/vmlDrawing1.vml"])
	}
	before, _ := parts(t, in)
	for _, name := range []string{"xl/worksheets/sheet1.xml", "xl/worksheets/_rels/sheet1.xml.rels", "xl/_rels/workbook.xml.rels", "[Content_Types].xml"} {
		if p[name] != before[name] {
			t.Errorf("%s changed", name)
		}
	}
}

func TestAddBadAnchors(t *testing.T) {
	data := build(t, commentedBook())
	for _, at := range []officecomments.Anchor{
		{},
		{Quote: "text"},
		{Cell: "B0"},
		{Cell: "AAAA1"},
		{Cell: "XFE1"},
		{Cell: "A1048577"},
		{Cell: "A1:B2"},
		{Sheet: "Nope", Cell: "A1"},
		{Cell: "B3"},
		{Sheet: "Sheet1", Cell: "a1"},
	} {
		if _, _, err := New().Add(data, at, officecomments.Comment{Text: "x"}); !errors.Is(err, officecomments.ErrBadAnchor) {
			t.Errorf("%+v: got %v", at, err)
		}
	}
	// A cell with a plain note has no room for a thread either.
	p, _ := parts(t, data)
	var withNote []entry
	for _, e := range commentedBook() {
		if e.name == "xl/comments1.xml" {
			e.body = strings.Replace(p[e.name], `</commentList>`, `<comment ref="E9" authorId="0"><text><r><t>note</t></r></text></comment></commentList>`, 1)
		}
		withNote = append(withNote, e)
	}
	if _, _, err := New().Add(build(t, withNote), officecomments.Anchor{Cell: "E9"}, officecomments.Comment{Text: "x"}); !errors.Is(err, officecomments.ErrBadAnchor) {
		t.Errorf("note: got %v", err)
	}
}

func TestReply(t *testing.T) {
	in := build(t, commentedBook())
	const id = "{EEEEEEEE-EEEE-4EEE-8EEE-EEEEEEEEEEEE}"
	out, err := New().Reply(in, tID1, officecomments.Comment{ID: id, AuthorID: "author:Pat", AuthorName: "Pat", CreatedAt: when, Text: "Agreed"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := parts(t, out)
	// After the thread's last reply, before the next thread.
	if !strings.Contains(p["xl/threadedComments/threadedComment1.xml"], `<text>Yes</text></threadedComment><threadedComment ref="B3" dT="2026-11-12T14:05:06.78" personId="`+pPat+`" id="`+id+`" parentId="`+tID1+`"><text>Agreed</text></threadedComment><threadedComment ref="A1"`) {
		t.Errorf("threaded: %s", p["xl/threadedComments/threadedComment1.xml"])
	}
	if !strings.Contains(p["xl/comments1.xml"], `<comment ref="B3" authorId="1" xr:uid="`+tID1+`"><text><r><t xml:space="preserve">`+legacyB3+"\n\nReply:\n    Agreed</t></r></text></comment>") {
		t.Errorf("legacy: %s", p["xl/comments1.xml"])
	}
	before, _ := parts(t, in)
	if p["xl/drawings/vmlDrawing1.vml"] != before["xl/drawings/vmlDrawing1.vml"] || p["xl/persons/person.xml"] != before["xl/persons/person.xml"] {
		t.Error("vml or persons changed")
	}
	threads, _ := New().List(out)
	b3 := threads[1]
	if len(b3.Comments) != 3 || b3.Comments[2].ID != id || b3.Comments[2].AuthorID != "author:Pat" || b3.Comments[2].Text != "Agreed" {
		t.Fatalf("after reply: %+v", b3)
	}
	if _, err := New().Reply(in, "{nope}", officecomments.Comment{Text: "x"}); !errors.Is(err, officecomments.ErrNoThread) {
		t.Fatalf("unknown thread: %v", err)
	}
	// A reply id is never the id of another comment.
	out2, err := New().Reply(in, tID3, officecomments.Comment{ID: tID2, Text: "again"})
	if err != nil {
		t.Fatal(err)
	}
	threads, _ = New().List(out2)
	if c := threads[0].Comments[1]; c.ID == tID2 || !guidRe.MatchString(c.ID) {
		t.Fatalf("reply id %s", c.ID)
	}
}

func TestResolve(t *testing.T) {
	in := build(t, commentedBook())
	out, err := New().Resolve(in, tID1, true)
	if err != nil {
		t.Fatal(err)
	}
	out, err = New().Resolve(out, tID3, false)
	if err != nil {
		t.Fatal(err)
	}
	threads, _ := New().List(out)
	if threads[0].Resolved || !threads[1].Resolved {
		t.Fatalf("resolved: %+v", threads)
	}
	p, _ := parts(t, out)
	before, _ := parts(t, in)
	want := strings.Replace(strings.Replace(before["xl/threadedComments/threadedComment1.xml"], `id="`+tID1+`" done="0"`, `id="`+tID1+`" done="1"`, 1), `id="`+tID3+`" done="1"`, `id="`+tID3+`" done="0"`, 1)
	if p["xl/threadedComments/threadedComment1.xml"] != want {
		t.Errorf("threaded: %s", p["xl/threadedComments/threadedComment1.xml"])
	}
	after := raw(t, out)
	for name, b := range raw(t, in) {
		if name != "xl/threadedComments/threadedComment1.xml" && !bytes.Equal(b, after[name]) {
			t.Errorf("%s changed", name)
		}
	}
	if _, err := New().Resolve(in, "{nope}", true); !errors.Is(err, officecomments.ErrNoThread) {
		t.Fatalf("unknown thread: %v", err)
	}
	// A thread Excel wrote without done gets one.
	if got := setAttr(`<threadedComment ref="A1" id="x">`, "done", "1"); got != `<threadedComment ref="A1" id="x" done="1">` {
		t.Fatal(got)
	}
}

func TestAgentAuthor(t *testing.T) {
	agent := officecomments.Comment{AuthorID: "agent:a-9", AuthorName: "Scout", Text: "Looked"}
	out, id, err := New().Add(build(t, commentedBook()), officecomments.Anchor{Cell: "F2"}, agent)
	if err != nil {
		t.Fatal(err)
	}
	out, err = New().Reply(out, id, agent)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := parts(t, out)
	if strings.Count(p["xl/persons/person.xml"], `userId="agent:a-9" providerId="WhatIff"`) != 1 || strings.Count(p["xl/persons/person.xml"], "<person ") != 4 {
		t.Errorf("persons: %s", p["xl/persons/person.xml"])
	}
	threads, _ := New().List(out)
	for _, th := range threads {
		if th.ID != id {
			continue
		}
		for _, c := range th.Comments {
			if c.AuthorID != "agent:a-9" || c.AuthorName != "Scout" {
				t.Fatalf("agent came back as %+v", c)
			}
		}
		return
	}
	t.Fatal("thread not listed")
}

func TestHelpers(t *testing.T) {
	if got := excelDate(when); got != "2026-11-12T14:05:06.78" {
		t.Fatal(got)
	}
	if r, c, ok := parseCell("XFD1048576"); !ok || r != 1048575 || c != 16383 || cellA1(r, c) != "XFD1048576" {
		t.Fatal("last cell")
	}
	if got := relTarget("xl/worksheets/sheet1.xml", "xl/comments1.xml"); got != "../comments1.xml" {
		t.Fatal(got)
	}
	if got := relTarget("xl/workbook.xml", "xl/persons/person.xml"); got != "persons/person.xml" {
		t.Fatal(got)
	}
	if got := resolve("", "xl/workbook.xml"); got != "xl/workbook.xml" {
		t.Fatal(got)
	}
	if got := sheetRef("It's", 0, 0); got != "'It''s'!A1" {
		t.Fatal(got)
	}
	if got := escapeXML("a\x01<b>'\""); got != "a&lt;b&gt;&apos;&quot;" {
		t.Fatal(got)
	}
}

func TestSheetNamedTakesAQuotedName(t *testing.T) {
	b := &book{sheets: []sheet{{name: "Plan"}, {name: "Bob's Sheet"}}}
	for _, name := range []string{"Bob's Sheet", "'Bob''s Sheet'", "bob's sheet"} {
		if s, ok := b.sheetNamed(name); !ok || s.name != "Bob's Sheet" {
			t.Fatalf("%q: %+v %v", name, s, ok)
		}
	}
}

func TestABrokenPartFailsTheChangeRatherThanBeingWrittenOver(t *testing.T) {
	broken := func(name, body string) []byte {
		entries := commentedBook()
		for i := range entries {
			if entries[i].name == name {
				entries[i].body = body
			}
		}
		return build(t, entries)
	}
	note := officecomments.Comment{AuthorID: "user:u-1", AuthorName: "Ann B", CreatedAt: when, Text: "New"}
	deep := `<?xml version="1.0"?><ThreadedComments xmlns="http://schemas.microsoft.com/office/spreadsheetml/2018/threadedcomments">` +
		strings.Repeat("<a>", officecomments.MaxXMLDepth) + strings.Repeat("</a>", officecomments.MaxXMLDepth) + `</ThreadedComments>`
	for _, c := range []struct {
		name, part, body string
		want             error
	}{
		{"unparsable threads", "xl/threadedComments/threadedComment1.xml", "<ThreadedComments><oops></ThreadedComments>", officecomments.ErrNotOffice},
		{"threads nested too deep", "xl/threadedComments/threadedComment1.xml", deep, officecomments.ErrTooLarge},
		{"unparsable sheet relationships", "xl/worksheets/_rels/sheet1.xml.rels", "<Relationships><", officecomments.ErrNotOffice},
	} {
		data := broken(c.part, c.body)
		if _, _, err := New().Add(data, officecomments.Anchor{Sheet: "Sheet1", Cell: "D4"}, note); !errors.Is(err, c.want) {
			t.Errorf("%s: add: %v", c.name, err)
		}
		if _, err := New().List(data); !errors.Is(err, c.want) {
			t.Errorf("%s: list: %v", c.name, err)
		}
	}
}
