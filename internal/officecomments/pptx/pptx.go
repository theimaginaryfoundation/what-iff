// Package pptx reads and writes the comments of PowerPoint files (ADR 0x045 §2, ADR 0x046), as the
// browser's slides editor does (web/app/src/app/features/office/pptx/pptx-comments.ts).
//
// PowerPoint's "modern" comments (the p188 namespace) are one part per slide: a cm per thread with
// its replies, anchored to the slide, to a shape on it or to a range of a shape's text, with the
// people in one authors part. Older "legacy" comments are read too; a slide whose comments change
// has them written in the modern form, as the browser does. Changes are made to the XML text, so
// everything else in the file keeps its bytes.
package pptx

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

// Format is PowerPoint's comments.
type Format struct{}

// New returns the PowerPoint comments format.
func New() Format { return Format{} }

var _ officecomments.Format = Format{}

// thread is a thread as read, with where it is in the file.
type thread struct {
	officecomments.Thread
	slide *slide
	// cm is the modern comment element, nil for a legacy thread.
	cm *node
	// pos is the pin's offset in points from the slide's top-left, for a thread on no shape.
	pos     [2]float64
	onShape bool
}

// slideThreads reads the threads of one slide, in the order of its comment relationships.
func (d *deck) slideThreads(sl *slide) []thread {
	slideRoot, _ := d.p.xml(sl.path)
	if slideRoot == nil {
		return nil
	}
	list := shapes(slideRoot)
	var out []thread
	for _, r := range d.p.rels(sl.path) {
		if r.External {
			continue
		}
		switch r.Type {
		case relModernComments:
			root, _ := d.p.xml(resolveTarget(sl.path, r.Target))
			for _, cm := range root.kids(nsP188, "cm") {
				out = append(out, d.modernThread(sl, list, cm))
			}
		case relLegacyComments:
			root, _ := d.p.xml(resolveTarget(sl.path, r.Target))
			out = append(out, d.legacyThreads(sl, root)...)
		}
	}
	return out
}

func (d *deck) modernThread(sl *slide, list []shape, cm *node) thread {
	comment := func(el *node) officecomments.Comment {
		id, name := person(d.authorByID(el.attr("authorId")), "")
		return officecomments.Comment{ID: el.attr("id"), AuthorID: id, AuthorName: name,
			CreatedAt: parseTime(el.attr("created")), Text: bodyText(el.kid(nsP188, "txBody"))}
	}
	t := thread{slide: sl, cm: cm}
	t.ID = cm.attr("id")
	t.Comments = append(t.Comments, comment(cm))
	for _, r := range cm.kid(nsP188, "replyLst").kids(nsP188, "reply") {
		t.Comments = append(t.Comments, comment(r))
	}
	status := cm.attr("status")
	t.Resolved = status == "resolved" || status == "closed"

	// A shape anchor names the shape by its id on the slide; a text anchor names it the same way,
	// then the range of its text.
	monikers := cm.kid(nsAC, "deMkLst")
	if monikers == nil {
		monikers = cm.kid(nsAC, "txMkLst")
	}
	var shapeMk, txMk *node
	if monikers != nil {
		monikers.walk(func(n *node) {
			if shapeMk == nil && n.Name.Space == nsAC && strings.HasSuffix(n.Name.Local, "Mk") && n.attr("id") != "" {
				shapeMk = n
			}
		})
		if monikers.Name.Local == "txMkLst" {
			txMk = monikers.first(nsAC, "txMk")
		}
	}
	var sh *shape
	if shapeMk != nil {
		sh = shapeByID(list, shapeMk.attr("id"))
	}
	if pos := cm.kid(nsP188, "pos"); pos != nil {
		x, _ := strconv.ParseFloat(pos.attr("x"), 64)
		y, _ := strconv.ParseFloat(pos.attr("y"), 64)
		t.pos = [2]float64{x / emuPerPt, y / emuPerPt}
	}
	t.onShape = sh != nil
	t.Where = fmt.Sprintf("slide %d", sl.num)
	if sh == nil {
		t.On = fmt.Sprintf("Slide %d", sl.num)
		return t
	}
	if sh.name != "" {
		t.Where += fmt.Sprintf(", shape %q", sh.name)
	}
	t.On = sh.name
	if txMk != nil {
		cp := atoi(txMk.attr("cp"))
		n := atoi(txMk.attr("len"))
		if cp >= 0 && n > 0 {
			if words := strings.TrimSpace(utf16Slice(sh.text(), cp, n)); words != "" {
				t.On = words
			}
		}
	}
	if t.On == "" {
		t.On = fmt.Sprintf("Slide %d", sl.num)
	}
	return t
}

func atoi(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}

func (d *deck) legacyThreads(sl *slide, root *node) []thread {
	var roots []*thread
	byID := map[string]*thread{}
	type reply struct {
		parent string
		c      officecomments.Comment
	}
	var replies []reply
	for _, cm := range root.kids(nsP, "cm") {
		authorID, idx := cm.attr("authorId"), cm.attr("idx")
		if authorID == "" {
			authorID = "0"
		}
		if idx == "" {
			idx = "0"
		}
		id, name := person(nil, d.legacyNames[authorID])
		var text string
		if t := cm.kid(nsP, "text"); t != nil {
			text = t.Text
		}
		c := officecomments.Comment{ID: legacyGUID(authorID, idx), AuthorID: id, AuthorName: name,
			CreatedAt: parseTime(cm.attr("dt")), Text: text}
		if parent := cm.first(nsP15, "parentCm"); parent != nil {
			pa, pi := parent.attr("authorId"), parent.attr("idx")
			if pa == "" {
				pa = "0"
			}
			if pi == "" {
				pi = "0"
			}
			replies = append(replies, reply{legacyGUID(pa, pi), c})
			continue
		}
		t := &thread{slide: sl}
		t.ID = c.ID
		t.On = fmt.Sprintf("Slide %d", sl.num)
		t.Where = fmt.Sprintf("slide %d", sl.num)
		t.Comments = []officecomments.Comment{c}
		if pos := cm.kid(nsP, "pos"); pos != nil {
			x, _ := strconv.ParseFloat(pos.attr("x"), 64)
			y, _ := strconv.ParseFloat(pos.attr("y"), 64)
			t.pos = [2]float64{x / legacyUnitsPerPt, y / legacyUnitsPerPt}
		}
		if _, dup := byID[t.ID]; !dup {
			roots = append(roots, t)
		}
		byID[t.ID] = t
	}
	for _, r := range replies {
		if t := byID[r.parent]; t != nil {
			t.Comments = append(t.Comments, r.c)
		}
	}
	out := make([]thread, 0, len(roots))
	for _, t := range roots {
		out = append(out, *byID[t.ID])
	}
	return out
}

func (d *deck) threads() []thread {
	var out []thread
	for _, sl := range d.slides {
		out = append(out, d.slideThreads(sl)...)
	}
	return out
}

func (d *deck) find(id string) *thread {
	all := d.threads()
	for i := range all {
		if all[i].ID == id {
			return &all[i]
		}
	}
	for i := range all {
		if strings.EqualFold(all[i].ID, id) {
			return &all[i]
		}
	}
	return nil
}

// List returns every thread of the file, slide by slide.
func (Format) List(data []byte) ([]officecomments.Thread, error) {
	d, err := openDeck(data)
	if err != nil {
		return nil, err
	}
	out := []officecomments.Thread{}
	for _, t := range d.threads() {
		out = append(out, t.Thread)
	}
	return out, nil
}

// Reply adds a comment to the end of a thread.
func (Format) Reply(data []byte, threadID string, c officecomments.Comment) ([]byte, error) {
	d, err := openDeck(data)
	if err != nil {
		return nil, err
	}
	t := d.find(threadID)
	if t == nil {
		return nil, officecomments.ErrNoThread
	}
	if t.cm == nil {
		if t = d.modernize(t.slide, t.ID); t == nil {
			return nil, officecomments.ErrNoThread
		}
	}
	partPath, root, part := d.modernPart(t.slide)
	cm := findCM(root, t.ID)
	if cm == nil {
		return nil, officecomments.ErrNoThread
	}
	ns := newNames(cm)
	p188, a := ns.prefix(nsP188, "p188"), ns.prefix(nsA, "a")
	reply := d.commentXML(p188, a, "reply", c, "")
	var e edit
	if lst := cm.kid(nsP188, "replyLst"); lst != nil {
		e = appendChild(part, lst, withDecls(reply, ns.decls()))
	} else {
		lstXML := withDecls("<"+p188+":replyLst>"+reply+"</"+p188+":replyLst>", ns.decls())
		if body := cm.kid(nsP188, "txBody"); body != nil {
			e = edit{Start: body.Start, End: body.Start, Text: lstXML}
		} else if ext := cm.kid(nsP188, "extLst"); ext != nil {
			e = edit{Start: ext.Start, End: ext.Start, Text: lstXML}
		} else {
			e = appendChild(part, cm, lstXML)
		}
	}
	d.p.put(partPath, applyEdits(part, []edit{e}))
	d.writeAuthors()
	return d.p.save()
}

// Resolve marks a thread resolved, or open again.
func (Format) Resolve(data []byte, threadID string, resolved bool) ([]byte, error) {
	d, err := openDeck(data)
	if err != nil {
		return nil, err
	}
	t := d.find(threadID)
	if t == nil {
		return nil, officecomments.ErrNoThread
	}
	if t.Resolved == resolved {
		return data, nil
	}
	if t.cm == nil {
		if t = d.modernize(t.slide, t.ID); t == nil {
			return nil, officecomments.ErrNoThread
		}
	}
	partPath, root, part := d.modernPart(t.slide)
	cm := findCM(root, t.ID)
	if cm == nil {
		return nil, officecomments.ErrNoThread
	}
	var value *string
	if resolved {
		v := "resolved"
		value = &v
	}
	d.p.put(partPath, applyEdits(part, []edit{setAttr(part, cm, "status", value)}))
	d.writeAuthors()
	return d.p.save()
}

func findCM(root *node, id string) *node {
	for _, cm := range root.kids(nsP188, "cm") {
		if cm.attr("id") == id {
			return cm
		}
	}
	for _, cm := range root.kids(nsP188, "cm") {
		if strings.EqualFold(cm.attr("id"), id) {
			return cm
		}
	}
	return nil
}

// withDecls puts namespace declarations on the first element of an XML fragment.
func withDecls(fragment, decls string) string {
	if decls == "" {
		return fragment
	}
	end := strings.IndexAny(fragment, " />")
	return fragment[:end] + decls + fragment[end:]
}

// commentXML is a comment (cm or reply) as the browser writes it; inner goes before its text.
func (d *deck) commentXML(p188, a, local string, c officecomments.Comment, inner string) string {
	id := c.ID
	if !isGUID(id) {
		id = newGUID()
	}
	created := c.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	authorID := d.authorFor(c.AuthorID, c.AuthorName)
	tag := qualify(p188, local)
	s := "<" + tag + ` id="` + escapeXML(id) + `" authorId="` + escapeXML(authorID) + `" created="` + isoTime(created) + `"`
	return s + ">" + inner + commentBody(a, p188, c.Text) + "</" + tag + ">"
}

// modernPart is a slide's modern comment part, if it has one.
func (d *deck) modernPart(sl *slide) (string, *node, []byte) {
	path := d.p.relTarget(sl.path, relModernComments)
	if path == "" {
		return "", nil, nil
	}
	root, data := d.p.xml(path)
	return path, root, data
}

// slideMk is the slide moniker of a slide, as the browser writes it.
func (d *deck) slideMk(sl *slide, slideRoot *node) string {
	cID := "0"
	if c := slideRoot.first(nsP14, "creationId"); c != nil {
		if _, ok := c.lookupAttr("val"); ok {
			cID = c.attr("val")
		}
	}
	return `<pc:docMk/><pc:sldMk cId="` + escapeXML(cID) + `" sldId="` + escapeXML(sl.sldID) + `"/>`
}

func (d *deck) slideCreationID(slideRoot *node) uint64 {
	if c := slideRoot.first(nsP14, "creationId"); c != nil {
		n, _ := strconv.ParseUint(c.attr("val"), 10, 64)
		return n
	}
	return 0
}

// addCMs adds comments (cm elements written with the p188 and a prefixes) to the end of a slide's
// modern comment part, making the part, its relationship, its content type and the slide's link
// to it when the slide has none. slideEdits are other changes to the slide's XML.
func (d *deck) addCMs(sl *slide, build func(p188, a string) string, slideEdits []edit) {
	slideRoot, slideData := d.p.xml(sl.path)
	if path, root, data := d.modernPart(sl); root != nil {
		ns := newNames(root)
		p188, a := ns.prefix(nsP188, "p188"), ns.prefix(nsA, "a")
		cms := build(p188, a)
		if decls := ns.decls(); decls != "" {
			// Declared on the list's root, as the browser writes it.
			d.p.put(path, applyEdits(data, []edit{appendChild(data, root, cms), {Start: root.Start + 1 + len(qname(data, root)), End: root.Start + 1 + len(qname(data, root)), Text: decls}}))
		} else {
			d.p.put(path, applyEdits(data, []edit{appendChild(data, root, cms)}))
		}
		if len(slideEdits) > 0 {
			d.p.put(sl.path, applyEdits(slideData, slideEdits))
		}
		return
	}
	cms := build("p188", "a")
	base := fmt.Sprintf("ppt/comments/modernComment_%X_%X", parseUintOr0(sl.sldID), d.slideCreationID(slideRoot))
	path := base + ".xml"
	for n := 2; d.p.has(path); n++ {
		path = fmt.Sprintf("%s_%d.xml", base, n)
	}
	d.p.put(path, []byte(xmlDeclaration+`<p188:cmLst xmlns:a="`+nsA+`" xmlns:r="`+nsR+`" xmlns:p188="`+nsP188+`">`+cms+`</p188:cmLst>`))
	id := d.p.addRel(sl.path, relModernComments, relativeTarget(sl.path, path))
	d.p.addOverride(path, ctModernComments)
	d.p.put(sl.path, applyEdits(slideData, append(slideEdits, commentRelEdits(slideData, slideRoot, id)...)))
}

func parseUintOr0(s string) uint64 {
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}

// commentRelEdits link a slide to its comment part: the p188:commentRel extension, the last child
// of p:sld, in place of any link it had.
func commentRelEdits(data []byte, sld *node, relID string) []edit {
	ns := newNames(sld)
	p := ns.prefix(nsP, "p")
	r, rDecl := "r", ` xmlns:r="`+nsR+`"`
	if pr, ok := sld.prefix(nsR); ok && pr != "" {
		r, rDecl = pr, ""
	}
	ext := `<` + p + `:ext uri="` + commentRelExt + `"><p188:commentRel xmlns:p188="` + nsP188 + `"` + rDecl + ` ` + r + `:id="` + relID + `"/></` + p + `:ext>`
	extLst := sld.kid(nsP, "extLst")
	if extLst == nil {
		return []edit{appendChild(data, sld, withDecls("<"+p+":extLst>"+ext+"</"+p+":extLst>", ns.decls()))}
	}
	var edits []edit
	for _, e := range extLst.kids(nsP, "ext") {
		if e.attr("uri") == commentRelExt {
			edits = append(edits, edit{Start: e.Start, End: e.End})
		}
	}
	return append(edits, appendChild(data, extLst, ext))
}
