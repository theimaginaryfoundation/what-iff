package models

import "strings"

// IsHTMLFile reports whether a file with this name and media type is a web page (ADR 0x050): an
// .html or .htm file, or one stored as text/html or application/xhtml+xml.
func IsHTMLFile(name, mediaType string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	kind := strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0]))
	return strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm") || kind == "text/html" || kind == "application/xhtml+xml"
}
