package skills

import (
	"archive/zip"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode"
)

// Bundle limits (ADR 0x035 §3): the Claude API's bundle size, a file count that keeps listings
// readable, and a per-file cap.
const (
	MaxBundleFiles     = 200
	MaxBundleBytes     = 30 << 20
	MaxBundleFileBytes = 10 << 20
)

// File is one bundle member other than SKILL.md, at its path relative to the skill directory
// (`scripts/fill.py`, `references/FORMS.md`).
type File struct {
	Path string
	Data []byte
}

// Bundle is an Agent Skills directory read from a zip or a set of uploaded files.
type Bundle struct {
	// Root is the enclosing directory the files were under in the zip ("" when SKILL.md was at
	// the zip root), reported back so the user understands what was unpacked.
	Root    string
	SkillMD []byte
	Files   []File
}

// ReadBundle unpacks a skill zip: SKILL.md at the root, or inside exactly one top-level directory
// that holds everything. Directory entries, macOS metadata and anything outside that directory are
// skipped; traversal, absolute paths and oversize members are refused.
func ReadBundle(r io.ReaderAt, size int64) (*Bundle, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("%w: not a zip file", ErrInvalid)
	}
	type member struct {
		path string
		f    *zip.File
	}
	var members []member
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		p, err := cleanBundlePath(f.Name)
		if err != nil {
			return nil, err
		}
		if isJunkPath(p) {
			continue
		}
		members = append(members, member{path: p, f: f})
	}
	root, err := bundleRoot(mapPaths(members, func(m member) string { return m.path }))
	if err != nil {
		return nil, err
	}

	b := &Bundle{Root: root}
	var total int64
	for _, m := range members {
		rel := m.path
		if root != "" {
			if !strings.HasPrefix(rel, root+"/") {
				continue // outside the skill directory
			}
			rel = strings.TrimPrefix(rel, root+"/")
		}
		if m.f.UncompressedSize64 > uint64(MaxBundleFileBytes) {
			return nil, fmt.Errorf("%w: %s is larger than %d MB", ErrInvalid, rel, MaxBundleFileBytes>>20)
		}
		total += int64(m.f.UncompressedSize64)
		if total > MaxBundleBytes {
			return nil, fmt.Errorf("%w: bundle is larger than %d MB", ErrInvalid, MaxBundleBytes>>20)
		}
		rc, err := m.f.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: cannot read %s: %v", ErrInvalid, rel, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, int64(MaxBundleFileBytes)+1))
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: cannot read %s: %v", ErrInvalid, rel, err)
		}
		if len(data) > MaxBundleFileBytes {
			return nil, fmt.Errorf("%w: %s is larger than %d MB", ErrInvalid, rel, MaxBundleFileBytes>>20)
		}
		if strings.EqualFold(rel, SkillFileName) {
			b.SkillMD = data
			continue
		}
		b.Files = append(b.Files, File{Path: rel, Data: data})
	}
	return finishBundle(b)
}

// BundleFromFiles builds a bundle from already-read files (a multipart upload), applying the same
// root detection and limits as ReadBundle.
func BundleFromFiles(files []File) (*Bundle, error) {
	cleaned := make([]File, 0, len(files))
	for _, f := range files {
		p, err := cleanBundlePath(f.Path)
		if err != nil {
			return nil, err
		}
		if isJunkPath(p) {
			continue
		}
		cleaned = append(cleaned, File{Path: p, Data: f.Data})
	}
	root, err := bundleRoot(mapPaths(cleaned, func(f File) string { return f.Path }))
	if err != nil {
		return nil, err
	}
	b := &Bundle{Root: root}
	var total int
	for _, f := range cleaned {
		rel := f.Path
		if root != "" {
			if !strings.HasPrefix(rel, root+"/") {
				continue
			}
			rel = strings.TrimPrefix(rel, root+"/")
		}
		if len(f.Data) > MaxBundleFileBytes {
			return nil, fmt.Errorf("%w: %s is larger than %d MB", ErrInvalid, rel, MaxBundleFileBytes>>20)
		}
		total += len(f.Data)
		if total > MaxBundleBytes {
			return nil, fmt.Errorf("%w: bundle is larger than %d MB", ErrInvalid, MaxBundleBytes>>20)
		}
		if strings.EqualFold(rel, SkillFileName) {
			b.SkillMD = f.Data
			continue
		}
		b.Files = append(b.Files, File{Path: rel, Data: f.Data})
	}
	return finishBundle(b)
}

func finishBundle(b *Bundle) (*Bundle, error) {
	if b.SkillMD == nil {
		return nil, fmt.Errorf("%w: bundle has no %s", ErrInvalid, SkillFileName)
	}
	if len(b.Files) > MaxBundleFiles {
		return nil, fmt.Errorf("%w: bundle has more than %d files", ErrInvalid, MaxBundleFiles)
	}
	return b, nil
}

// WriteBundle writes `<slug>/SKILL.md` and the files under `<slug>/` as a zip.
func WriteBundle(w io.Writer, slug string, skillMD []byte, files []File) error {
	zw := zip.NewWriter(w)
	write := func(name string, data []byte) error {
		f, err := zw.Create(slug + "/" + name)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	}
	if err := write(SkillFileName, skillMD); err != nil {
		return fmt.Errorf("write bundle: %w", err)
	}
	for _, f := range files {
		p, err := cleanBundlePath(f.Path)
		if err != nil {
			return err
		}
		if err := write(p, f.Data); err != nil {
			return fmt.Errorf("write bundle: %w", err)
		}
	}
	return zw.Close()
}

// bundleRoot finds where SKILL.md sits: "" at the root, or the single top-level directory that
// holds one.
func bundleRoot(paths []string) (string, error) {
	roots := map[string]struct{}{}
	for _, p := range paths {
		parts := strings.Split(p, "/")
		if strings.EqualFold(parts[len(parts)-1], SkillFileName) {
			switch len(parts) {
			case 1:
				return "", nil
			case 2:
				roots[parts[0]] = struct{}{}
			}
		}
	}
	switch len(roots) {
	case 0:
		return "", fmt.Errorf("%w: bundle has no %s at its root or in a top-level folder", ErrInvalid, SkillFileName)
	case 1:
		for r := range roots {
			return r, nil
		}
	}
	return "", fmt.Errorf("%w: bundle holds more than one skill; import one at a time", ErrInvalid)
}

// cleanBundlePath normalizes a member path to forward slashes and refuses anything that could
// escape the skill directory or confuse a listing.
func cleanBundlePath(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	if p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "../") || p == ".." || strings.Contains(p, "/../") || strings.HasSuffix(p, "/..") {
		return "", fmt.Errorf("%w: bundle path %q is not allowed", ErrInvalid, p)
	}
	if strings.ContainsRune(p, 0) || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%w: bundle path %q contains control characters", ErrInvalid, p)
	}
	clean := path.Clean(p)
	if clean == "." || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("%w: bundle path %q is not allowed", ErrInvalid, p)
	}
	return clean, nil
}

// isJunkPath skips what archivers add: macOS resource forks and Finder metadata.
func isJunkPath(p string) bool {
	if strings.HasPrefix(p, "__MACOSX/") {
		return true
	}
	base := path.Base(p)
	return base == ".DS_Store" || strings.HasPrefix(base, "._")
}

func mapPaths[T any](items []T, f func(T) string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = f(it)
	}
	return out
}
