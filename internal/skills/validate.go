package skills

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Bounds on a skill's text (ADR 0x035 §1 and §10).
const (
	MaxNameLen          = 255
	MaxDescriptionLen   = 1024
	MaxBodyBytes        = 64 << 10
	MaxLicenseLen       = 500
	MaxCompatibilityLen = 500
	MaxAllowedTools     = 64
	MaxAllowedToolLen   = 128
	MaxMetadataEntries  = 64
	MaxMetadataKeyLen   = 64
	MaxMetadataValueLen = 2048
	// SoftBodyTokens is the advisory body size the editor warns above; the standard's guidance.
	SoftBodyTokens = 5000
)

// Normalize trims and sanitizes a ritual's text in place, derives a slug from the name when the
// request carries none and allowDeriveSlug is set, and checks every bound. Pointer fields that are
// nil are left nil (the caller decides whether that means "unchanged" or "default"). The first
// problem found is returned, wrapped in ErrInvalid.
func Normalize(r *models.Ritual, allowDeriveSlug bool) error {
	r.Name = strings.TrimSpace(SanitizeText(r.Name))
	r.Description = strings.TrimSpace(SanitizeText(r.Description))
	r.Content = strings.TrimSpace(SanitizeText(r.Content))
	r.Hotkeys = strings.TrimSpace(r.Hotkeys)
	r.Slug = strings.TrimSpace(strings.ToLower(r.Slug))

	switch {
	case r.Name == "":
		return fmt.Errorf("%w: name is required", ErrInvalid)
	case utf8.RuneCountInString(r.Name) > MaxNameLen:
		return fmt.Errorf("%w: name is at most %d characters", ErrInvalid, MaxNameLen)
	case r.Description == "":
		return fmt.Errorf("%w: description is required", ErrInvalid)
	case utf8.RuneCountInString(r.Description) > MaxDescriptionLen:
		return fmt.Errorf("%w: description is at most %d characters", ErrInvalid, MaxDescriptionLen)
	case r.Content == "":
		return fmt.Errorf("%w: content is required", ErrInvalid)
	case len(r.Content) > MaxBodyBytes:
		return fmt.Errorf("%w: content is at most %d KB", ErrInvalid, MaxBodyBytes>>10)
	}
	if err := RejectTags("name", r.Name); err != nil {
		return err
	}
	if err := RejectTags("description", r.Description); err != nil {
		return err
	}

	if r.Slug == "" && allowDeriveSlug {
		r.Slug = Slugify(r.Name)
	}
	if r.Slug != "" {
		if err := ValidateSlug(r.Slug); err != nil {
			return err
		}
	}

	if r.License != nil {
		v := strings.TrimSpace(SanitizeText(*r.License))
		if utf8.RuneCountInString(v) > MaxLicenseLen {
			return fmt.Errorf("%w: license is at most %d characters", ErrInvalid, MaxLicenseLen)
		}
		r.License = &v
	}
	if r.Compatibility != nil {
		v := strings.TrimSpace(SanitizeText(*r.Compatibility))
		if utf8.RuneCountInString(v) > MaxCompatibilityLen {
			return fmt.Errorf("%w: compatibility is at most %d characters", ErrInvalid, MaxCompatibilityLen)
		}
		r.Compatibility = &v
	}
	if r.AllowedTools != nil {
		tools, err := NormalizeAllowedTools(*r.AllowedTools)
		if err != nil {
			return err
		}
		r.AllowedTools = &tools
	}
	if r.Metadata != nil {
		meta, err := NormalizeMetadata(*r.Metadata)
		if err != nil {
			return err
		}
		r.Metadata = &meta
	}
	return nil
}

// NormalizeAllowedTools trims, sanitizes and de-duplicates allowed-tools entries, keeping their
// order. Entries are opaque tool names (`web_search`, `mcp__github__search`, `Bash(git:*)`): no
// whitespace, at most MaxAllowedToolLen chars. The result is never nil.
func NormalizeAllowedTools(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, raw := range in {
		name := strings.TrimSpace(SanitizeText(raw))
		if name == "" {
			continue
		}
		if utf8.RuneCountInString(name) > MaxAllowedToolLen {
			return nil, fmt.Errorf("%w: an allowed tool name is at most %d characters", ErrInvalid, MaxAllowedToolLen)
		}
		if strings.IndexFunc(name, unicode.IsSpace) >= 0 {
			return nil, fmt.Errorf("%w: allowed tool %q cannot contain whitespace", ErrInvalid, name)
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) > MaxAllowedTools {
		return nil, fmt.Errorf("%w: at most %d allowed tools", ErrInvalid, MaxAllowedTools)
	}
	return out, nil
}

// NormalizeMetadata trims and sanitizes metadata keys and values and drops empty keys. The
// result is never nil.
func NormalizeMetadata(in map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.TrimSpace(SanitizeText(k))
		if key == "" {
			continue
		}
		if utf8.RuneCountInString(key) > MaxMetadataKeyLen {
			return nil, fmt.Errorf("%w: a metadata key is at most %d characters", ErrInvalid, MaxMetadataKeyLen)
		}
		if strings.IndexFunc(key, unicode.IsSpace) >= 0 {
			return nil, fmt.Errorf("%w: metadata key %q cannot contain whitespace", ErrInvalid, key)
		}
		val := strings.TrimSpace(SanitizeText(v))
		if utf8.RuneCountInString(val) > MaxMetadataValueLen {
			return nil, fmt.Errorf("%w: metadata value for %q is at most %d characters", ErrInvalid, key, MaxMetadataValueLen)
		}
		out[key] = val
	}
	if len(out) > MaxMetadataEntries {
		return nil, fmt.Errorf("%w: at most %d metadata entries", ErrInvalid, MaxMetadataEntries)
	}
	return out, nil
}

// SortedMetadataKeys returns m's keys in order, for stable rendering.
func SortedMetadataKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// EstimateTokens is the rough token count the editor and index budget use for prose: about four
// bytes per token.
func EstimateTokens(s string) int {
	return (len(s) + 3) / 4
}
