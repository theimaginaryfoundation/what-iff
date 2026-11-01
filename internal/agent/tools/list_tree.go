package tools

import (
	"sort"
	"strings"
)

// renderFileTree draws files as a folder tree of their paths, like the tree command: folders
// first, then files, each sorted case-insensitively. Files agents wrote are marked "(agent)".
func renderFileTree(items []listItem) string {
	type node struct {
		dirs  map[string]*node
		files []string
	}
	newNode := func() *node { return &node{dirs: map[string]*node{}} }
	root := newNode()
	for _, it := range items {
		n := root
		if it.Folder != "" {
			for _, seg := range strings.Split(it.Folder, "/") {
				child, ok := n.dirs[seg]
				if !ok {
					child = newNode()
					n.dirs[seg] = child
				}
				n = child
			}
		}
		name := it.Name
		if it.Origin != "" {
			name += " (" + it.Origin + ")"
		}
		n.files = append(n.files, name)
	}

	byFold := func(s []string) {
		sort.Slice(s, func(i, j int) bool { return strings.ToLower(s[i]) < strings.ToLower(s[j]) })
	}
	var b strings.Builder
	b.WriteString(".\n")
	var walk func(n *node, prefix string)
	walk = func(n *node, prefix string) {
		dirs := make([]string, 0, len(n.dirs))
		for d := range n.dirs {
			dirs = append(dirs, d)
		}
		byFold(dirs)
		byFold(n.files)
		entries := len(dirs) + len(n.files)
		i := 0
		emit := func(label string) (childPrefix string) {
			i++
			branch, next := "├── ", "│   "
			if i == entries {
				branch, next = "└── ", "    "
			}
			b.WriteString(prefix + branch + label + "\n")
			return prefix + next
		}
		for _, d := range dirs {
			walk(n.dirs[d], emit(d+"/"))
		}
		for _, f := range n.files {
			emit(f)
		}
	}
	walk(root, "")
	return strings.TrimSuffix(b.String(), "\n")
}
