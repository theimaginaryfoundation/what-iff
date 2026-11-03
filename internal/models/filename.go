package models

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxFileNameRunes caps a file's display name.
const MaxFileNameRunes = 200

// maxExtensionRunes is the longest suffix treated as an extension: ".jpeg" and ".markdown" are
// extensions, the ".2 notes" in "v1.2 notes" is not.
const maxExtensionRunes = 10

// ErrInvalidFileName is what NormalizeFileName wraps for a name it refuses.
var ErrInvalidFileName = errors.New("invalid file name")

// NormalizeFileName turns a requested display name into the one to store. It is trimmed and must
// be non-empty, within MaxFileNameRunes, and free of control characters and of "/" and "\" (a name
// is not a folder path). A name without an extension keeps current's, so renaming
// "3f2a….png" to "self-portrait" gives "self-portrait.png". Only the label changes; the stored
// object keeps its key.
func NormalizeFileName(raw, current string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: a name cannot be empty", ErrInvalidFileName)
	}
	if strings.ContainsAny(name, folderSeparatorRuneSet) {
		return "", fmt.Errorf("%w: a name cannot contain / or \\ (use folder to file it)", ErrInvalidFileName)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: a name cannot contain control characters", ErrInvalidFileName)
		}
	}
	kept := ""
	if FileExtension(name) == "" {
		kept = FileExtension(current)
		name += kept
	}
	if utf8.RuneCountInString(name) > MaxFileNameRunes {
		if kept != "" {
			return "", fmt.Errorf("%w: a name is at most %d characters, counting the %s extension it keeps", ErrInvalidFileName, MaxFileNameRunes, kept)
		}
		return "", fmt.Errorf("%w: a name is at most %d characters", ErrInvalidFileName, MaxFileNameRunes)
	}
	return name, nil
}

// FileExtension returns name's extension with its dot (".png"), or "" when it has none. A suffix
// that is long or holds anything but letters and digits is not an extension.
func FileExtension(name string) string {
	ext := path.Ext(name)
	if len(ext) < 2 || ext == name || utf8.RuneCountInString(ext) > maxExtensionRunes {
		return ""
	}
	for _, r := range ext[1:] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return ""
		}
	}
	return ext
}
