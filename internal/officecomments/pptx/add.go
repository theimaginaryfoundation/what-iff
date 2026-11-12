package pptx

import (
	"math"
	"strconv"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

// slidePin is where a thread on the whole slide puts its pin, as the browser does: along the top,
// clear of earlier ones.
var slidePin = struct{ x, y, step float64 }{12, 24, 26}

// jsRound rounds as JavaScript's Math.round.
func jsRound(x float64) int64 { return int64(math.Floor(x + 0.5)) }

func posXML(p188 string, x, y float64) string {
	return "<" + qualify(p188, "pos") + ` x="` + strconv.FormatInt(jsRound(x*emuPerPt), 10) + `" y="` + strconv.FormatInt(jsRound(y*emuPerPt), 10) + `"/>`
}

// Add starts a thread on a range of a shape's text (Anchor.Quote) or on a slide.
func (Format) Add(data []byte, at officecomments.Anchor, first officecomments.Comment) ([]byte, string, error) {
	d, err := openDeck(data)
	if err != nil {
		return nil, "", err
	}
	if at.Slide < 0 || at.Slide > len(d.slides) || (at.Quote == "" && at.Slide == 0) {
		return nil, "", officecomments.ErrBadAnchor
	}
	if !isGUID(first.ID) {
		first.ID = newGUID()
	}

	var sl *slide
	var target *match
	if at.Quote != "" {
		if target, err = d.findQuote(at); err != nil {
			return nil, "", err
		}
		sl = target.slide
	} else {
		sl = d.slides[at.Slide-1]
	}
	before := d.slideThreads(sl)
	// A slide whose comments change has its legacy comments written in the modern form.
	d.modernize(sl, "")

	slideRoot, slideData := d.p.xml(sl.path)
	sldMk := d.slideMk(sl, slideRoot)
	var anchor string
	var slideEdits []edit
	var x, y float64
	if target != nil {
		list := shapes(slideRoot)
		sh := shapeByID(list, target.shapeID)
		moniker, edits := shapeMoniker(slideData, slideRoot, target.shapeID)
		slideEdits = edits
		anchor = `<ac:txMkLst xmlns:ac="` + nsAC + `" xmlns:pc="` + nsPC + `">` + sldMk + moniker +
			`<ac:txMk cp="` + strconv.Itoa(target.cp) + `" len="` + strconv.Itoa(target.length) + `">` + textContext(sh.txBody) + `</ac:txMk></ac:txMkLst>`
		// The pin is at the shape's top-right corner, from the shape's top-left.
		x = d.width(sl, sh)
	} else {
		taken := 0
		for _, t := range before {
			if !t.onShape && t.pos[1] == slidePin.y {
				taken++
			}
		}
		anchor = `<pc:sldMkLst xmlns:pc="` + nsPC + `">` + sldMk + `</pc:sldMkLst>`
		x, y = slidePin.x+float64(taken)*slidePin.step, slidePin.y
	}
	d.addCMs(sl, func(p188, a string) string {
		return d.commentXML(p188, a, "cm", first, anchor+posXML(p188, x, y))
	}, slideEdits)
	d.writeAuthors()
	out, err := d.p.save()
	if err != nil {
		return nil, "", err
	}
	return out, first.ID, nil
}

// match is where a quote was found: a shape's text, in UTF-16 code units as the browser counts.
type match struct {
	slide      *slide
	shapeID    string
	cp, length int
}

func (d *deck) findQuote(at officecomments.Anchor) (*match, error) {
	slides := d.slides
	if at.Slide > 0 {
		slides = d.slides[at.Slide-1 : at.Slide]
	}
	var found []match
	for _, sl := range slides {
		root, _ := d.p.xml(sl.path)
		if root == nil {
			continue
		}
		for _, sh := range shapes(root) {
			if sh.txBody == nil {
				continue
			}
			text := sh.text()
			for from := 0; ; {
				i := strings.Index(text[from:], at.Quote)
				if i < 0 {
					break
				}
				start := from + i
				found = append(found, match{slide: sl, shapeID: sh.id, cp: utf16Len(text[:start]), length: utf16Len(at.Quote)})
				from = start + len(at.Quote)
			}
		}
	}
	switch {
	case at.Occurrence > 0 && at.Occurrence <= len(found):
		return &found[at.Occurrence-1], nil
	case at.Occurrence > 0 || len(found) == 0:
		return nil, officecomments.ErrQuoteNotFound
	case len(found) > 1:
		return nil, officecomments.ErrQuoteAmbiguous
	}
	return &found[0], nil
}

// shapeMoniker is the moniker naming a shape on the slide (ac:spMk and its kin): its id and its
// creation id. A shape with no creation id is given one, as the browser and PowerPoint do; the
// edits make that change to the slide.
func shapeMoniker(data []byte, slideRoot *node, shapeID string) (string, []edit) {
	var cNvPr *node
	slideRoot.walk(func(n *node) {
		if cNvPr == nil && n.is(nsP, "cNvPr") && n.attr("id") == shapeID {
			cNvPr = n
		}
	})
	if cNvPr == nil || cNvPr.Parent == nil || cNvPr.Parent.Parent == nil {
		return "", nil
	}
	name := monikerByNode[cNvPr.Parent.Parent.Name.Local]
	if name == "" {
		return "", nil
	}
	var edits []edit
	creation := cNvPr.first(nsA16, "creationId")
	id := ""
	if creation != nil {
		id = creation.attr("id")
	} else {
		id = newGUID()
		ns := newNames(cNvPr)
		a := ns.prefix(nsA, "a")
		ext := "<" + a + `:ext uri="` + creationIDExt + `"><a16:creationId xmlns:a16="` + nsA16 + `" id="` + id + `"/></` + a + ":ext>"
		if extLst := cNvPr.kid(nsA, "extLst"); extLst != nil {
			edits = append(edits, appendChild(data, extLst, withDecls(ext, ns.decls())))
		} else {
			edits = append(edits, appendChild(data, cNvPr, withDecls("<"+a+":extLst>"+ext+"</"+a+":extLst>", ns.decls())))
		}
	}
	return `<ac:` + name + ` id="` + escapeXML(shapeID) + `" creationId="` + escapeXML(id) + `"/>`, edits
}

// modernize writes a slide's legacy comments in the modern form, as the browser does when a
// slide's comments change, and returns the thread with the id given (nil if none or "").
func (d *deck) modernize(sl *slide, id string) *thread {
	var legacy []rel
	for _, r := range d.p.rels(sl.path) {
		if r.Type == relLegacyComments && !r.External {
			legacy = append(legacy, r)
		}
	}
	if len(legacy) == 0 {
		return d.threadOn(sl, id)
	}
	var old []thread
	for _, t := range d.slideThreads(sl) {
		if t.cm == nil {
			old = append(old, t)
		}
	}
	slideRoot, _ := d.p.xml(sl.path)
	sldMk := `<pc:sldMkLst xmlns:pc="` + nsPC + `">` + d.slideMk(sl, slideRoot) + `</pc:sldMkLst>`
	d.addCMs(sl, func(p188, a string) string {
		var b strings.Builder
		for _, t := range old {
			// The replies' authors come first, as the browser names them.
			replies := ""
			if len(t.Comments) > 1 {
				replies = "<" + p188 + ":replyLst>"
				for _, c := range t.Comments[1:] {
					replies += d.commentXML(p188, a, "reply", c, "")
				}
				replies += "</" + p188 + ":replyLst>"
			}
			b.WriteString(d.commentXML(p188, a, "cm", t.Comments[0], sldMk+posXML(p188, t.pos[0], t.pos[1])+replies))
		}
		return b.String()
	}, nil)
	ids := make([]string, 0, len(legacy))
	for _, r := range legacy {
		d.p.remove(resolveTarget(sl.path, r.Target))
		ids = append(ids, r.ID)
	}
	d.p.removeRel(sl.path, ids...)
	return d.threadOn(sl, id)
}

func (d *deck) threadOn(sl *slide, id string) *thread {
	if id == "" {
		return nil
	}
	for _, t := range d.slideThreads(sl) {
		if strings.EqualFold(t.ID, id) {
			return &t
		}
	}
	return nil
}
