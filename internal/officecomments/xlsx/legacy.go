package xlsx

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Excel keeps a legacy note for each thread, with placeholder text for older versions and an
// author of "tc={id}" tying it to the thread, and a VML shape for the note. The browser writes
// them with ExcelJS and xlsx-threaded-comments.ts; these write the same.

const (
	relComments   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments"
	relVML        = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/vmlDrawing"
	ctComments    = "application/vnd.openxmlformats-officedocument.spreadsheetml.comments+xml"
	ctVML         = "application/vnd.openxmlformats-officedocument.vmlDrawing"
	nsRevision    = "http://schemas.microsoft.com/office/spreadsheetml/2014/revision"
	nsMC          = "http://schemas.openxmlformats.org/markup-compatibility/2006"
	nsOfficeRels  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	excelJSHead   = xmlHead + "\n"
	revisionAttrs = ` xmlns:mc="` + nsMC + `" mc:Ignorable="xr" xmlns:xr="` + nsRevision + `"`
)

// legacyText is the placeholder Excel writes into a thread's legacy note.
func legacyText(texts []string) string {
	replies := make([]string, 0, len(texts)-1)
	for _, t := range texts[1:] {
		replies = append(replies, "\nReply:\n    "+t)
	}
	return "[Threaded comment]\n\nYour version of Excel allows you to read this threaded comment; however, any edits to it will get removed if the file is opened in a newer version of Excel. Learn more: https://go.microsoft.com/fwlink/?linkid=870924\n\n" +
		"Comment:\n    " + texts[0] + "\n" + strings.Join(replies, "\n")
}

// noteTextXML is a note's <text> as ExcelJS writes a plain-text note.
func noteTextXML(pfx, text string) string {
	space := ""
	if regexp.MustCompile(`^\s|\n|\s$`).MatchString(text) {
		space = ` xml:space="preserve"`
	}
	return `<` + pfx + `text><` + pfx + `r><` + pfx + `t` + space + `>` + escapeXML(text) + `</` + pfx + `t></` + pfx + `r></` + pfx + `text>`
}

func sameCell(a, b string) bool {
	ra, ca, okA := parseCell(a)
	rb, cb, okB := parseCell(b)
	return okA && okB && ra == rb && ca == cb
}

// legacyPath is the sheet's legacy comments part, or "".
func (b *book) legacyPath(s sheet) string {
	return b.p.relOf(s.path, relComments)
}

// hasNote tells whether the sheet has a legacy note on the cell.
func (b *book) hasNote(s sheet, ref string) bool {
	data, ok := b.p.read(b.legacyPath(s))
	if !ok {
		return false
	}
	root, err := scan(data, 2)
	if err != nil {
		return false
	}
	if list := root.kid("commentList"); list != nil {
		for _, c := range list.kids("comment") {
			if sameCell(c.get("ref"), ref) {
				return true
			}
		}
	}
	return false
}

// putLegacy writes the thread's legacy note on its cell, adding it (and its shape) when missing,
// else bringing its text up to date.
func (b *book) putLegacy(s sheet, ref, threadID, text string) {
	commentsPath := b.legacyPath(s)
	data, ok := b.p.read(commentsPath)
	if commentsPath == "" || !ok {
		commentsPath = b.p.unused(path.Join(path.Dir(b.wbPath), "comments%d.xml"))
		b.p.put(commentsPath, []byte(excelJSHead+`<comments`+revisionAttrs+` xmlns="`+nsMain+`"><authors><author>Author</author><author>tc=`+threadID+`</author></authors><commentList>`+
			`<comment ref="`+escapeXML(ref)+`" authorId="1" xr:uid="`+threadID+`">`+noteTextXML("", text)+`</comment></commentList></comments>`))
		b.p.addOverride(commentsPath, ctComments)
		b.p.addRel(s.path, relComments, commentsPath)
		b.putShape(s, ref)
		return
	}
	root, err := scan(data, 3)
	if err != nil {
		return
	}
	pfx := prefix(data, root)
	list := root.kid("commentList")
	if list != nil {
		for _, c := range list.kids("comment") {
			if !sameCell(c.get("ref"), ref) {
				continue
			}
			if t := c.kid("text"); t != nil {
				b.p.put(commentsPath, apply(data, []edit{{t.start, t.end, noteTextXML(pfx, text)}}))
			} else {
				b.p.put(commentsPath, apply(data, []edit{appendChild(data, c, noteTextXML(pfx, text))}))
			}
			b.putShape(s, ref)
			return
		}
	}
	var edits []edit
	tag := string(data[root.start:root.tagEnd])
	uid := ""
	if strings.Contains(tag, `xmlns:xr="`+nsRevision+`"`) {
		uid = ` xr:uid="` + threadID + `"`
	} else if ignorable := regexp.MustCompile(`\smc:Ignorable="([^"]*)"`); ignorable.MatchString(tag) || strings.Contains(tag, `xmlns:mc="`) {
		if ignorable.MatchString(tag) {
			tag = ignorable.ReplaceAllString(tag, ` mc:Ignorable="$1 xr"`)
		} else {
			tag = strings.Replace(tag, `xmlns:mc="`, `mc:Ignorable="xr" xmlns:mc="`, 1)
		}
		tag = "<" + qname(data, root) + ` xmlns:xr="` + nsRevision + `"` + strings.TrimPrefix(tag, "<"+qname(data, root))
		edits = append(edits, edit{root.start, root.tagEnd, tag})
		uid = ` xr:uid="` + threadID + `"`
	} else {
		edits = append(edits, edit{root.start, root.tagEnd, "<" + qname(data, root) + revisionAttrs + strings.TrimPrefix(tag, "<"+qname(data, root))})
		uid = ` xr:uid="` + threadID + `"`
	}
	author := `<` + pfx + `author>tc=` + threadID + `</` + pfx + `author>`
	authorID := 0
	authors := root.kid("authors")
	if authors != nil {
		authorID = len(authors.kids("author"))
		edits = append(edits, appendChild(data, authors, author))
	} else {
		edits = append(edits, edit{root.tagEnd, root.tagEnd, `<` + pfx + `authors>` + author + `</` + pfx + `authors>`})
	}
	comment := `<` + pfx + `comment ref="` + escapeXML(ref) + `" authorId="` + strconv.Itoa(authorID) + `"` + uid + `>` + noteTextXML(pfx, text) + `</` + pfx + `comment>`
	if list != nil {
		edits = append(edits, appendChild(data, list, comment))
	} else {
		at := root.tagEnd
		if authors != nil {
			at = authors.end
		}
		edits = append(edits, edit{at, at, `<` + pfx + `commentList>` + comment + `</` + pfx + `commentList>`})
	}
	b.p.put(commentsPath, apply(data, edits))
	b.putShape(s, ref)
}

const vmlShapeType = `<v:shapetype id="_x0000_t202" coordsize="21600,21600" o:spt="202" path="m,l,21600r21600,l21600,xe"><v:stroke joinstyle="miter"/><v:path gradientshapeok="t" o:connecttype="rect"/></v:shapetype>`

// vmlShape is a note's shape as ExcelJS writes it, with its default anchor.
func vmlShape(id, row, col int) string {
	top := row - 1
	if top < 0 {
		top = 0
	}
	anchor := fmt.Sprintf("%d, 6, %d, 14, %d, 2, %d, 16", col+1, top, col+3, top+4)
	return fmt.Sprintf(`<v:shape id="_x0000_s%d" type="#_x0000_t202" style="position:absolute; margin-left:105.3pt;margin-top:10.5pt;width:97.8pt;height:59.1pt;z-index:1;visibility:hidden" fillcolor="infoBackground [80]" strokecolor="none [81]" o:insetmode="auto"><v:fill color2="infoBackground [80]"/><v:shadow color="none [81]" obscured="t"/><v:path o:connecttype="none"/><v:textbox style="mso-direction-alt:auto" inset="1.3mm,1.3mm,2.5mm,2.5mm"><div style="text-align:left"/></v:textbox><x:ClientData ObjectType="Note"><x:MoveWithCells/><x:SizeWithCells/><x:Anchor>%s</x:Anchor><x:Locked>True</x:Locked><x:AutoFill>False</x:AutoFill><x:LockText>True</x:LockText><x:Row>%d</x:Row><x:Column>%d</x:Column></x:ClientData></v:shape>`, id, anchor, row, col)
}

var (
	vmlShapeRe   = regexp.MustCompile(`(?s)<v:shape\b.*?</v:shape>`)
	vmlShapeIDRe = regexp.MustCompile(`_x0000_s(\d+)`)
	vmlIDMapRe   = regexp.MustCompile(`<o:idmap\b[^>]*\bdata="(\d+)`)
	vmlRowRe     = regexp.MustCompile(`<x:Row>\s*(\d+)\s*</x:Row>`)
	vmlColRe     = regexp.MustCompile(`<x:Column>\s*(\d+)\s*</x:Column>`)
	rPrefixRe    = regexp.MustCompile(`xmlns:(\w+)="` + regexp.QuoteMeta(nsOfficeRels) + `"`)
)

// afterLegacyDrawing is what follows <legacyDrawing> in a worksheet.
var afterLegacyDrawing = map[string]bool{"legacyDrawingHF": true, "drawingHF": true, "picture": true, "oleObjects": true, "controls": true, "webPublishItems": true, "tableParts": true, "extLst": true}

// putShape makes sure the sheet's VML drawing has a note shape on the cell.
func (b *book) putShape(s sheet, ref string) {
	row, col, ok := parseCell(ref)
	if !ok {
		return
	}
	sheetXML, ok := b.p.read(s.path)
	if !ok {
		return
	}
	root, err := scan(sheetXML, 1)
	if err != nil {
		return
	}
	vmlPath := ""
	if ld := root.kid("legacyDrawing"); ld != nil {
		vmlPath = b.p.relByID(s.path, ld.relID())
	}
	vml, ok := b.p.read(vmlPath)
	if vmlPath == "" || !ok {
		vmlPath = b.p.unused(path.Join(path.Dir(b.wbPath), "drawings/vmlDrawing%d.vml"))
		b.p.put(vmlPath, []byte(excelJSHead+`<xml xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel"><o:shapelayout v:ext="edit"><o:idmap v:ext="edit" data="1"/></o:shapelayout>`+vmlShapeType+vmlShape(1025, row, col)+`</xml>`))
		b.p.addDefault("vml", ctVML)
		id := b.p.addRel(s.path, relVML, vmlPath)
		if root.kid("legacyDrawing") != nil {
			// A legacyDrawing whose drawing is missing points at the new one.
			ld := root.kid("legacyDrawing")
			tag := regexp.MustCompile(`(:id\s*=\s*)("[^"]*"|'[^']*')`).ReplaceAllString(string(sheetXML[ld.start:ld.tagEnd]), `${1}"`+id+`"`)
			b.p.put(s.path, apply(sheetXML, []edit{{ld.start, ld.tagEnd, tag}}))
			return
		}
		pfx := prefix(sheetXML, root)
		el := ""
		if m := rPrefixRe.FindSubmatch(sheetXML[root.start:root.tagEnd]); m != nil {
			el = `<` + pfx + `legacyDrawing ` + string(m[1]) + `:id="` + id + `"/>`
		} else {
			el = `<` + pfx + `legacyDrawing xmlns:r="` + nsOfficeRels + `" r:id="` + id + `"/>`
		}
		at := -1
		for _, c := range root.children {
			if afterLegacyDrawing[c.name.Local] {
				at = c.start
				break
			}
		}
		if at < 0 {
			b.p.put(s.path, apply(sheetXML, []edit{appendChild(sheetXML, root, el)}))
		} else {
			b.p.put(s.path, apply(sheetXML, []edit{{at, at, el}}))
		}
		return
	}
	text := string(vml)
	next := 0
	for _, shape := range vmlShapeRe.FindAllString(text, -1) {
		r, c := vmlRowRe.FindStringSubmatch(shape), vmlColRe.FindStringSubmatch(shape)
		if strings.Contains(shape, `ObjectType="Note"`) && r != nil && c != nil && r[1] == strconv.Itoa(row) && c[1] == strconv.Itoa(col) {
			return
		}
	}
	for _, m := range vmlShapeIDRe.FindAllStringSubmatch(text, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= next {
			next = n + 1
		}
	}
	if next == 0 {
		next = 1025
		if m := vmlIDMapRe.FindStringSubmatch(text); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
				next = n*1024 + 1
			}
		}
	}
	add := vmlShape(next, row, col)
	if !strings.Contains(text, `id="_x0000_t202"`) {
		add = vmlShapeType + add
	}
	end := strings.LastIndex(text, "</xml>")
	if end < 0 {
		return
	}
	b.p.put(vmlPath, []byte(text[:end]+add+text[end:]))
}
