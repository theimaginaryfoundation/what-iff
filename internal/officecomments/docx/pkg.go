package docx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"path"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

const (
	relOfficeDocument = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	relHeader         = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header"
	relFooter         = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer"
	contentTypesPath  = "[Content_Types].xml"
)

// pkg is a .docx opened for reading, with the parts a change rewrites or adds.
type pkg struct {
	budget  *officecomments.Budget
	zr      *zip.Reader
	files   map[string]*zip.File
	changed map[string][]byte
	added   []string
}

func openPkg(data []byte) (*pkg, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, officecomments.ErrNotOffice
	}
	p := &pkg{budget: officecomments.NewBudget(), zr: zr, files: map[string]*zip.File{}, changed: map[string][]byte{}}
	for _, f := range zr.File {
		p.files[f.Name] = f
	}
	if p.files[contentTypesPath] == nil {
		return nil, officecomments.ErrNotOffice
	}
	return p, nil
}

// read is a part's bytes (as changed so far), or nil when it is not there.
func (p *pkg) read(name string) ([]byte, error) {
	if b, ok := p.changed[name]; ok {
		return b, nil
	}
	f := p.files[name]
	if f == nil {
		return nil, nil
	}
	data, err := p.budget.Read(f)
	if err != nil {
		return nil, fmt.Errorf("docx: read %s: %w", name, err)
	}
	return data, nil
}

func (p *pkg) write(name string, data []byte) {
	if _, ok := p.changed[name]; !ok && p.files[name] == nil {
		p.added = append(p.added, name)
	}
	p.changed[name] = data
}

// bytes is the whole package: untouched entries copied as they were, in their order, changed
// ones written again in their place and new ones at the end.
func (p *pkg) bytes() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	put := func(name string, method uint16, data []byte, like *zip.FileHeader) error {
		h := &zip.FileHeader{Name: name, Method: method}
		if like != nil {
			h.Modified, h.Comment = like.Modified, like.Comment
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	for _, f := range p.zr.File {
		data, ok := p.changed[f.Name]
		if !ok {
			if err := zw.Copy(f); err != nil {
				return nil, fmt.Errorf("docx: copy %s: %w", f.Name, err)
			}
			continue
		}
		if err := put(f.Name, zip.Deflate, data, &f.FileHeader); err != nil {
			return nil, err
		}
	}
	for _, name := range p.added {
		if err := put(name, zip.Deflate, p.changed[name], nil); err != nil {
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
	id, typ, target string
	external        bool
}

// relsPath is where a part's relationships are.
func relsPath(part string) string {
	dir, file := path.Split(part)
	return dir + "_rels/" + file + ".rels"
}

// rels reads a part's relationships; none when it has no .rels.
func (p *pkg) rels(part string) ([]rel, error) {
	data, err := p.read(relsPath(part))
	if err != nil || data == nil {
		return nil, err
	}
	root, err := parseXML(data)
	if err != nil {
		return nil, officecomments.NotOffice(err)
	}
	var out []rel
	for _, r := range root.children {
		if r.local != "Relationship" {
			continue
		}
		id, _ := r.attr("", "Id")
		typ, _ := r.attr("", "Type")
		target, _ := r.attr("", "Target")
		mode, _ := r.attr("", "TargetMode")
		out = append(out, rel{id: id, typ: typ, target: target, external: mode == "External"})
	}
	return out, nil
}

// resolveTarget is a relationship's target as a package path, relative to the part.
func resolveTarget(part, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(path.Clean(target), "/")
	}
	return strings.TrimPrefix(path.Join(path.Dir(part), target), "/")
}

// mainPart is the package's main document.
func (p *pkg) mainPart() (string, error) {
	rels, err := p.rels("")
	if err != nil {
		return "", err
	}
	for _, r := range rels {
		if r.typ == relOfficeDocument && !r.external {
			return resolveTarget("", r.target), nil
		}
	}
	if p.files["word/document.xml"] != nil {
		return "word/document.xml", nil
	}
	return "", officecomments.ErrNotOffice
}

// addRel relates a part to a new target.
func (p *pkg) addRel(part, typ, target string) error {
	name := relsPath(part)
	data, err := p.read(name)
	if err != nil {
		return err
	}
	if data == nil {
		data = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`)
	}
	root, err := parseXML(data)
	if err != nil {
		return officecomments.NotOffice(err)
	}
	used := map[string]bool{}
	for _, r := range root.children {
		id, _ := r.attr("", "Id")
		used[id] = true
	}
	n := len(root.children) + 1
	for used[fmt.Sprintf("rId%d", n)] {
		n++
	}
	x := fmt.Sprintf(`<Relationship Id="rId%d" Type="%s" Target="%s"/>`, n, escapeXML(typ), escapeXML(target))
	p.write(name, applyEdits(data, []edit{appendChildren(data, root, x)}))
	return nil
}

// addOverride gives a part its content type, when [Content_Types].xml does not already.
func (p *pkg) addOverride(part, contentType string) error {
	data, err := p.read(contentTypesPath)
	if err != nil {
		return err
	}
	root, err := parseXML(data)
	if err != nil {
		return officecomments.NotOffice(err)
	}
	name := "/" + part
	for _, o := range root.children {
		if v, _ := o.attr("", "PartName"); o.local == "Override" && strings.EqualFold(v, name) {
			return nil
		}
	}
	x := fmt.Sprintf(`<Override PartName="%s" ContentType="%s"/>`, escapeXML(name), escapeXML(contentType))
	p.write(contentTypesPath, applyEdits(data, []edit{appendChildren(data, root, x)}))
	return nil
}
