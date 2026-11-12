package docx

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
	"github.com/theimaginaryfoundation/what-iff/internal/officecomments/officecommentstest"
)

const (
	wNS   = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	w14NS = `xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml"`
)

type part struct{ name, body string }

func build(t *testing.T, parts ...part) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, p.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func entries(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func rawEntries(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		off, err := f.DataOffset()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = data[off : off+int64(f.CompressedSize64)]
	}
	return out
}

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`</Types>`

const rootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`</Relationships>`

func document(body string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document ` + wNS + ` ` + w14NS + `><w:body>` + body + `<w:sectPr/></w:body></w:document>`
}

// plain is a file with no comments.
func plain(t *testing.T, body string) []byte {
	return build(t,
		part{"[Content_Types].xml", contentTypes},
		part{"_rels/.rels", rootRels},
		part{"word/document.xml", document(body)},
		part{"word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`},
		part{"docProps/app.xml", `<Properties>untouched</Properties>`},
	)
}

// commented is a file as the browser saves one thread with a reply, resolved, by a WhatIff user
// and an agent.
func commented(t *testing.T) []byte {
	head := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
	mc := `xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"`
	rels := head + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	cts := strings.TrimSuffix(contentTypes, `</Types>`)
	for i, p := range commentParts {
		rels += `<Relationship Id="rId` + string(rune('1'+i)) + `" Type="` + p.rel + `" Target="` + p.file + `"/>`
		cts += `<Override PartName="/word/` + p.file + `" ContentType="` + p.contentType + `"/>`
	}
	rels += `</Relationships>`
	cts += `</Types>`
	return build(t,
		part{"[Content_Types].xml", cts},
		part{"_rels/.rels", rootRels},
		part{"word/document.xml", document(
			`<w:p><w:r><w:t>Intro.</w:t></w:r></w:p>` +
				`<w:p><w:r><w:t xml:space="preserve">The </w:t></w:r>` +
				`<w:commentRangeStart w:id="0"/><w:commentRangeStart w:id="1"/>` +
				`<w:r><w:rPr><w:b/></w:rPr><w:t>quick</w:t></w:r><w:r><w:t xml:space="preserve"> fox</w:t></w:r>` +
				`<w:commentRangeEnd w:id="0"/><w:r><w:commentReference w:id="0"/></w:r>` +
				`<w:commentRangeEnd w:id="1"/><w:r><w:commentReference w:id="1"/></w:r>` +
				`<w:r><w:t xml:space="preserve"> jumps.</w:t></w:r></w:p>`)},
		part{"word/_rels/document.xml.rels", rels},
		part{"word/comments.xml", head + `<w:comments ` + wNS + ` ` + w14NS + ` xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml" ` + mc + ` mc:Ignorable="w14 w15">` +
			`<w:comment w:id="0" w:author="Pat Doe" w:date="2026-11-01T10:00:00Z" w:initials="PD">` +
			`<w:p w14:paraId="1A2B0000" w14:textId="77777777"><w:r><w:annotationRef/></w:r><w:r><w:t xml:space="preserve">Is it quick?</w:t></w:r></w:p>` +
			`<w:p w14:paraId="1A2B3C4D" w14:textId="77777777"><w:r><w:t xml:space="preserve">Really &amp; truly</w:t></w:r></w:p></w:comment>` +
			`<w:comment w:id="1" w:author="Helper" w:date="2026-11-01T11:00:00Z" w:initials="H">` +
			`<w:p w14:paraId="22223333" w14:textId="77777777"><w:r><w:annotationRef/></w:r><w:r><w:t xml:space="preserve">Yes.</w:t></w:r></w:p></w:comment>` +
			`</w:comments>`},
		part{"word/commentsExtended.xml", head + `<w15:commentsEx xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml" ` + mc + ` mc:Ignorable="w15">` +
			`<w15:commentEx w15:paraId="1A2B3C4D" w15:done="1"/><w15:commentEx w15:paraId="22223333" w15:paraIdParent="1A2B3C4D" w15:done="1"/></w15:commentsEx>`},
		part{"word/commentsIds.xml", head + `<w16cid:commentsIds xmlns:w16cid="http://schemas.microsoft.com/office/word/2016/wordml/cid" ` + mc + ` mc:Ignorable="w16cid">` +
			`<w16cid:commentId w16cid:paraId="1A2B3C4D" w16cid:durableId="0ABC0001"/><w16cid:commentId w16cid:paraId="22223333" w16cid:durableId="0ABC0002"/></w16cid:commentsIds>`},
		part{"word/commentsExtensible.xml", head + `<w16cex:commentsExtensible xmlns:w16cex="http://schemas.microsoft.com/office/word/2018/wordml/cex" ` + mc + ` mc:Ignorable="w16cex">` +
			`<w16cex:commentExtensible w16cex:durableId="0ABC0001" w16cex:dateUtc="2026-11-01T09:00:00Z"/></w16cex:commentsExtensible>`},
		part{"word/people.xml", head + `<w15:people xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml" ` + mc + ` mc:Ignorable="w15">` +
			`<w15:person w15:author="Pat Doe"><w15:presenceInfo w15:providerId="WhatIff" w15:userId="u-1"/></w15:person>` +
			`<w15:person w15:author="Helper"><w15:presenceInfo w15:providerId="WhatIff" w15:userId="agent:p-9"/></w15:person></w15:people>`},
		part{"docProps/app.xml", `<Properties>untouched</Properties>`},
	)
}

func TestListBrowserFile(t *testing.T) {
	threads, err := New().List(commented(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads: %+v", threads)
	}
	th := threads[0]
	if th.ID != "0ABC0001" || th.On != "quick fox" || th.Where != "paragraph 2" || !th.Resolved {
		t.Fatalf("thread: %+v", th)
	}
	if len(th.Comments) != 2 {
		t.Fatalf("comments: %+v", th.Comments)
	}
	first, reply := th.Comments[0], th.Comments[1]
	if first.ID != "0ABC0001" || first.AuthorID != "user:u-1" || first.AuthorName != "Pat Doe" ||
		first.Text != "Is it quick?\nReally & truly" || !first.CreatedAt.Equal(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("first: %+v", first)
	}
	// No dateUtc: w:date read as written.
	if reply.ID != "0ABC0002" || reply.AuthorID != "agent:p-9" || reply.Text != "Yes." ||
		!reply.CreatedAt.Equal(time.Date(2026, 11, 1, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("reply: %+v", reply)
	}
}

func TestNotOffice(t *testing.T) {
	if _, err := New().List([]byte("hello")); !errors.Is(err, officecomments.ErrNotOffice) {
		t.Fatalf("got %v", err)
	}
}

func TestAddInsideOneRun(t *testing.T) {
	data := plain(t, `<w:p><w:r w:rsidR="00A1"><w:rPr><w:i/></w:rPr><w:t>Hello brave world</w:t></w:r></w:p>`)
	out, id, err := New().Add(data, officecomments.Anchor{Quote: "brave"}, officecomments.Comment{
		AuthorID: "agent:p-1", AuthorName: "Claude", Text: "Bold?\nMaybe <not>",
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := entries(t, out)["word/document.xml"]
	want := `<w:p><w:r w:rsidR="00A1"><w:rPr><w:i/></w:rPr><w:t xml:space="preserve">Hello </w:t></w:r>` +
		`<w:commentRangeStart w:id="0"/>` +
		`<w:r w:rsidR="00A1"><w:rPr><w:i/></w:rPr><w:t xml:space="preserve">brave</w:t></w:r>` +
		`<w:commentRangeEnd w:id="0"/><w:r><w:commentReference w:id="0"/></w:r>` +
		`<w:r w:rsidR="00A1"><w:rPr><w:i/></w:rPr><w:t xml:space="preserve"> world</w:t></w:r></w:p>`
	if !strings.Contains(doc, want) {
		t.Fatalf("document:\n%s", doc)
	}
	threads, err := New().List(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].ID != id || threads[0].On != "brave" || threads[0].Where != "paragraph 1" {
		t.Fatalf("threads: %+v (id %s)", threads, id)
	}
	c := threads[0].Comments[0]
	if c.ID != id || c.AuthorID != "agent:p-1" || c.AuthorName != "Claude" || c.Text != "Bold?\nMaybe <not>" || c.CreatedAt.IsZero() {
		t.Fatalf("comment: %+v", c)
	}
	files := entries(t, out)
	if !strings.Contains(files["word/people.xml"], `w15:providerId="WhatIff" w15:userId="agent:p-1"`) {
		t.Fatalf("people: %s", files["word/people.xml"])
	}
	if !strings.Contains(files["[Content_Types].xml"], `PartName="/word/comments.xml"`) ||
		!strings.Contains(files["word/_rels/document.xml.rels"], `Target="commentsExtended.xml"`) {
		t.Fatal("parts not related")
	}
	if !strings.Contains(files["word/comments.xml"], `mc:Ignorable="w14 w15"`) || !strings.Contains(files["word/comments.xml"], `w:initials="C"`) {
		t.Fatalf("comments: %s", files["word/comments.xml"])
	}
}

func TestAddAcrossRuns(t *testing.T) {
	data := plain(t, `<w:p><w:r><w:t>Alpha</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t xml:space="preserve">one tw</w:t></w:r><w:r><w:rPr><w:b/></w:rPr><w:t>o thr</w:t></w:r><w:r><w:t>ee</w:t></w:r></w:p>`)
	out, _, err := New().Add(data, officecomments.Anchor{Quote: "two three"}, officecomments.Comment{AuthorID: "user:u-1", AuthorName: "Pat", Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	doc := entries(t, out)["word/document.xml"]
	want := `<w:r><w:t xml:space="preserve">one </w:t></w:r><w:commentRangeStart w:id="0"/><w:r><w:t xml:space="preserve">tw</w:t></w:r>` +
		`<w:r><w:rPr><w:b/></w:rPr><w:t>o thr</w:t></w:r>` +
		`<w:r><w:t>ee</w:t></w:r><w:commentRangeEnd w:id="0"/><w:r><w:commentReference w:id="0"/></w:r></w:p>`
	if !strings.Contains(doc, want) {
		t.Fatalf("document:\n%s", doc)
	}
	threads, _ := New().List(out)
	if len(threads) != 1 || threads[0].On != "two three" || threads[0].Where != "paragraph 2" {
		t.Fatalf("threads: %+v", threads)
	}
	if !strings.Contains(entries(t, out)["word/people.xml"], `w15:userId="u-1"`) {
		t.Fatal("user not in people")
	}
}

func TestAddQuoteErrors(t *testing.T) {
	data := plain(t, `<w:p><w:r><w:t>one cat, one dog</w:t></w:r></w:p>`)
	f := New()
	cm := officecomments.Comment{AuthorName: "Pat", Text: "x"}
	if _, _, err := f.Add(data, officecomments.Anchor{Quote: "one"}, cm); !errors.Is(err, officecomments.ErrQuoteAmbiguous) {
		t.Fatalf("ambiguous: %v", err)
	}
	if _, _, err := f.Add(data, officecomments.Anchor{Quote: "bird"}, cm); !errors.Is(err, officecomments.ErrQuoteNotFound) {
		t.Fatalf("not found: %v", err)
	}
	if _, _, err := f.Add(data, officecomments.Anchor{Quote: "one", Occurrence: 3}, cm); !errors.Is(err, officecomments.ErrQuoteNotFound) {
		t.Fatalf("occurrence: %v", err)
	}
	if _, _, err := f.Add(data, officecomments.Anchor{}, cm); !errors.Is(err, officecomments.ErrBadAnchor) {
		t.Fatalf("empty: %v", err)
	}
	out, _, err := f.Add(data, officecomments.Anchor{Quote: "one", Occurrence: 2}, cm)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entries(t, out)["word/document.xml"], `one cat, </w:t></w:r><w:commentRangeStart w:id="0"/>`) {
		t.Fatal("second occurrence not marked")
	}
}

func TestReplyAndResolve(t *testing.T) {
	f := New()
	data := commented(t)
	out, err := f.Reply(data, "0ABC0001", officecomments.Comment{AuthorID: "agent:p-9", AuthorName: "Helper", Text: "Done."})
	if err != nil {
		t.Fatal(err)
	}
	files := entries(t, out)
	if !strings.Contains(files["word/document.xml"], `<w:commentRangeStart w:id="1"/><w:commentRangeStart w:id="2"/>`) ||
		!strings.Contains(files["word/document.xml"], `<w:commentReference w:id="1"/></w:r><w:commentRangeEnd w:id="2"/><w:r><w:commentReference w:id="2"/></w:r>`) {
		t.Fatalf("document:\n%s", files["word/document.xml"])
	}
	if !strings.Contains(files["word/commentsExtended.xml"], `w15:paraIdParent="1A2B3C4D" w15:done="1"/></w15:commentsEx>`) {
		t.Fatalf("extended: %s", files["word/commentsExtended.xml"])
	}
	if strings.Count(files["word/people.xml"], "<w15:person ") != 2 {
		t.Fatal("known author added again")
	}
	threads, _ := f.List(out)
	if len(threads) != 1 || len(threads[0].Comments) != 3 || threads[0].Comments[2].Text != "Done." || threads[0].Comments[2].AuthorID != "agent:p-9" {
		t.Fatalf("threads: %+v", threads)
	}

	out, err = f.Resolve(out, "0ABC0001", false)
	if err != nil {
		t.Fatal(err)
	}
	if ext := entries(t, out)["word/commentsExtended.xml"]; strings.Contains(ext, `done="1"`) {
		t.Fatalf("still done: %s", ext)
	}
	threads, _ = f.List(out)
	if threads[0].Resolved {
		t.Fatal("still resolved")
	}
	out, _ = f.Resolve(out, "0ABC0001", true)
	threads, _ = f.List(out)
	if !threads[0].Resolved {
		t.Fatal("not resolved")
	}

	if _, err := f.Reply(data, "nope", officecomments.Comment{Text: "x"}); !errors.Is(err, officecomments.ErrNoThread) {
		t.Fatalf("reply: %v", err)
	}
	if _, err := f.Resolve(data, "nope", true); !errors.Is(err, officecomments.ErrNoThread) {
		t.Fatalf("resolve: %v", err)
	}
}

func TestResolveAddedThread(t *testing.T) {
	f := New()
	out, id, err := f.Add(plain(t, `<w:p><w:r><w:t>text</w:t></w:r></w:p>`), officecomments.Anchor{Quote: "text"}, officecomments.Comment{AuthorName: "Pat", Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	out, err = f.Resolve(out, id, true)
	if err != nil {
		t.Fatal(err)
	}
	threads, _ := f.List(out)
	if !threads[0].Resolved || threads[0].Comments[0].AuthorID != "author:Pat" {
		t.Fatalf("threads: %+v", threads)
	}
	if !strings.Contains(entries(t, out)["word/people.xml"], `w15:providerId="None" w15:userId="Pat"`) {
		t.Fatal("named author not written as the browser writes it")
	}
}

func TestUntouchedPartsKeepTheirBytes(t *testing.T) {
	data := commented(t)
	out, err := New().Reply(data, "0ABC0001", officecomments.Comment{AuthorName: "Pat Doe", Text: "More."})
	if err != nil {
		t.Fatal(err)
	}
	before, after := rawEntries(t, data), rawEntries(t, out)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "docProps/app.xml", "word/_rels/document.xml.rels", "word/people.xml"} {
		if !bytes.Equal(before[name], after[name]) {
			t.Errorf("%s changed", name)
		}
	}
	zr, _ := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if zr.File[0].Name != "[Content_Types].xml" || zr.File[len(zr.File)-1].Name != "docProps/app.xml" {
		t.Fatal("entries reordered")
	}
}

func TestGivenIDIsKept(t *testing.T) {
	out, id, err := New().Add(plain(t, `<w:p><w:r><w:t>text</w:t></w:r></w:p>`), officecomments.Anchor{Quote: "text"},
		officecomments.Comment{ID: "0000BEEF", AuthorName: "Pat", Text: "x", CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)})
	if err != nil || id != "0000BEEF" {
		t.Fatalf("id %s, %v", id, err)
	}
	threads, _ := New().List(out)
	if !threads[0].Comments[0].CreatedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("date: %v", threads[0].Comments[0].CreatedAt)
	}
}

func TestHostileInput(t *testing.T) {
	f := New()

	// Pasted control characters in a comment or an author's name are dropped, not written raw.
	out, _, err := f.Add(plain(t, `<w:p><w:r><w:t>Hello brave world</w:t></w:r></w:p>`), officecomments.Anchor{Quote: "brave"},
		officecomments.Comment{AuthorID: "agent:p-1", AuthorName: "Cl\faude", Text: "see \v here"})
	if err != nil {
		t.Fatal(err)
	}
	if err := officecommentstest.WellFormed(out); err != nil {
		t.Fatal(err)
	}
	threads, err := f.List(out)
	if err != nil || threads[0].Comments[0].Text != "see  here" || threads[0].Comments[0].AuthorName != "Claude" {
		t.Fatalf("read back: %+v %v", threads, err)
	}

	// A part nested past MaxXMLDepth is refused rather than walked.
	deep := strings.Repeat("<w:sdt>", officecomments.MaxXMLDepth) + strings.Repeat("</w:sdt>", officecomments.MaxXMLDepth)
	if _, err := f.List(plain(t, `<w:p><w:r><w:t>x</w:t></w:r></w:p>`+deep)); !errors.Is(err, officecomments.ErrTooLarge) {
		t.Fatalf("deep: %v", err)
	}
}

func TestAWord2007CommentIsNotRepliedToOrResolvedAsIfItWere(t *testing.T) {
	// The commented file with its first comment's paragraph ids taken away, as Word 2007 wrote it.
	old := strings.NewReplacer(` w14:paraId="1A2B0000"`, ``, ` w14:paraId="1A2B3C4D"`, ``).Replace(string(commentedEntry(t, "word/comments.xml")))
	data := replaceEntry(t, commented(t), "word/comments.xml", old)
	threads, err := New().List(data)
	if err != nil {
		t.Fatal(err)
	}
	id := threads[0].ID
	if _, err := New().Reply(data, id, officecomments.Comment{AuthorName: "Helper", Text: "Done."}); !errors.Is(err, officecomments.ErrOldComment) {
		t.Fatalf("reply: %v", err)
	}
	if _, err := New().Resolve(data, id, false); !errors.Is(err, officecomments.ErrOldComment) {
		t.Fatalf("resolve: %v", err)
	}
}

func commentedEntry(t *testing.T, name string) []byte {
	return []byte(entries(t, commented(t))[name])
}

// replaceEntry is data with one entry's content replaced, the rest as they were.
func replaceEntry(t *testing.T, data []byte, name, content string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var parts []part
	for _, f := range zr.File {
		body := entries(t, data)[f.Name]
		if f.Name == name {
			body = content
		}
		parts = append(parts, part{f.Name, body})
	}
	return build(t, parts...)
}
