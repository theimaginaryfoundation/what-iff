// Package skills holds the pure Agent Skills logic behind What Iff skills (rituals): slug
// rules, text sanitizing, bounds, SKILL.md parsing and rendering, and bundle zips. It knows
// nothing about the database or HTTP; the datastore and handlers call it (ADR 0x035).
package skills

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// MaxSlugLen is the Agent Skills limit on a skill's name (the slug here).
const MaxSlugLen = 64

// ErrInvalid is wrapped by every validation failure in this package, so callers can map any of
// them to a 400 without listing each.
var ErrInvalid = errors.New("invalid skill")

// Slugify derives a spec-conforming slug from a display name: lowercase ASCII letters and
// digits, runs of anything else collapsed to one hyphen, no leading, trailing or double hyphens,
// at most MaxSlugLen chars. A name with nothing usable in it gives "skill".
func Slugify(name string) string {
	var b strings.Builder
	lastHyphen := true // suppress a leading hyphen
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			// A non-ASCII letter or digit: the spec allows only [a-z0-9-], so it separates.
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
		if b.Len() >= MaxSlugLen {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "skill"
	}
	return slug
}

// ValidateSlug reports whether slug meets the Agent Skills name rules.
func ValidateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("%w: slug is required", ErrInvalid)
	}
	if len(slug) > MaxSlugLen {
		return fmt.Errorf("%w: slug is at most %d characters", ErrInvalid, MaxSlugLen)
	}
	if strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") {
		return fmt.Errorf("%w: slug cannot start or end with a hyphen", ErrInvalid)
	}
	if strings.Contains(slug, "--") {
		return fmt.Errorf("%w: slug cannot contain consecutive hyphens", ErrInvalid)
	}
	for _, r := range slug {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return fmt.Errorf("%w: slug may only contain lowercase letters, digits and hyphens", ErrInvalid)
		}
	}
	return nil
}

// UniqueSlug returns base, or base with a numeric suffix (base-2, base-3, …), whichever taken
// first reports free. The suffix is applied within MaxSlugLen. It gives up after 999 tries.
func UniqueSlug(base string, taken func(string) (bool, error)) (string, error) {
	if base == "" {
		base = "skill"
	}
	if ok, err := taken(base); err != nil {
		return "", err
	} else if !ok {
		return base, nil
	}
	for n := 2; n < 1000; n++ {
		suffix := fmt.Sprintf("-%d", n)
		stem := base
		if len(stem)+len(suffix) > MaxSlugLen {
			stem = strings.TrimRight(stem[:MaxSlugLen-len(suffix)], "-")
		}
		candidate := stem + suffix
		if ok, err := taken(candidate); err != nil {
			return "", err
		} else if !ok {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: no free slug for %q", ErrInvalid, base)
}
