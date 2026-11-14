package sharelink

import (
	"strings"
	"unicode"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// maxProjectPath bounds a path inside a shared web app.
const maxProjectPath = 1024

// projectPlace is where a path inside a shared web app is, among the owner's gallery files: the
// folder (root, or one inside it) and the file's name (ADR 0x051 §7). The path is relative, in
// "/"-separated segments; an empty segment, ".", "..", a backslash or a control character is
// refused, never resolved. Folder segments are lower-cased, as the gallery keeps folders; the
// name is matched as it is. ok is false for a path that is refused.
func projectPlace(root, path string) (folder, name string, ok bool) {
	if root == "" || path == "" || len(path) > maxProjectPath || strings.HasPrefix(path, "/") || strings.ContainsRune(path, '\\') {
		return "", "", false
	}
	segments := strings.Split(path, "/")
	for _, s := range segments {
		if s == "" || s == "." || s == ".." || strings.IndexFunc(s, unicode.IsControl) >= 0 {
			return "", "", false
		}
	}
	name = segments[len(segments)-1]
	folder = root
	for _, s := range segments[:len(segments)-1] {
		folder += "/" + strings.ToLower(s)
	}
	if !models.FolderIsWithin(folder, root) {
		return "", "", false
	}
	return folder, name, true
}

// projectPath is a file's path inside a shared web app rooted at root ("css/app.css"); ok is false
// for a file outside it.
func projectPath(root string, f models.FileAttachment) (string, bool) {
	if f.Folder == root {
		return f.Name, true
	}
	if !strings.HasPrefix(f.Folder, root+"/") {
		return "", false
	}
	return f.Folder[len(root)+1:] + "/" + f.Name, true
}
