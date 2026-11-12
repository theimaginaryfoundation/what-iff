package docx

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

// written is what a new comment is in the file.
type written struct {
	wordID, paraID, durableID string
}

var wordIDShape = regexp.MustCompile(`^[0-9A-F]{8}$`)

// newWordID is a Word paraId / durableId: 8 hex digits, at least 1 and below 0x7FFFFFFF, as the
// browser's newWordCommentId.
func newWordID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%08X", max(1, binary.BigEndian.Uint32(b[:])%0x7fffffff))
}

// newComment writes a comment into the comment parts (creating them when the file has none):
// its w:comment, its commentEx (a reply when parentParaID is set), its durable id, its UTC date
// and its author. The caller marks it in the text.
func (c *comments) newComment(cm officecomments.Comment, parentParaID string, done bool) (written, error) {
	if cm.CreatedAt.IsZero() {
		cm.CreatedAt = time.Now().UTC()
	}
	next := 0
	taken := map[string]bool{}
	for _, wc := range c.comments {
		if n, err := strconv.Atoi(wc.wordID); err == nil && n >= next {
			next = n + 1
		}
		taken[wc.lastParaID] = true
		taken[wc.durableID] = true
	}
	w := written{wordID: strconv.Itoa(next)}
	w.paraID = strings.ToUpper(cm.ID)
	if !wordIDShape.MatchString(w.paraID) || w.paraID >= "80000000" {
		w.paraID = newWordID()
	}
	for taken[w.paraID] {
		w.paraID = newWordID()
	}
	w.durableID = w.paraID

	styles, err := c.styleIDs()
	if err != nil {
		return w, err
	}
	if err := c.appendTo("comments", commentXML(cm, w, styles["CommentText"], styles["CommentReference"])); err != nil {
		return w, err
	}
	parent := ""
	if parentParaID != "" {
		parent = ` w15:paraIdParent="` + parentParaID + `"`
	}
	if err := c.appendTo("extended", `<w15:commentEx w15:paraId="`+w.paraID+`"`+parent+` w15:done="`+doneValue(done)+`"/>`); err != nil {
		return w, err
	}
	if err := c.appendTo("ids", `<w16cid:commentId w16cid:paraId="`+w.paraID+`" w16cid:durableId="`+w.durableID+`"/>`); err != nil {
		return w, err
	}
	utc := cm.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")
	if err := c.appendTo("extensible", `<w16cex:commentExtensible w16cex:durableId="`+w.durableID+`" w16cex:dateUtc="`+utc+`"/>`); err != nil {
		return w, err
	}
	known := false
	for _, p := range c.people {
		known = known || p.author == cm.AuthorName
	}
	if !known {
		presence := `<w15:presenceInfo w15:providerId="None" w15:userId="` + escapeXML(cm.AuthorName) + `"/>`
		if userID := officecomments.ProviderUserID(cm.AuthorID); userID != "" {
			presence = `<w15:presenceInfo w15:providerId="` + officecomments.WhatIffProvider + `" w15:userId="` + escapeXML(userID) + `"/>`
		}
		if err := c.appendTo("people", `<w15:person w15:author="`+escapeXML(cm.AuthorName)+`">`+presence+`</w15:person>`); err != nil {
			return w, err
		}
	}
	return w, nil
}

func doneValue(done bool) string {
	if done {
		return "1"
	}
	return "0"
}

// commentXML is a w:comment as the browser's commentXml writes it.
func commentXML(cm officecomments.Comment, w written, commentText, commentReference bool) string {
	lines := regexp.MustCompile(`\r\n|\r|\n`).Split(cm.Text, -1)
	pPr, refRPr := "", ""
	if commentText {
		pPr = `<w:pPr><w:pStyle w:val="CommentText"/></w:pPr>`
	}
	if commentReference {
		refRPr = `<w:rPr><w:rStyle w:val="CommentReference"/></w:rPr>`
	}
	base, _ := strconv.ParseUint(w.paraID, 16, 32)
	var b strings.Builder
	// Word writes w:date as local time marked Z; on the server local time is UTC.
	b.WriteString(`<w:comment w:id="` + w.wordID + `" w:author="` + escapeXML(cm.AuthorName) + `" w:date="` +
		cm.CreatedAt.UTC().Format("2006-01-02T15:04:05Z") + `" w:initials="` + escapeXML(initialsOf(cm.AuthorName)) + `">`)
	for i, line := range lines {
		// Only the last paragraph's id is the comment's; the others need ids of their own.
		paraID := w.paraID
		if i < len(lines)-1 {
			paraID = fmt.Sprintf("%08X", uint32(base)^uint32((i+1)*0x10000))
		}
		b.WriteString(`<w:p w14:paraId="` + paraID + `" w14:textId="77777777">` + pPr)
		if i == 0 {
			b.WriteString(`<w:r>` + refRPr + `<w:annotationRef/></w:r>`)
		}
		if line != "" {
			b.WriteString(`<w:r><w:t xml:space="preserve">` + escapeXML(line) + `</w:t></w:r>`)
		}
		b.WriteString(`</w:p>`)
	}
	b.WriteString(`</w:comment>`)
	return b.String()
}

// initialsOf is up to three initials of a name, or "?".
func initialsOf(name string) string {
	var b strings.Builder
	for _, word := range strings.Fields(name) {
		b.WriteString(strings.ToUpper(string([]rune(word)[0])))
	}
	r := []rune(b.String())
	if len(r) == 0 {
		return "?"
	}
	return string(r[:min(3, len(r))])
}

// referenceRun is the run that holds a comment's reference mark, as the browser writes it.
func (c *comments) referenceRun(wordID string) string {
	styles, _ := c.styleIDs()
	rPr := ""
	if styles["CommentReference"] {
		rPr = `<w:rPr><w:rStyle w:val="CommentReference"/></w:rPr>`
	}
	return `<w:r>` + rPr + `<w:commentReference w:id="` + wordID + `"/></w:r>`
}

// styleIDs are the style ids the file's styles part defines.
func (c *comments) styleIDs() (map[string]bool, error) {
	out := map[string]bool{}
	rels, err := c.p.rels(c.mainPath)
	if err != nil {
		return nil, err
	}
	for _, r := range rels {
		if r.typ != "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" || r.external {
			continue
		}
		data, err := c.p.read(resolveTarget(c.mainPath, r.target))
		if err != nil || data == nil {
			return out, err
		}
		root, err := parseXML(data)
		if err != nil {
			return out, nil
		}
		for _, s := range root.all(nsW, "style") {
			if id, ok := s.attr(nsW, "styleId"); ok {
				out[id] = true
			}
		}
	}
	return out, nil
}

// ensurePart is a comment part's path, relating the main document to a new one when it has none.
func (c *comments) ensurePart(key string) (string, error) {
	if name, ok := c.paths[key]; ok {
		return name, nil
	}
	var part commentPart
	for _, cp := range commentParts {
		if cp.key == key {
			part = cp
		}
	}
	name := path.Join(path.Dir(c.mainPath), part.file)
	data, err := c.p.read(name)
	if err != nil {
		return "", err
	}
	if data == nil {
		c.p.write(name, []byte(emptyPart(key)))
	}
	if err := c.p.addRel(c.mainPath, part.rel, part.file); err != nil {
		return "", err
	}
	if err := c.p.addOverride(name, part.contentType); err != nil {
		return "", err
	}
	c.paths[key] = name
	return name, nil
}

// appendTo adds entries at the end of a comment part, declaring the namespaces they use.
func (c *comments) appendTo(key, entries string) error {
	name, err := c.ensurePart(key)
	if err != nil {
		return err
	}
	data, err := c.p.read(name)
	if err != nil {
		return err
	}
	root, err := parseXML(data)
	if err != nil {
		return officecomments.ErrNotOffice
	}
	edits := withNamespaces(data, root, rootNamespaces[key])
	edits = append(edits, appendChildren(data, root, entries))
	c.p.write(name, applyEdits(data, edits))
	return nil
}

var doneAttr = regexp.MustCompile(`\s(\w+):done="[^"]*"`)

// setDone writes done on each comment of the thread, adding the commentEx a comment lacks.
func (c *comments) setDone(t *thread, resolved bool) error {
	name, err := c.ensurePart("extended")
	if err != nil {
		return err
	}
	data, err := c.p.read(name)
	if err != nil {
		return err
	}
	root, err := parseXML(data)
	if err != nil {
		return officecomments.ErrNotOffice
	}
	byPara := map[string]*node{}
	for _, el := range root.all(nsW15, "commentEx") {
		if id, ok := el.attr(nsW15, "paraId"); ok {
			byPara[strings.ToUpper(id)] = el
		}
	}
	value := doneValue(resolved)
	edits := withNamespaces(data, root, rootNamespaces["extended"])
	var missing strings.Builder
	rootPara := t.comments[0].lastParaID
	for i, wc := range t.comments {
		if wc.lastParaID == "" {
			continue
		}
		el := byPara[wc.lastParaID]
		if el == nil {
			parent := ""
			if i > 0 && rootPara != "" {
				parent = ` w15:paraIdParent="` + rootPara + `"`
			}
			missing.WriteString(`<w15:commentEx w15:paraId="` + wc.lastParaID + `"` + parent + ` w15:done="` + value + `"/>`)
			continue
		}
		tag := string(data[el.start:el.startEnd])
		if m := doneAttr.FindStringSubmatchIndex(tag); m != nil {
			prefix := tag[m[2]:m[3]]
			tag = tag[:m[0]] + ` ` + prefix + `:done="` + value + `"` + tag[m[1]:]
		} else {
			cut := len(tag) - 1
			if strings.HasSuffix(tag, "/>") {
				cut--
			}
			tag = tag[:cut] + ` w15:done="` + value + `"` + tag[cut:]
		}
		edits = append(edits, edit{el.start, el.startEnd, tag})
	}
	if missing.Len() > 0 {
		edits = append(edits, appendChildren(data, root, missing.String()))
	}
	c.p.write(name, applyEdits(data, edits))
	return nil
}
