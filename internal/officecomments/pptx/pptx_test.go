package pptx

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
	"github.com/theimaginaryfoundation/what-iff/internal/officecomments/officecommentstest"
)

const (
	head        = "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\r\n"
	dana        = "{11111111-1111-4111-8111-111111111111}"
	me          = "{33333333-3333-4333-8333-333333333333}"
	shapeThread = "{AAAAAAAA-0000-4000-8000-000000000001}"
	slideThread = "{AAAAAAAA-0000-4000-8000-000000000002}"
	slideReply  = "{AAAAAAAA-0000-4000-8000-000000000003}"
	textThread  = "{AAAAAAAA-0000-4000-8000-000000000004}"
	legacyID    = "{00000000-0000-4000-8000-000000000001}"
)

// part is one zip entry of a test package.
type part struct{ name, body string }

func slideXML(shapes string) string {
	return head + `<p:sld xmlns:a="` + nsA + `" xmlns:r="` + nsR + `" xmlns:p="` + nsP + `"><p:cSld><p:spTree>` +
		`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>` + shapes +
		`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
}

// textShape is a text box; paragraphs are split on "|", line breaks are "^".
func textShape(id, name, text string, creationID string) string {
	ext := ""
	if creationID != "" {
		ext = `<a:extLst><a:ext uri="` + creationIDExt + `"><a16:creationId xmlns:a16="` + nsA16 + `" id="` + creationID + `"/></a:ext></a:extLst>`
	}
	cNvPr := `<p:cNvPr id="` + id + `" name="` + name + `"/>`
	if ext != "" {
		cNvPr = `<p:cNvPr id="` + id + `" name="` + name + `">` + ext + `</p:cNvPr>`
	}
	var paras strings.Builder
	for _, p := range strings.Split(text, "|") {
		paras.WriteString("<a:p>")
		for i, line := range strings.Split(p, "^") {
			if i > 0 {
				paras.WriteString(`<a:br><a:rPr lang="en-US"/></a:br>`)
			}
			if line != "" {
				paras.WriteString(`<a:r><a:rPr lang="en-US" dirty="0"/><a:t>` + escapeXML(line) + `</a:t></a:r>`)
			}
		}
		paras.WriteString(`<a:endParaRPr lang="en-US"/></a:p>`)
	}
	return `<p:sp><p:nvSpPr>` + cNvPr + `<p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="914400" y="914400"/><a:ext cx="3657600" cy="914400"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>` +
		`<p:txBody><a:bodyPr/><a:lstStyle/>` + paras.String() + `</p:txBody></p:sp>`
}

func rels(entries ...string) string {
	return head + `<Relationships xmlns="` + nsRel + `">` + strings.Join(entries, "") + `</Relationships>`
}

func relXML(id, typ, target string) string {
	return `<Relationship Id="` + id + `" Type="` + typ + `" Target="` + target + `"/>`
}

// basicParts is a two-slide presentation with no comments: "Quarterly plan" and "Budget" (twice
// "plan" on slide 2, in a second shape).
func basicParts() []part {
	return []part{
		{"[Content_Types].xml", head + `<Types xmlns="` + nsCT + `"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>` +
			`<Override PartName="/ppt/slides/slide1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>` +
			`<Override PartName="/ppt/slides/slide2.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/></Types>`},
		{"_rels/.rels", rels(relXML("rId1", relOfficeDoc, "ppt/presentation.xml"))},
		{"ppt/presentation.xml", head + `<p:presentation xmlns:a="` + nsA + `" xmlns:r="` + nsR + `" xmlns:p="` + nsP + `"><p:sldIdLst><p:sldId id="256" r:id="rId2"/><p:sldId id="257" r:id="rId3"/></p:sldIdLst><p:sldSz cx="9144000" cy="5143500"/></p:presentation>`},
		{"ppt/_rels/presentation.xml.rels", rels(relXML("rId2", relSlide, "slides/slide1.xml"), relXML("rId3", relSlide, "slides/slide2.xml"))},
		{"ppt/slides/slide1.xml", slideXML(textShape("2", "Title 1", "Quarterly plan", ""))},
		{"ppt/slides/_rels/slide1.xml.rels", rels()},
		{"ppt/slides/slide2.xml", slideXML(textShape("2", "Title 1", "Budget plan", "{44444444-4444-4444-8444-444444444444}") + textShape("3", "Body", "Cost|Plan^and plan", ""))},
		{"ppt/slides/_rels/slide2.xml.rels", rels()},
		{"docProps/app.xml", head + `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Microsoft Office PowerPoint</Application></Properties>`},
	}
}

func body(text string) string {
	return `<p188:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US" dirty="0"/><a:t>` + text + `</a:t></a:r></a:p></p188:txBody>`
}

// commentedParts adds comments as the browser and PowerPoint write them: on slide 1 a shape
// thread, a resolved slide thread with a reply and a thread on "plan"; on slide 2 a legacy thread
// with a threaded reply.
func commentedParts() []part {
	parts := basicParts()
	set := func(name, b string) {
		for i := range parts {
			if parts[i].name == name {
				parts[i].body = b
				return
			}
		}
		parts = append(parts, part{name, b})
	}
	slide1 := slideXML(textShape("2", "Title 1", "Quarterly plan", "{55555555-5555-4555-8555-555555555555}"))
	set("ppt/slides/slide1.xml", strings.Replace(slide1, "</p:sld>",
		`<p:extLst><p:ext uri="`+commentRelExt+`"><p188:commentRel xmlns:p188="`+nsP188+`" r:id="rId9"/></p:ext></p:extLst></p:sld>`, 1))
	set("ppt/slides/_rels/slide1.xml.rels", rels(relXML("rId9", relModernComments, "../comments/modernComment_100_0.xml")))
	mk := `<pc:docMk/><pc:sldMk cId="0" sldId="256"/>`
	set("ppt/comments/modernComment_100_0.xml", head+`<p188:cmLst xmlns:a="`+nsA+`" xmlns:r="`+nsR+`" xmlns:p188="`+nsP188+`">`+
		`<p188:cm id="`+shapeThread+`" authorId="`+dana+`" created="2026-11-02T17:15:00.000"><ac:deMkLst xmlns:ac="`+nsAC+`" xmlns:pc="`+nsPC+`">`+mk+`<ac:spMk id="2" creationId="{55555555-5555-4555-8555-555555555555}"/></ac:deMkLst><p188:pos x="4572000" y="0"/>`+body("Is this title final?")+`</p188:cm>`+
		`<p188:cm id="`+slideThread+`" authorId="`+me+`" created="2026-11-03T09:00:00.000Z" status="resolved"><pc:sldMkLst xmlns:pc="`+nsPC+`">`+mk+`</pc:sldMkLst><p188:pos x="152400" y="304800"/>`+
		`<p188:replyLst><p188:reply id="`+slideReply+`" authorId="`+dana+`" created="2026-11-03T10:00:00.000Z">`+body("Done &amp; dusted")+`</p188:reply></p188:replyLst>`+body("Fix the colours")+`</p188:cm>`+
		`<p188:cm id="`+textThread+`" authorId="`+me+`" created="2026-11-04T09:00:00.000Z"><ac:txMkLst xmlns:ac="`+nsAC+`" xmlns:pc="`+nsPC+`">`+mk+`<ac:spMk id="2" creationId="{55555555-5555-4555-8555-555555555555}"/><ac:txMk cp="10" len="4"><ac:context len="15" hash="`+itoa(officeArtHash("Quarterly plan\r"))+`"/></ac:txMk></ac:txMkLst><p188:pos x="4572000" y="0"/>`+body("Which plan?")+`</p188:cm></p188:cmLst>`)
	set("ppt/authors.xml", head+`<p188:authorLst xmlns:a="`+nsA+`" xmlns:r="`+nsR+`" xmlns:p188="`+nsP188+`">`+
		`<p188:author id="`+dana+`" name="Dana Reyes" initials="DR" userId="dana@example.com" providerId="AD"/>`+
		`<p188:author id="`+me+`" name="Chris Writer" initials="CW" userId="u-42" providerId="WhatIff"/></p188:authorLst>`)
	set("ppt/slides/_rels/slide2.xml.rels", rels(relXML("rId7", relLegacyComments, "../comments/comment1.xml")))
	set("ppt/comments/comment1.xml", head+`<p:cmLst xmlns:p="`+nsP+`" xmlns:p15="`+nsP15+`"><p:cm authorId="0" dt="2026-11-04T12:00:00.000" idx="1"><p:pos x="80" y="160"/><p:text>Cut this slide?</p:text></p:cm>`+
		`<p:cm authorId="1" dt="2026-11-04T13:00:00.000" idx="1"><p:pos x="80" y="160"/><p:text>No, keep it.</p:text><p:extLst><p:ext uri="{C676402C-5697-4E1C-873F-D02D1690AC5C}"><p15:threadingInfo timeZoneBias="0"><p15:parentCm authorId="0" idx="1"/></p15:threadingInfo></p:ext></p:extLst></p:cm></p:cmLst>`)
	set("ppt/commentAuthors.xml", head+`<p:cmAuthorLst xmlns:p="`+nsP+`"><p:cmAuthor id="0" name="Kim Lee" initials="KL" lastIdx="1" clrIdx="0"/><p:cmAuthor id="1" name="Ola Berg" initials="OB" lastIdx="1" clrIdx="1"/></p:cmAuthorLst>`)
	set("ppt/_rels/presentation.xml.rels", rels(relXML("rId2", relSlide, "slides/slide1.xml"), relXML("rId3", relSlide, "slides/slide2.xml"),
		relXML("rId4", relAuthors, "authors.xml"), relXML("rId5", relLegacyAuthors, "commentAuthors.xml")))
	ct := parts[0].body
	parts[0].body = strings.Replace(ct, "</Types>", `<Override PartName="/ppt/comments/modernComment_100_0.xml" ContentType="`+ctModernComments+`"/>`+
		`<Override PartName="/ppt/authors.xml" ContentType="`+ctAuthors+`"/>`+
		`<Override PartName="/ppt/comments/comment1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.comments+xml"/></Types>`, 1)
	return parts
}

func itoa(n uint32) string { return strconv.FormatUint(uint64(n), 10) }

func build(t *testing.T, parts []part) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// entries are a package's parts by name, and their names in order.
func entries(t *testing.T, data []byte) (map[string]string, []string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	var names []string
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(b)
		names = append(names, f.Name)
	}
	return out, names
}

func TestOfficeArtHash(t *testing.T) {
	// The context PowerPoint wrote for "Churn 3.1%" (pptx-comments.spec.ts).
	if got := officeArtHash("Churn 3.1%\r"); got != 1604253316 {
		t.Fatalf("hash = %d", got)
	}
	root, err := parseXML([]byte(`<p:txBody xmlns:p="` + nsP + `" xmlns:a="` + nsA + `"><a:bodyPr/><a:p><a:r><a:t>Quarterly plan</a:t></a:r></a:p></p:txBody>`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := textContext(root), `<ac:context len="15" hash="`+itoa(officeArtHash("Quarterly plan\r"))+`"/>`; got != want {
		t.Fatalf("context = %s, want %s", got, want)
	}
	// A line break is a "\v" and every paragraph ends with "\r".
	root, _ = parseXML([]byte(`<p:txBody xmlns:p="` + nsP + `" xmlns:a="` + nsA + `"><a:p><a:r><a:t>a</a:t></a:r><a:br/><a:r><a:t>b</a:t></a:r></a:p><a:p/></p:txBody>`))
	if got, want := textContext(root), `<ac:context len="5" hash="`+itoa(officeArtHash("a\vb\r\r"))+`"/>`; got != want {
		t.Fatalf("context = %s, want %s", got, want)
	}
}

func TestList(t *testing.T) {
	threads, err := New().List(build(t, commentedParts()))
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		id, on, where string
		resolved      bool
		comments      []string
	}
	var got []row
	for _, th := range threads {
		r := row{id: th.ID, on: th.On, where: th.Where, resolved: th.Resolved}
		for _, c := range th.Comments {
			r.comments = append(r.comments, c.ID+" "+c.AuthorID+" "+c.AuthorName+": "+c.Text+" @"+c.CreatedAt.Format(time.RFC3339))
		}
		got = append(got, r)
	}
	want := []row{
		{shapeThread, "Title 1", `slide 1, shape "Title 1"`, false, []string{shapeThread + " author:Dana Reyes Dana Reyes: Is this title final? @2026-11-02T17:15:00Z"}},
		{slideThread, "Slide 1", "slide 1", true, []string{
			slideThread + " user:u-42 Chris Writer: Fix the colours @2026-11-03T09:00:00Z",
			slideReply + " author:Dana Reyes Dana Reyes: Done & dusted @2026-11-03T10:00:00Z",
		}},
		{textThread, "plan", `slide 1, shape "Title 1"`, false, []string{textThread + " user:u-42 Chris Writer: Which plan? @2026-11-04T09:00:00Z"}},
		{legacyID, "Slide 2", "slide 2", false, []string{
			legacyID + " author:Kim Lee Kim Lee: Cut this slide? @2026-11-04T12:00:00Z",
			"{00000000-0000-4000-8000-000100000001} author:Ola Berg Ola Berg: No, keep it. @2026-11-04T13:00:00Z",
		}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d threads: %+v", len(got), got)
	}
	for i := range want {
		if got[i].id != want[i].id || got[i].on != want[i].on || got[i].where != want[i].where || got[i].resolved != want[i].resolved ||
			strings.Join(got[i].comments, "\n") != strings.Join(want[i].comments, "\n") {
			t.Errorf("thread %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestNotOffice(t *testing.T) {
	for _, data := range [][]byte{[]byte("not a zip"), build(t, []part{{"hello.txt", "hi"}})} {
		if _, err := New().List(data); !errors.Is(err, officecomments.ErrNotOffice) {
			t.Fatalf("err = %v", err)
		}
	}
}

var agent = officecomments.Comment{AuthorID: "agent:p-7", AuthorName: "Helper Bot", Text: "Is this <right> & final?\nSecond line"}

func TestAddOnQuote(t *testing.T) {
	data := build(t, basicParts())
	out, id, err := New().Add(data, officecomments.Anchor{Quote: "plan", Slide: 1}, agent)
	if err != nil {
		t.Fatal(err)
	}
	if !isGUID(id) {
		t.Fatalf("thread id %q", id)
	}
	parts, _ := entries(t, out)
	slide := parts["ppt/slides/slide1.xml"]
	creation := regexp.MustCompile(`<p:cNvPr id="2" name="Title 1"><a:extLst><a:ext uri="\{FF2B5EF4-FFF2-40B4-BE49-F238E27FC236\}"><a16:creationId xmlns:a16="` + regexp.QuoteMeta(nsA16) + `" id="(\{[0-9A-F-]{36}\})"/></a:ext></a:extLst></p:cNvPr>`).FindStringSubmatch(slide)
	if creation == nil {
		t.Fatalf("no creation id on the shape: %s", slide)
	}
	if !strings.HasSuffix(slide, `<p:extLst><p:ext uri="`+commentRelExt+`"><p188:commentRel xmlns:p188="`+nsP188+`" r:id="rId1"/></p:ext></p:extLst></p:sld>`) {
		t.Fatalf("slide not linked to its comments: %s", slide)
	}
	if got := parts["ppt/slides/_rels/slide1.xml.rels"]; !strings.Contains(got, relXML("rId1", relModernComments, "../comments/modernComment_100_0.xml")) {
		t.Fatalf("slide rels: %s", got)
	}
	cm := parts["ppt/comments/modernComment_100_0.xml"]
	if !regexp.MustCompile(`<ac:txMk cp="10" len="4"><ac:context len="15" hash="`+itoa(officeArtHash("Quarterly plan\r"))+`"/></ac:txMk>`).MatchString(cm) ||
		!strings.HasPrefix(cm, head+`<p188:cmLst xmlns:a="`+nsA+`" xmlns:r="`+nsR+`" xmlns:p188="`+nsP188+`"><p188:cm id="`+id+`" authorId="`) ||
		!strings.Contains(cm, `<ac:txMkLst xmlns:ac="`+nsAC+`" xmlns:pc="`+nsPC+`"><pc:docMk/><pc:sldMk cId="0" sldId="256"/><ac:spMk id="2" creationId="`+creation[1]+`"/><ac:txMk`) ||
		!strings.Contains(cm, `</ac:txMkLst><p188:pos x="3657600" y="0"/><p188:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US" dirty="0"/><a:t>Is this &lt;right&gt; &amp; final?</a:t></a:r></a:p><a:p><a:r><a:rPr lang="en-US" dirty="0"/><a:t>Second line</a:t></a:r></a:p></p188:txBody></p188:cm></p188:cmLst>`) {
		t.Fatalf("comment part:\n%s", cm)
	}
	authorID := regexp.MustCompile(`authorId="([^"]+)"`).FindStringSubmatch(cm)[1]
	if got, want := parts["ppt/authors.xml"], head+`<p188:authorLst xmlns:a="`+nsA+`" xmlns:r="`+nsR+`" xmlns:p188="`+nsP188+`"><p188:author id="`+authorID+`" name="Helper Bot" initials="HB" userId="agent:p-7" providerId="WhatIff"/></p188:authorLst>`; got != want {
		t.Fatalf("authors:\n got %s\nwant %s", got, want)
	}
	if got := parts["ppt/_rels/presentation.xml.rels"]; !strings.Contains(got, relXML("rId1", relAuthors, "authors.xml")) {
		t.Fatalf("presentation rels: %s", got)
	}
	ct := parts["[Content_Types].xml"]
	for _, want := range []string{`<Override PartName="/ppt/comments/modernComment_100_0.xml" ContentType="` + ctModernComments + `"/>`, `<Override PartName="/ppt/authors.xml" ContentType="` + ctAuthors + `"/>`} {
		if !strings.Contains(ct, want) {
			t.Fatalf("content types lack %s", want)
		}
	}

	threads, err := New().List(out)
	if err != nil || len(threads) != 1 {
		t.Fatalf("list: %v %+v", err, threads)
	}
	th := threads[0]
	c := th.Comments[0]
	if th.ID != id || th.On != "plan" || th.Where != `slide 1, shape "Title 1"` || c.ID != id || c.AuthorID != "agent:p-7" || c.AuthorName != "Helper Bot" || c.Text != agent.Text || time.Since(c.CreatedAt) > time.Minute {
		t.Fatalf("read back %+v", th)
	}
}

func TestAddKeepsCreationIDAndCountsOccurrences(t *testing.T) {
	data := build(t, basicParts())
	// Slide 2: "plan" in "Budget plan", then in the second shape after a line break ("Plan" is not it).
	if _, _, err := New().Add(data, officecomments.Anchor{Quote: "plan", Slide: 2}, agent); !errors.Is(err, officecomments.ErrQuoteAmbiguous) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := New().Add(data, officecomments.Anchor{Quote: "plan"}, agent); !errors.Is(err, officecomments.ErrQuoteAmbiguous) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := New().Add(data, officecomments.Anchor{Quote: "plan", Slide: 2, Occurrence: 2}, agent); err != nil {
		t.Fatal(err)
	}
	if _, _, err := New().Add(data, officecomments.Anchor{Quote: "plan", Slide: 2, Occurrence: 3}, agent); !errors.Is(err, officecomments.ErrQuoteNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := New().Add(data, officecomments.Anchor{Quote: "nowhere"}, agent); !errors.Is(err, officecomments.ErrQuoteNotFound) {
		t.Fatalf("err = %v", err)
	}
	for _, at := range []officecomments.Anchor{{}, {Slide: 3}, {Slide: -1}, {Quote: "plan", Slide: 9}} {
		if _, _, err := New().Add(data, at, agent); !errors.Is(err, officecomments.ErrBadAnchor) {
			t.Fatalf("%+v: err = %v", at, err)
		}
	}

	// The third "plan" overall is in the second shape, after its "Plan" paragraph and line break.
	out, _, err := New().Add(data, officecomments.Anchor{Quote: "plan", Occurrence: 3}, agent)
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := entries(t, out)
	cm := parts["ppt/comments/modernComment_101_0.xml"]
	if !strings.Contains(cm, `<ac:spMk id="3" creationId="{`) || !strings.Contains(cm, `<ac:txMk cp="14" len="4"><ac:context len="19" hash="`+itoa(officeArtHash("Cost\rPlan\vand plan\r"))+`"/>`) {
		t.Fatalf("comment part: %s", cm)
	}
	// Slide 2's first shape keeps the creation id it had; the second gets one.
	if n := strings.Count(parts["ppt/slides/slide2.xml"], "<a16:creationId"); n != 2 ||
		!strings.Contains(parts["ppt/slides/slide2.xml"], `id="{44444444-4444-4444-8444-444444444444}"`) {
		t.Fatalf("slide 2: %s", parts["ppt/slides/slide2.xml"])
	}
	out2, _, err := New().Add(data, officecomments.Anchor{Quote: "Budget"}, agent)
	if err != nil {
		t.Fatal(err)
	}
	parts, _ = entries(t, out2)
	if !strings.Contains(parts["ppt/comments/modernComment_101_0.xml"], `<ac:spMk id="2" creationId="{44444444-4444-4444-8444-444444444444}"/>`) {
		t.Fatalf("comment part: %s", parts["ppt/comments/modernComment_101_0.xml"])
	}
}

func TestAddOnSlideAndUntouchedParts(t *testing.T) {
	data := build(t, commentedParts())
	before, beforeNames := entries(t, data)
	first := officecomments.Comment{ID: "{BBBBBBBB-0000-4000-8000-000000000001}", AuthorID: "user:u-42", AuthorName: "Chris Writer",
		CreatedAt: time.Date(2026, 11, 12, 8, 30, 0, 0, time.UTC), Text: "Whole slide"}
	out, id, err := New().Add(data, officecomments.Anchor{Slide: 1}, first)
	if err != nil {
		t.Fatal(err)
	}
	if id != first.ID {
		t.Fatalf("id = %s", id)
	}
	after, afterNames := entries(t, out)
	// One slide pin is already along the top, so the new one goes after it; the author is known.
	wantCM := `<p188:cm id="` + id + `" authorId="` + me + `" created="2026-11-12T08:30:00.000Z"><pc:sldMkLst xmlns:pc="` + nsPC + `"><pc:docMk/><pc:sldMk cId="0" sldId="256"/></pc:sldMkLst>` +
		`<p188:pos x="482600" y="304800"/>` + body("Whole slide") + `</p188:cm></p188:cmLst>`
	part := after["ppt/comments/modernComment_100_0.xml"]
	if part != strings.TrimSuffix(before["ppt/comments/modernComment_100_0.xml"], "</p188:cmLst>")+wantCM {
		t.Fatalf("comment part:\n%s", part)
	}
	if strings.Join(afterNames, ",") != strings.Join(beforeNames, ",") {
		t.Fatalf("entries %v, were %v", afterNames, beforeNames)
	}
	for name, b := range before {
		if name != "ppt/comments/modernComment_100_0.xml" && after[name] != b {
			t.Errorf("%s changed", name)
		}
	}
	// Untouched entries are copied as they were stored.
	raw := func(data []byte) map[string][]byte {
		zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		out := map[string][]byte{}
		for _, f := range zr.File {
			rc, _ := f.OpenRaw()
			b, _ := io.ReadAll(rc)
			out[f.Name] = b
		}
		return out
	}
	r0, r1 := raw(data), raw(out)
	for name := range r0 {
		if name != "ppt/comments/modernComment_100_0.xml" && !bytes.Equal(r0[name], r1[name]) {
			t.Errorf("%s stored differently", name)
		}
	}
}

func TestReplyAndResolve(t *testing.T) {
	data := build(t, commentedParts())
	reply := officecomments.Comment{AuthorID: "agent:p-7", AuthorName: "Helper Bot", Text: "On it"}
	out, err := New().Reply(data, shapeThread, reply)
	if err != nil {
		t.Fatal(err)
	}
	out, err = New().Reply(out, shapeThread, officecomments.Comment{ID: "{CCCCCCCC-0000-4000-8000-000000000001}", AuthorID: "agent:p-7", AuthorName: "Helper Bot", Text: "Done"})
	if err != nil {
		t.Fatal(err)
	}
	out, err = New().Resolve(out, shapeThread, true)
	if err != nil {
		t.Fatal(err)
	}
	out, err = New().Resolve(out, slideThread, false)
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := entries(t, out)
	cm := parts["ppt/comments/modernComment_100_0.xml"]
	if !regexp.MustCompile(`<p188:cm id="` + regexp.QuoteMeta(shapeThread) + `" authorId="` + regexp.QuoteMeta(dana) + `" created="2026-11-02T17:15:00.000" status="resolved"><ac:deMkLst .*?</ac:deMkLst><p188:pos x="4572000" y="0"/>` +
		`<p188:replyLst><p188:reply id="\{[0-9A-F-]{36}\}" authorId="(\{[0-9A-F-]{36}\})" created="[^"]+Z">` + regexp.QuoteMeta(body("On it")) + `</p188:reply>` +
		`<p188:reply id="\{CCCCCCCC-0000-4000-8000-000000000001\}" authorId="(\{[0-9A-F-]{36}\})" created="[^"]+Z">` + regexp.QuoteMeta(body("Done")) + `</p188:reply></p188:replyLst>` +
		regexp.QuoteMeta(body("Is this title final?")) + `</p188:cm>`).MatchString(cm) {
		t.Fatalf("comment part: %s", cm)
	}
	if strings.Contains(cm, `status="resolved"><pc:sldMkLst`) || !strings.Contains(cm, `<p188:cm id="`+slideThread+`" authorId="`+me+`" created="2026-11-03T09:00:00.000Z"><pc:sldMkLst`) {
		t.Fatalf("slide thread still resolved: %s", cm)
	}
	// The agent is one author, named by its whole "agent:<id>".
	if n := strings.Count(parts["ppt/authors.xml"], `userId="agent:p-7" providerId="WhatIff"`); n != 1 {
		t.Fatalf("authors: %s", parts["ppt/authors.xml"])
	}

	threads, err := New().List(out)
	if err != nil {
		t.Fatal(err)
	}
	th := threads[0]
	if th.ID != shapeThread || !th.Resolved || len(th.Comments) != 3 || th.Comments[1].AuthorID != "agent:p-7" || th.Comments[2].ID != "{CCCCCCCC-0000-4000-8000-000000000001}" || th.Comments[2].Text != "Done" {
		t.Fatalf("read back %+v", th)
	}
	if threads[1].Resolved {
		t.Fatal("slide thread still resolved")
	}

	// Resolving what is resolved changes nothing.
	again, err := New().Resolve(out, shapeThread, true)
	if err != nil || !bytes.Equal(again, out) {
		t.Fatalf("resolve again: %v", err)
	}
	if _, err := New().Reply(data, "{DEADBEEF-0000-4000-8000-000000000000}", reply); !errors.Is(err, officecomments.ErrNoThread) {
		t.Fatalf("err = %v", err)
	}
	if _, err := New().Resolve(data, "nope", true); !errors.Is(err, officecomments.ErrNoThread) {
		t.Fatalf("err = %v", err)
	}
}

func TestReplyToLegacyThreadWritesTheSlideModern(t *testing.T) {
	data := build(t, commentedParts())
	out, err := New().Reply(data, legacyID, officecomments.Comment{AuthorID: "user:u-42", AuthorName: "Chris Writer", Text: "Agreed"})
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := entries(t, out)
	if _, ok := parts["ppt/comments/comment1.xml"]; ok {
		t.Fatal("legacy part kept")
	}
	if strings.Contains(parts["[Content_Types].xml"], "comment1.xml") || strings.Contains(parts["ppt/slides/_rels/slide2.xml.rels"], "comment1.xml") {
		t.Fatal("legacy part still named")
	}
	cm := parts["ppt/comments/modernComment_101_0.xml"]
	if !strings.Contains(cm, `<p188:cm id="`+legacyID+`" authorId="{`) || !strings.Contains(cm, `<p188:pos x="127000" y="254000"/><p188:replyLst><p188:reply id="{00000000-0000-4000-8000-000100000001}"`) {
		t.Fatalf("comment part: %s", cm)
	}
	authors := parts["ppt/authors.xml"]
	for _, want := range []string{`name="Kim Lee" initials="KL" userId="Kim Lee" providerId="None"`, `name="Ola Berg" initials="OB" userId="Ola Berg" providerId="None"`} {
		if !strings.Contains(authors, want) {
			t.Fatalf("authors lack %s: %s", want, authors)
		}
	}
	if strings.Count(authors, `name="Chris Writer"`) != 1 {
		t.Fatalf("authors: %s", authors)
	}
	threads, err := New().List(out)
	if err != nil {
		t.Fatal(err)
	}
	last := threads[len(threads)-1]
	var got []string
	for _, c := range last.Comments {
		got = append(got, c.AuthorID+": "+c.Text)
	}
	if last.ID != legacyID || strings.Join(got, "|") != "author:Kim Lee: Cut this slide?|author:Ola Berg: No, keep it.|user:u-42: Agreed" {
		t.Fatalf("read back %s %v", last.ID, got)
	}
}

func TestHostileInput(t *testing.T) {
	// Pasted control characters are dropped, so the slide's comment part stays one Office opens,
	// and a second comment goes into the same part rather than a new one beside a broken one.
	agent := officecomments.Comment{AuthorID: "agent:p-1", AuthorName: "Cl\faude", Text: "see \v here"}
	out, _, err := New().Add(build(t, basicParts()), officecomments.Anchor{Quote: "plan", Slide: 1}, agent)
	if err != nil {
		t.Fatal(err)
	}
	if err := officecommentstest.WellFormed(out); err != nil {
		t.Fatal(err)
	}
	out, _, err = New().Add(out, officecomments.Anchor{Quote: "Quarterly", Slide: 1}, agent)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := entries(t, out)
	n := 0
	for name := range files {
		if strings.HasPrefix(name, "ppt/comments/") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d comment parts", n)
	}

	// A slide with no shape tree has no shapes, rather than a nil pointer.
	parts := basicParts()
	parts[4].body = head + `<p:sld xmlns:a="` + nsA + `" xmlns:r="` + nsR + `" xmlns:p="` + nsP + `"/>`
	if _, err := New().List(build(t, parts)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := New().Add(build(t, parts), officecomments.Anchor{Quote: "Budget"}, agent); err != nil {
		t.Fatal(err)
	}

	// A broken comment part is an error, not a missing part to write again beside it.
	broken := commentedParts()
	for i := range broken {
		if strings.HasPrefix(broken[i].name, "ppt/comments/modernComment") {
			broken[i].body = head + "<p188:cmLst>\v</p188:cmLst>"
		}
	}
	if _, _, err := New().Add(build(t, broken), officecomments.Anchor{Quote: "Quarterly", Slide: 1}, agent); !errors.Is(err, officecomments.ErrNotOffice) {
		t.Fatalf("broken part: %v", err)
	}
}
