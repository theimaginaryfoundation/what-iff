package xlsx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"path"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

const (
	nsRels       = "http://schemas.openxmlformats.org/package/2006/relationships"
	relOfficeDoc = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	xmlHead      = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
)

// pkg is an open zip package. Parts that change are kept here until save; every other entry is
// copied as it was.
type pkg struct {
	budget *officecomments.Budget
	// err is the first part that could not be read: a change is not saved over it, so an
	// oversized part is never taken for a missing one and written again beside it.
	err     error
	r       *zip.Reader
	files   map[string]*zip.File
	changed map[string][]byte
	added   []string
}

func openPkg(data []byte) (*pkg, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, officecomments.ErrNotOffice
	}
	p := &pkg{budget: officecomments.NewBudget(), r: r, files: map[string]*zip.File{}, changed: map[string][]byte{}}
	for _, f := range r.File {
		p.files[strings.ToLower(f.Name)] = f
	}
	return p, nil
}

// actual is the part's name as the package spells it (part names ignore case).
func (p *pkg) actual(part string) (string, bool) {
	if _, ok := p.changed[part]; ok {
		return part, true
	}
	if f, ok := p.files[strings.ToLower(part)]; ok {
		return f.Name, true
	}
	for name := range p.changed {
		if strings.EqualFold(name, part) {
			return name, true
		}
	}
	return part, false
}

func (p *pkg) has(part string) bool {
	_, ok := p.actual(part)
	return ok
}

func (p *pkg) read(part string) ([]byte, bool) {
	name, ok := p.actual(part)
	if !ok {
		return nil, false
	}
	if data, ok := p.changed[name]; ok {
		return data, true
	}
	data, err := p.budget.Read(p.files[strings.ToLower(name)])
	if err != nil {
		if p.err == nil {
			p.err = fmt.Errorf("xlsx: read %s: %w", name, err)
		}
		return nil, false
	}
	return data, true
}

func (p *pkg) put(part string, data []byte) {
	name, ok := p.actual(part)
	if !ok {
		p.added = append(p.added, name)
	}
	p.changed[name] = data
}

// save writes the package: entries in their order, unchanged ones copied raw, new parts last.
func (p *pkg) save() ([]byte, error) {
	if p.err != nil {
		return nil, p.err
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	write := func(h *zip.FileHeader, data []byte) error {
		fw, err := w.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = fw.Write(data)
		return err
	}
	for _, f := range p.r.File {
		data, ok := p.changed[f.Name]
		if !ok {
			if err := w.Copy(f); err != nil {
				return nil, err
			}
			continue
		}
		if err := write(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: f.Modified}, data); err != nil {
			return nil, err
		}
	}
	for _, name := range p.added {
		if err := write(&zip.FileHeader{Name: name, Method: zip.Deflate}, p.changed[name]); err != nil {
			return nil, err
		}
	}
	if p.r.Comment != "" {
		if err := w.SetComment(p.r.Comment); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// unused is the first name the pattern gives (with 1, 2, ...) that no part has.
func (p *pkg) unused(pattern string) string {
	for n := 1; ; n++ {
		name := fmt.Sprintf(pattern, n)
		if !p.has(name) {
			return name
		}
	}
}

type rel struct {
	id, typ, target string
	external        bool
}

func relsPath(part string) string {
	dir, file := path.Split(part)
	return dir + "_rels/" + file + ".rels"
}

func (p *pkg) rels(part string) []rel {
	data, ok := p.read(relsPath(part))
	if !ok {
		return nil
	}
	root, err := scan(data, 1)
	if err != nil {
		return nil
	}
	var out []rel
	for _, n := range root.kids("Relationship") {
		out = append(out, rel{id: n.get("Id"), typ: n.get("Type"), target: n.get("Target"), external: n.get("TargetMode") == "External"})
	}
	return out
}

// relOf is the part the first relationship of the type points at, or "".
func (p *pkg) relOf(part, typ string) string {
	for _, r := range p.rels(part) {
		if r.typ == typ && !r.external {
			return resolve(part, r.target)
		}
	}
	return ""
}

func (p *pkg) relByID(part, id string) string {
	for _, r := range p.rels(part) {
		if r.id == id && !r.external {
			return resolve(part, r.target)
		}
	}
	return ""
}

// resolve is the part a relationship target from `from` names.
func resolve(from, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(path.Clean(target), "/")
	}
	return strings.TrimPrefix(path.Clean(path.Join(path.Dir(from), target)), "./")
}

// relTarget is the relative target from part `from` to part `to`.
func relTarget(from, to string) string {
	fromDir := strings.Split(path.Dir(from), "/")
	if path.Dir(from) == "." {
		fromDir = nil
	}
	toParts := strings.Split(to, "/")
	i := 0
	for i < len(fromDir) && i < len(toParts)-1 && fromDir[i] == toParts[i] {
		i++
	}
	return strings.Repeat("../", len(fromDir)-i) + strings.Join(toParts[i:], "/")
}

// addRel adds a relationship from part to target (a part) and returns its id, numbered as the
// browser numbers them.
func (p *pkg) addRel(part, typ, target string) string {
	rels := p.rels(part)
	n := len(rels) + 1
	used := func(id string) bool {
		for _, r := range rels {
			if r.id == id {
				return true
			}
		}
		return false
	}
	for used(fmt.Sprintf("rId%d", n)) {
		n++
	}
	id := fmt.Sprintf("rId%d", n)
	entry := `<Relationship Id="` + id + `" Type="` + typ + `" Target="` + escapeXML(relTarget(part, target)) + `"/>`
	data, ok := p.read(relsPath(part))
	var root *node
	if ok {
		root, _ = scan(data, 0)
	}
	if root == nil {
		p.put(relsPath(part), []byte(xmlHead+`<Relationships xmlns="`+nsRels+`">`+entry+`</Relationships>`))
		return id
	}
	p.put(relsPath(part), apply(data, []edit{appendChild(data, root, entry)}))
	return id
}

const contentTypesPath = "[Content_Types].xml"

// addOverride gives a part its content type unless it has one.
func (p *pkg) addOverride(part, contentType string) {
	p.addType(`PartName="/`+part+`"`, `<Override PartName="/`+part+`" ContentType="`+contentType+`"/>`)
}

// addDefault gives an extension its content type unless it has one.
func (p *pkg) addDefault(ext, contentType string) {
	p.addType(`Extension="`+ext+`"`, `<Default Extension="`+ext+`" ContentType="`+contentType+`"/>`)
}

func (p *pkg) addType(marker, entry string) {
	data, _ := p.read(contentTypesPath)
	if strings.Contains(strings.ToLower(string(data)), strings.ToLower(marker)) {
		return
	}
	root, err := scan(data, 0)
	if err != nil {
		return
	}
	p.put(contentTypesPath, apply(data, []edit{appendChild(data, root, entry)}))
}
