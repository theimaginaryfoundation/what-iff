package pptx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

const (
	nsRel          = "http://schemas.openxmlformats.org/package/2006/relationships"
	nsCT           = "http://schemas.openxmlformats.org/package/2006/content-types"
	nsR            = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	relOfficeDoc   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	relSlide       = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide"
	relLayout      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout"
	relMaster      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster"
	xmlDeclaration = "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\r\n"
)

// pkg is an open .pptx: its zip entries in order, and the parts changed or added since.
type pkg struct {
	budget *officecomments.Budget
	// err is the first part that could not be read or parsed: a change is not saved over it, so a
	// broken or oversized part is never taken for a missing one and written again beside it.
	err error
	// read holds each part as first read, so reading it again costs nothing against the budget.
	readCache map[string][]byte
	files     []*zip.File
	byName    map[string]*zip.File
	changed   map[string][]byte
	added     []string
	removed   map[string]bool
}

func openPkg(data []byte) (*pkg, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, officecomments.ErrNotOffice
	}
	p := &pkg{budget: officecomments.NewBudget(), files: zr.File, byName: map[string]*zip.File{}, changed: map[string][]byte{}, removed: map[string]bool{}}
	for _, f := range zr.File {
		p.byName[f.Name] = f
	}
	if p.byName["[Content_Types].xml"] == nil {
		return nil, officecomments.ErrNotOffice
	}
	return p, nil
}

func (p *pkg) has(name string) bool {
	if p.removed[name] {
		return false
	}
	_, changed := p.changed[name]
	return changed || p.byName[name] != nil
}

// read is a part's bytes, or nil when there is no such part.
func (p *pkg) read(name string) []byte {
	if p.removed[name] {
		return nil
	}
	if b, ok := p.changed[name]; ok {
		return b
	}
	f := p.byName[name]
	if f == nil {
		return nil
	}
	if b, ok := p.readCache[name]; ok {
		return b
	}
	b, err := p.budget.Read(f)
	if err != nil {
		p.fail(fmt.Errorf("pptx: read %s: %w", name, err))
		return nil
	}
	if p.readCache == nil {
		p.readCache = map[string][]byte{}
	}
	p.readCache[name] = b
	return b
}

// fail records the first part that could not be read.
func (p *pkg) fail(err error) {
	if p.err == nil {
		p.err = err
	}
}

func (p *pkg) xml(name string) (*node, []byte) {
	data := p.read(name)
	if data == nil {
		return nil, nil
	}
	root, err := parseXML(data)
	if err != nil {
		if !errors.Is(err, officecomments.ErrTooLarge) {
			err = fmt.Errorf("%w: %s: %v", officecomments.ErrNotOffice, name, err)
		}
		p.fail(err)
		return nil, nil
	}
	return root, data
}

func (p *pkg) put(name string, data []byte) {
	delete(p.removed, name)
	if _, ok := p.changed[name]; !ok && p.byName[name] == nil {
		p.added = append(p.added, name)
	}
	p.changed[name] = data
}

// remove takes a part out, with its relationships and its content type override.
func (p *pkg) remove(name string) {
	p.removed[name] = true
	p.removed[relsPath(name)] = true
	ct, data := p.xml("[Content_Types].xml")
	if ct == nil {
		return
	}
	var edits []edit
	for _, o := range ct.kids(nsCT, "Override") {
		if o.attr("PartName") == "/"+name {
			edits = append(edits, edit{Start: o.Start, End: o.End})
		}
	}
	if len(edits) > 0 {
		p.put("[Content_Types].xml", applyEdits(data, edits))
	}
}

// save writes the package: untouched entries as they were, in their order, then the new ones.
func (p *pkg) save() ([]byte, error) {
	if p.err != nil {
		return nil, p.err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name string, data []byte, like *zip.File) error {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if like != nil {
			h.Modified = like.Modified
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	for _, f := range p.files {
		if p.removed[f.Name] {
			continue
		}
		if data, ok := p.changed[f.Name]; ok {
			if err := write(f.Name, data, f); err != nil {
				return nil, err
			}
			continue
		}
		if err := zw.Copy(f); err != nil {
			return nil, err
		}
	}
	for _, name := range p.added {
		if p.removed[name] {
			continue
		}
		if err := write(name, p.changed[name], nil); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// rel is one relationship of a part.
type rel struct {
	ID, Type, Target string
	External         bool
}

func relsPath(part string) string {
	dir, file := path.Split(part)
	return dir + "_rels/" + file + ".rels"
}

func (p *pkg) rels(part string) []rel {
	root, _ := p.xml(relsPath(part))
	var out []rel
	for _, r := range root.kids(nsRel, "Relationship") {
		out = append(out, rel{ID: r.attr("Id"), Type: r.attr("Type"), Target: r.attr("Target"), External: r.attr("TargetMode") == "External"})
	}
	return out
}

// relTarget is the part the first internal relationship of the type points to, or "".
func (p *pkg) relTarget(part, typ string) string {
	for _, r := range p.rels(part) {
		if r.Type == typ && !r.External {
			return resolveTarget(part, r.Target)
		}
	}
	return ""
}

// addRel adds a relationship to a part and returns its id.
func (p *pkg) addRel(part, typ, target string) string {
	existing := p.rels(part)
	id := freshRelID(existing)
	entry := fmt.Sprintf(`<Relationship Id="%s" Type="%s" Target="%s"/>`, id, typ, strings.NewReplacer("&", "&amp;", `"`, "&quot;").Replace(target))
	name := relsPath(part)
	root, data := p.xml(name)
	if root == nil {
		p.put(name, []byte(xmlDeclaration+`<Relationships xmlns="`+nsRel+`">`+entry+`</Relationships>`))
		return id
	}
	p.put(name, applyEdits(data, []edit{appendChild(data, root, entry)}))
	return id
}

// removeRel takes out the relationships of a part with the given ids.
func (p *pkg) removeRel(part string, ids ...string) {
	name := relsPath(part)
	root, data := p.xml(name)
	if root == nil {
		return
	}
	var edits []edit
	for _, r := range root.kids(nsRel, "Relationship") {
		for _, id := range ids {
			if r.attr("Id") == id {
				edits = append(edits, edit{Start: r.Start, End: r.End})
			}
		}
	}
	if len(edits) > 0 {
		p.put(name, applyEdits(data, edits))
	}
}

// addOverride names a part's content type.
func (p *pkg) addOverride(part, contentType string) {
	root, data := p.xml("[Content_Types].xml")
	if root == nil {
		return
	}
	// A part the types already name (a leftover Override) is not named twice.
	for _, o := range root.kids(nsCT, "Override") {
		if strings.EqualFold(o.attr("PartName"), "/"+part) {
			return
		}
	}
	entry := fmt.Sprintf(`<Override PartName="/%s" ContentType="%s"/>`, part, contentType)
	p.put("[Content_Types].xml", applyEdits(data, []edit{appendChild(data, root, entry)}))
}

func freshRelID(rels []rel) string {
	ids := map[string]bool{}
	for _, r := range rels {
		ids[r.ID] = true
	}
	n := 1
	for ids[fmt.Sprintf("rId%d", n)] {
		n++
	}
	return fmt.Sprintf("rId%d", n)
}

// resolveTarget is a relationship's target as a package path.
func resolveTarget(part, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(path.Clean(target), "/")
	}
	dir := path.Dir(part)
	if dir == "." {
		dir = ""
	}
	return strings.TrimPrefix(path.Clean("/"+dir+"/"+target), "/")
}

// relativeTarget is a part as seen from another part's folder ("../comments/x.xml").
func relativeTarget(fromPart, toPart string) string {
	var fromDir []string
	if i := strings.LastIndex(fromPart, "/"); i >= 0 {
		for _, s := range strings.Split(fromPart[:i], "/") {
			if s != "" {
				fromDir = append(fromDir, s)
			}
		}
	}
	to := strings.Split(toPart, "/")
	i := 0
	for i < len(fromDir) && i < len(to)-1 && fromDir[i] == to[i] {
		i++
	}
	var out []string
	for range fromDir[i:] {
		out = append(out, "..")
	}
	return strings.Join(append(out, to[i:]...), "/")
}
