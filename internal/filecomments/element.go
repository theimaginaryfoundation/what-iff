package filecomments

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Limits on an element anchor (ADR 0x050 §5). The page sends it, so it is bounded here: a selector
// is a few hundred characters even for a deep structural path, and the fingerprint is what a page
// needs to recognise the element, not a copy of it.
const (
	maxElementSelectorRunes = 1000
	maxElementTextRunes     = 200
	maxElementAttrs         = 8
	maxElementAttrNameRunes = 40
	maxElementAttrRunes     = 200
)

var elementTag = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// cleanElement is the element anchor as it may be kept: nil when it names no page (an HTML file's
// id) or no selector, the fingerprint trimmed to its limits and dropped when its tag is not one.
func cleanElement(e *models.FileCommentElement) *models.FileCommentElement {
	if e == nil {
		return nil
	}
	page := strings.TrimSpace(e.Page)
	if _, err := uuid.Parse(page); err != nil {
		return nil
	}
	selector := strings.TrimSpace(e.Selector)
	if selector == "" || utf8.RuneCountInString(selector) > maxElementSelectorRunes {
		return nil
	}
	out := &models.FileCommentElement{Page: page, Selector: selector}
	if f := e.Fingerprint; f != nil {
		if tag := strings.ToLower(strings.TrimSpace(f.Tag)); elementTag.MatchString(tag) {
			fp := &models.FileCommentElementFingerprint{Tag: tag, Text: truncateRunes(strings.TrimSpace(f.Text), maxElementTextRunes)}
			for name, value := range f.Attrs {
				if len(fp.Attrs) == maxElementAttrs {
					break
				}
				name = strings.TrimSpace(name)
				if name == "" || utf8.RuneCountInString(name) > maxElementAttrNameRunes {
					continue
				}
				if fp.Attrs == nil {
					fp.Attrs = map[string]string{}
				}
				fp.Attrs[name] = truncateRunes(value, maxElementAttrRunes)
			}
			out.Fingerprint = fp
		}
	}
	return out
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
