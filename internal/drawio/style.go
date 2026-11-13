package drawio

import (
	"fmt"
	"regexp"
	"strings"
)

// Style is a cell's style string as its parts, in order: bare names ("ellipse", "swimlane") and
// key=value pairs, separated by ';'.
type Style struct {
	parts []stylePart
}

type stylePart struct {
	key, value string
	pair       bool
}

// ParseStyle splits a style string. A value may itself hold '=' (a data URI); it never holds ';'.
func ParseStyle(s string) Style {
	var st Style
	for _, tok := range strings.Split(s, ";") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if k, v, ok := strings.Cut(tok, "="); ok {
			st.parts = append(st.parts, stylePart{key: strings.TrimSpace(k), value: v, pair: true})
		} else {
			st.parts = append(st.parts, stylePart{key: tok})
		}
	}
	return st
}

// Get is the value of key, or "".
func (s Style) Get(key string) string {
	for _, p := range s.parts {
		if p.pair && p.key == key {
			return p.value
		}
	}
	return ""
}

// Has reports whether the style has the bare name.
func (s Style) Has(name string) bool {
	for _, p := range s.parts {
		if !p.pair && p.key == name {
			return true
		}
	}
	return false
}

// Merge applies changes: a key=value pair sets the key (in place, or at the end), "key=" removes
// it, and a bare name is added when missing ("-name" removes it).
func (s Style) Merge(changes Style) Style {
	out := Style{parts: append([]stylePart(nil), s.parts...)}
	for _, c := range changes.parts {
		if !c.pair && strings.HasPrefix(c.key, "-") {
			out.drop(strings.TrimPrefix(c.key, "-"), false)
			continue
		}
		if !c.pair {
			if !out.Has(c.key) {
				out.parts = append(out.parts, c)
			}
			continue
		}
		if c.value == "" {
			out.drop(c.key, true)
			continue
		}
		set := false
		for i, p := range out.parts {
			if p.pair && p.key == c.key {
				out.parts[i].value = c.value
				set = true
			}
		}
		if !set {
			out.parts = append(out.parts, c)
		}
	}
	return out
}

func (s *Style) drop(key string, pair bool) {
	kept := s.parts[:0]
	for _, p := range s.parts {
		if p.key != key || p.pair != pair {
			kept = append(kept, p)
		}
	}
	s.parts = kept
}

// String is the style as draw.io writes it: each part followed by ';'.
func (s Style) String() string {
	var b strings.Builder
	for _, p := range s.parts {
		b.WriteString(p.key)
		if p.pair {
			b.WriteString("=" + p.value)
		}
		b.WriteString(";")
	}
	return b.String()
}

// CheckStyle reports what is wrong with a style string: a part with an empty key, or a key or
// bare name that is not a plain name (letters, digits, '.', '_', '-').
func CheckStyle(s string) error {
	for _, tok := range strings.Split(s, ";") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		k, _, pair := strings.Cut(tok, "=")
		k = strings.TrimSpace(k)
		if k == "" {
			return fmt.Errorf("%q has no key before '='", tok)
		}
		if !styleName(k) {
			if pair {
				return fmt.Errorf("%q is not a style key", k)
			}
			return fmt.Errorf("%q is neither key=value nor a style name", tok)
		}
	}
	return nil
}

// styleNamePattern is what draw.io style keys and bare style names are made of: ASCII letters,
// digits, '.', '_' and '-' (e.g. "fillColor", "mxgraph.flowchart.document", "shape-2").
var styleNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// styleName reports whether s can be a style key or a bare style name.
func styleName(s string) bool { return styleNamePattern.MatchString(s) }
