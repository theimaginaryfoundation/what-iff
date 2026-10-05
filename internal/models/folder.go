package models

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on a gallery folder path. They keep paths readable in the UI and in the agent's listings.
const (
	MaxFolderDepth         = 8
	MaxFolderSegmentRunes  = 64
	MaxFolderPathRunes     = 255
	folderSeparator        = "/"
	folderSeparatorRuneSet = `/\`
)

// ErrInvalidFolder is what NormalizeFolder wraps for a path it refuses.
var ErrInvalidFolder = errors.New("invalid folder")

// NormalizeFolder turns what a person or an agent typed into the canonical folder path: segments
// separated by "/", trimmed, lower-cased, with empty segments dropped ("Charts//Oura/" is
// "charts/oura"). "" and "/" are the top level. Both "/" and "\" separate segments. "." and ".."
// are refused, as are control characters and paths deeper or longer than the limits above.
//
// Lower-casing makes "Charts" and "charts" one folder, so the gallery never shows two that differ
// only by case and every comparison (equality, prefix) can be exact.
func NormalizeFolder(raw string) (string, error) {
	var segments []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return strings.ContainsRune(folderSeparatorRuneSet, r) }) {
		seg := strings.ToLower(strings.TrimSpace(part))
		if seg == "" {
			continue
		}
		if seg == "." || seg == ".." {
			return "", fmt.Errorf("%w: %q is not allowed in a folder name", ErrInvalidFolder, seg)
		}
		for _, r := range seg {
			if unicode.IsControl(r) {
				return "", fmt.Errorf("%w: folder names cannot contain control characters", ErrInvalidFolder)
			}
		}
		if utf8.RuneCountInString(seg) > MaxFolderSegmentRunes {
			return "", fmt.Errorf("%w: a folder name is at most %d characters", ErrInvalidFolder, MaxFolderSegmentRunes)
		}
		segments = append(segments, seg)
	}
	if len(segments) > MaxFolderDepth {
		return "", fmt.Errorf("%w: folders nest at most %d deep", ErrInvalidFolder, MaxFolderDepth)
	}
	path := strings.Join(segments, folderSeparator)
	if utf8.RuneCountInString(path) > MaxFolderPathRunes {
		return "", fmt.Errorf("%w: a folder path is at most %d characters", ErrInvalidFolder, MaxFolderPathRunes)
	}
	return path, nil
}

// FolderIsWithin reports whether path is folder itself or beneath it. Both must be normalized.
// The top level ("") contains everything.
func FolderIsWithin(path, folder string) bool {
	return folder == "" || path == folder || strings.HasPrefix(path, folder+folderSeparator)
}

// ExpressionFolderRoot is the gallery folder that generated expression images are filed under, so
// they do not crowd the top level of the gallery.
const ExpressionFolderRoot = "expressions"

// ExpressionFolder is the folder a personality's generated expression images are filed in:
// "expressions/<name>". The name is always one folder level (a "/" or "\" in it becomes "-"), so
// any personality name gives a valid path. Personalities that share a name share a folder; the
// label is only a convenience, so that is fine.
func ExpressionFolder(personalityName string) string {
	name := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.NewReplacer("/", "-", `\`, "-").Replace(personalityName))
	if runes := []rune(strings.TrimSpace(name)); len(runes) > MaxFolderSegmentRunes {
		name = string(runes[:MaxFolderSegmentRunes])
	}
	if name = strings.TrimSpace(name); name == "" || name == "." || name == ".." {
		name = "unnamed"
	}
	folder, err := NormalizeFolder(ExpressionFolderRoot + folderSeparator + name)
	if err != nil {
		return ExpressionFolderRoot
	}
	return folder
}

// FolderCount is how many gallery images sit directly in a folder.
type FolderCount struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}
