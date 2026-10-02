// Package exporter builds a clean, id-stripped projection of a user's account into an export archive
// tree (rendered to a ZIP, see ZipTree). Its conversations.json round-trips back through the
// OpenAI/Claude import pipeline.
//
// This is a WET-parallel to internal/datastore/accountbackup.go (the admin ZIP clone tool):
// the admin exporter preserves raw Postgres identity and cannot be loaded into a second account
// without primary-key collisions. This package deliberately drops internal ids so an export is
// portable. The two are expected to converge once this shape settles.
package exporter

import (
	"bytes"
	"sort"
)

// Tree is a sink for repo-relative files. Paths use forward slashes and are relative to the repo
// root (e.g. "conversations.json", "personalities/<id>/scratchpad.md"). Implementations render the
// tree onto a git working directory (the worker), a zip, or memory (tests).
type Tree interface {
	// Write creates or overwrites the file at path with content. Writing the same path twice is
	// last-write-wins; callers must not depend on ordering for correctness.
	Write(path string, content []byte) error
}

// MemTree is an in-memory Tree used by tests and for deterministic assembly. It is not safe for
// concurrent use.
type MemTree struct {
	files map[string][]byte
}

// NewMemTree returns an empty in-memory tree.
func NewMemTree() *MemTree {
	return &MemTree{files: make(map[string][]byte)}
}

// Write records content at path, copying the slice so later mutation by the caller is not observed.
func (t *MemTree) Write(path string, content []byte) error {
	buf := make([]byte, len(content))
	copy(buf, content)
	t.files[path] = buf
	return nil
}

// Get returns the content written at path and whether it exists.
func (t *MemTree) Get(path string) ([]byte, bool) {
	b, ok := t.files[path]
	return b, ok
}

// Paths returns all written paths in sorted order — the deterministic enumeration the git tree
// relies on so identical account state produces identical commits.
func (t *MemTree) Paths() []string {
	paths := make([]string, 0, len(t.files))
	for p := range t.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// Equal reports whether two MemTrees hold identical path→content sets. Used by determinism tests.
func (t *MemTree) Equal(other *MemTree) bool {
	if len(t.files) != len(other.files) {
		return false
	}
	for p, b := range t.files {
		ob, ok := other.files[p]
		if !ok || !bytes.Equal(b, ob) {
			return false
		}
	}
	return true
}
