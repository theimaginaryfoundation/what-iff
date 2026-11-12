package pptx

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

const (
	nsP    = "http://schemas.openxmlformats.org/presentationml/2006/main"
	nsA    = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsP14  = "http://schemas.microsoft.com/office/powerpoint/2010/main"
	nsP15  = "http://schemas.microsoft.com/office/powerpoint/2012/main"
	nsP188 = "http://schemas.microsoft.com/office/powerpoint/2018/8/main"
	nsPC   = "http://schemas.microsoft.com/office/powerpoint/2013/main/command"
	nsAC   = "http://schemas.microsoft.com/office/drawing/2013/main/command"
	nsA16  = "http://schemas.microsoft.com/office/drawing/2014/main"

	relModernComments = "http://schemas.microsoft.com/office/2018/10/relationships/comments"
	relAuthors        = "http://schemas.microsoft.com/office/2018/10/relationships/authors"
	relLegacyComments = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments"
	relLegacyAuthors  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/commentAuthors"
	ctModernComments  = "application/vnd.ms-powerpoint.comments+xml"
	ctAuthors         = "application/vnd.ms-powerpoint.authors+xml"

	// commentRelExt is the slide extension that links a slide to its modern comment part.
	commentRelExt = "{6950BFC3-D8DA-4A85-94F7-54DA5524770B}"
	// creationIDExt is the a:ext that carries a shape's creation id.
	creationIDExt = "{FF2B5EF4-FFF2-40B4-BE49-F238E27FC236}"

	emuPerPt = 12700
	// Legacy comment positions are in PowerPoint's master units, 8 per point.
	legacyUnitsPerPt = 8
)

// The moniker that names a shape on a slide, by the shape's element.
var monikerByNode = map[string]string{
	"sp":           "spMk",
	"pic":          "picMk",
	"cxnSp":        "cxnSpMk",
	"grpSp":        "grpSpMk",
	"graphicFrame": "graphicFrameMk",
}

// deck is an open presentation: its slides in order and the comment authors it names.
type deck struct {
	p           *pkg
	presPath    string
	slides      []*slide
	authorsPath string
	authors     []author
	newAuthors  []author
	legacyNames map[string]string
}

type slide struct {
	num   int
	path  string
	sldID string
}

type author struct {
	id, name, initials, userID, providerID string
	hasInitials                            bool
}

func openDeck(data []byte) (*deck, error) {
	p, err := openPkg(data)
	if err != nil {
		return nil, err
	}
	d := &deck{p: p, presPath: p.relTarget("", relOfficeDoc), legacyNames: map[string]string{}}
	pres, _ := p.xml(d.presPath)
	if d.presPath == "" || !pres.is(nsP, "presentation") {
		return nil, officecomments.ErrNotOffice
	}
	presRels := map[string]rel{}
	for _, r := range p.rels(d.presPath) {
		presRels[r.ID] = r
	}
	for _, s := range pres.kid(nsP, "sldIdLst").kids(nsP, "sldId") {
		r, ok := presRels[s.attrNS(nsR, "id")]
		if !ok || r.External || r.Type != relSlide {
			continue
		}
		d.slides = append(d.slides, &slide{num: len(d.slides) + 1, path: resolveTarget(d.presPath, r.Target), sldID: s.attr("id")})
	}
	d.authorsPath = p.relTarget(d.presPath, relAuthors)
	if d.authorsPath != "" {
		root, _ := p.xml(d.authorsPath)
		for _, a := range root.kids(nsP188, "author") {
			in, has := a.lookupAttr("initials")
			d.authors = append(d.authors, author{id: a.attr("id"), name: a.attr("name"), initials: in, hasInitials: has,
				userID: a.attr("userId"), providerID: a.attr("providerId")})
		}
	}
	if path := p.relTarget(d.presPath, relLegacyAuthors); path != "" {
		root, _ := p.xml(path)
		for _, a := range root.kids(nsP, "cmAuthor") {
			d.legacyNames[a.attr("id")] = a.attr("name")
		}
	}
	return d, nil
}

// person is the panel's id and name for a comment author.
func person(a *author, fallbackName string) (string, string) {
	if a != nil && a.providerID == officecomments.WhatIffProvider && a.userID != "" {
		return officecomments.PersonID(a.providerID, a.userID, a.name), a.name
	}
	name := fallbackName
	if a != nil && a.name != "" {
		name = a.name
	}
	if name == "" {
		name = "Unknown"
	}
	return officecomments.PersonID("", "", name), name
}

func (d *deck) authorByID(id string) *author {
	for i := range d.authors {
		if strings.EqualFold(d.authors[i].id, id) {
			return &d.authors[i]
		}
	}
	return nil
}

// authorFor is the author id for a comment's author, adding them when the file has no such author:
// a WhatIff person by their user id, anyone else by name.
func (d *deck) authorFor(personID, name string) string {
	userID := officecomments.ProviderUserID(personID)
	for _, list := range [][]author{d.authors, d.newAuthors} {
		for _, a := range list {
			if userID != "" && a.providerID == officecomments.WhatIffProvider && a.userID == userID {
				return a.id
			}
			if userID == "" && a.name == name && a.providerID != officecomments.WhatIffProvider {
				return a.id
			}
		}
	}
	a := author{id: newGUID(), name: name, initials: initials(name), hasInitials: true, userID: name, providerID: "None"}
	if userID != "" {
		a.userID, a.providerID = userID, officecomments.WhatIffProvider
	}
	d.newAuthors = append(d.newAuthors, a)
	return a.id
}

func authorXML(prefix string, a author) string {
	s := "<" + qualify(prefix, "author") + ` id="` + escapeXML(a.id) + `" name="` + escapeXML(a.name) + `"`
	if a.hasInitials && a.initials != "" {
		s += ` initials="` + escapeXML(a.initials) + `"`
	}
	return s + ` userId="` + escapeXML(a.userID) + `" providerId="` + escapeXML(a.providerID) + `"/>`
}

// writeAuthors adds the authors new in this change to the authors part, making it if need be.
func (d *deck) writeAuthors() {
	if len(d.newAuthors) == 0 {
		return
	}
	if d.authorsPath != "" {
		if root, data := d.p.xml(d.authorsPath); root != nil {
			ns := newNames(root)
			prefix := ns.prefix(nsP188, "p188")
			var b strings.Builder
			for _, a := range d.newAuthors {
				b.WriteString(authorXML(prefix, a))
			}
			text := b.String()
			if decl := ns.decls(); decl != "" {
				// Declared on each new author, as the root does not.
				text = strings.ReplaceAll(text, "<"+prefix+":author ", "<"+prefix+":author"+decl+" ")
			}
			d.p.put(d.authorsPath, applyEdits(data, []edit{appendChild(data, root, text)}))
			d.authors, d.newAuthors = append(d.authors, d.newAuthors...), nil
			return
		}
	}
	var b strings.Builder
	b.WriteString(xmlDeclaration + `<p188:authorLst xmlns:a="` + nsA + `" xmlns:r="` + nsR + `" xmlns:p188="` + nsP188 + `">`)
	for _, a := range append(append([]author(nil), d.authors...), d.newAuthors...) {
		b.WriteString(authorXML("p188", a))
	}
	b.WriteString("</p188:authorLst>")
	path := d.authorsPath
	if path == "" {
		path = "ppt/authors.xml"
	}
	d.p.put(path, []byte(b.String()))
	if d.p.relTarget(d.presPath, relAuthors) == "" {
		d.p.addRel(d.presPath, relAuthors, relativeTarget(d.presPath, path))
		d.p.addOverride(path, ctAuthors)
	}
	d.authorsPath = path
	d.authors, d.newAuthors = append(d.authors, d.newAuthors...), nil
}

// names gives qualified names for new XML at a place in a part, declaring a namespace on the new
// XML when the place has no prefix for it.
type names struct {
	at   *node
	decl map[string]string
}

func newNames(at *node) *names { return &names{at: at, decl: map[string]string{}} }

func (n *names) prefix(space, want string) string {
	if p, ok := n.at.prefix(space); ok && p != "" {
		return p
	}
	n.decl[want] = space
	return want
}

func (n *names) decls() string {
	keys := make([]string, 0, len(n.decl))
	for k := range n.decl {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(` xmlns:` + k + `="` + n.decl[k] + `"`)
	}
	return b.String()
}

// shape is a shape at the top of a slide's shape tree, as the browser's deck has its elements.
type shape struct {
	id, name string
	el       *node
	cNvPr    *node
	txBody   *node
}

// shapes are the elements at the top of a slide's shape tree, in order.
func shapes(slideRoot *node) []shape {
	tree := slideRoot.kid(nsP, "cSld").kid(nsP, "spTree")
	// A slide with no shape tree (another writer's, or a broken one) has no shapes.
	if tree == nil {
		return nil
	}
	var out []shape
	for _, el := range tree.Children {
		if el.Name.Space != nsP {
			continue
		}
		if _, ok := monikerByNode[el.Name.Local]; !ok {
			continue
		}
		var cNvPr *node
		for _, nv := range el.Children {
			if strings.HasPrefix(nv.Name.Local, "nv") && nv.Name.Space == nsP {
				cNvPr = nv.kid(nsP, "cNvPr")
				break
			}
		}
		if cNvPr == nil {
			continue
		}
		s := shape{id: cNvPr.attr("id"), name: cNvPr.attr("name"), el: el, cNvPr: cNvPr}
		if el.is(nsP, "sp") {
			s.txBody = el.kid(nsP, "txBody")
		}
		out = append(out, s)
	}
	return out
}

func shapeByID(list []shape, id string) *shape {
	for i := range list {
		if list[i].id == id {
			return &list[i]
		}
	}
	return nil
}

// text is a shape's text as comments count it ("" for anything but a shape with text).
func (s *shape) text() string {
	if s == nil || s.txBody == nil {
		return ""
	}
	return plainText(s.txBody)
}

// width is the shape's width in points, as the browser's deck rounds it, read from the slide or
// from the placeholder it takes its place from.
func (d *deck) width(sl *slide, s *shape) float64 {
	cx, ok := extCX(s.el)
	if !ok {
		cx, ok = d.placeholderCX(sl.path, s.el, 0)
	}
	if !ok {
		return 0
	}
	return math.Round(float64(cx)/emuPerPt*100) / 100
}

func extCX(el *node) (int64, bool) {
	var pr *node
	for _, c := range el.Children {
		if c.Name.Space == nsP && (c.Name.Local == "spPr" || c.Name.Local == "grpSpPr") {
			pr = c
		}
	}
	if el.is(nsP, "graphicFrame") {
		pr = el
	}
	ext := pr.kid(nsA, "xfrm").kid(nsA, "ext")
	if ext == nil {
		ext = pr.kid(nsP, "xfrm").kid(nsA, "ext")
	}
	if ext == nil {
		return 0, false
	}
	cx, err := strconv.ParseInt(ext.attr("cx"), 10, 64)
	return cx, err == nil
}

// placeholderCX finds the size of the layout's (then the master's) placeholder a shape stands for.
func (d *deck) placeholderCX(part string, el *node, depth int) (int64, bool) {
	ph := el.first(nsP, "ph")
	if ph == nil || depth > 1 {
		return 0, false
	}
	relType := relLayout
	if depth == 1 {
		relType = relMaster
	}
	parent := d.p.relTarget(part, relType)
	root, _ := d.p.xml(parent)
	if root == nil {
		return 0, false
	}
	idx, typ := ph.attr("idx"), ph.attr("type")
	var match *node
	for _, s := range shapes(root) {
		other := s.el.first(nsP, "ph")
		if other == nil {
			continue
		}
		if idx != "" && other.attr("idx") == idx {
			match = s.el
			break
		}
		if match == nil && typ != "" && other.attr("type") == typ {
			match = s.el
		}
	}
	if match == nil {
		return 0, false
	}
	if cx, ok := extCX(match); ok {
		return cx, true
	}
	return d.placeholderCX(parent, match, depth+1)
}
