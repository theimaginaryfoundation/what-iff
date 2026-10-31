package skills

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Metadata keys What Iff writes into SKILL.md so a skill round-trips through other harnesses
// without losing what the standard has no field for. All but display_name are prefixed.
const (
	MetaDisplayName            = "display_name"
	MetaHotkeys                = "whatiff-hotkeys"
	MetaConnectors             = "whatiff-connectors"
	MetaDisableModelInvocation = "whatiff-disable-model-invocation"
	MetaUserInvocable          = "whatiff-user-invocable"
)

// SkillFileName is the bundle's entry file.
const SkillFileName = "SKILL.md"

// Parsed is what ParseSkillMD extracts from a SKILL.md.
type Parsed struct {
	// Ritual carries Slug, Name, Description, Content, Hotkeys and the pointer fields, all set.
	Ritual models.Ritual
	// Connectors are the connector names the exporting account linked; they are account-local, so
	// the importer matches them by name and drops the rest.
	Connectors []string
	// Warnings are the lenient-parse repairs the user should see (a derived slug, an unknown key).
	Warnings []string
}

// frontmatter is the YAML document SKILL.md opens with, in the standard's field order.
type frontmatter struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	License       string            `yaml:"license,omitempty"`
	Compatibility string            `yaml:"compatibility,omitempty"`
	AllowedTools  string            `yaml:"allowed-tools,omitempty"`
	Metadata      map[string]string `yaml:"metadata,omitempty"`
}

// ParseSkillMD reads a SKILL.md leniently, as the standard's client guide recommends: a missing or
// invalid name is derived (with a warning), unknown frontmatter keys are kept in metadata, and only
// a missing description or body is an error. Text is sanitized and bounds are checked.
func ParseSkillMD(data []byte) (*Parsed, error) {
	data = bytes.TrimPrefix(data, []byte("\ufeff"))
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	fmText, body, err := splitFrontmatter(text)
	if err != nil {
		return nil, err
	}

	raw := map[string]any{}
	if err := yaml.Unmarshal([]byte(fmText), &raw); err != nil {
		return nil, fmt.Errorf("%w: SKILL.md frontmatter is not valid YAML: %v", ErrInvalid, err)
	}

	p := &Parsed{}
	r := &p.Ritual
	meta := map[string]string{}
	for k, v := range raw {
		switch strings.ToLower(k) {
		case "name":
			r.Slug = yamlScalar(v)
		case "description":
			r.Description = yamlScalar(v)
		case "license":
			r.License = ptr(yamlScalar(v))
		case "compatibility":
			r.Compatibility = ptr(yamlScalar(v))
		case "allowed-tools", "allowed_tools":
			r.AllowedTools = ptr(parseAllowedTools(v))
		case "metadata":
			m, ok := v.(map[string]any)
			if !ok {
				p.Warnings = append(p.Warnings, "metadata is not a map and was ignored")
				continue
			}
			for mk, mv := range m {
				meta[mk] = yamlScalar(mv)
			}
		default:
			// Keep unknown keys so the file round-trips; nested values are kept as YAML text.
			if _, taken := meta[k]; !taken {
				meta[k] = yamlScalar(v)
			}
			p.Warnings = append(p.Warnings, fmt.Sprintf("unknown frontmatter key %q kept in metadata", k))
		}
	}

	// Our own keys come back out of metadata.
	r.Name = strings.TrimSpace(meta[MetaDisplayName])
	r.Hotkeys = strings.TrimSpace(meta[MetaHotkeys])
	if names := strings.TrimSpace(meta[MetaConnectors]); names != "" {
		for _, n := range strings.Split(names, ",") {
			if n = strings.TrimSpace(n); n != "" {
				p.Connectors = append(p.Connectors, n)
			}
		}
	}
	r.DisableModelInvocation = ptr(parseBool(meta[MetaDisableModelInvocation], false))
	r.UserInvocable = ptr(parseBool(meta[MetaUserInvocable], true))
	for _, k := range []string{MetaDisplayName, MetaHotkeys, MetaConnectors, MetaDisableModelInvocation, MetaUserInvocable} {
		delete(meta, k)
	}
	r.Metadata = &meta
	if r.License == nil {
		r.License = ptr("")
	}
	if r.Compatibility == nil {
		r.Compatibility = ptr("")
	}
	if r.AllowedTools == nil {
		r.AllowedTools = ptr([]string{})
	}

	// Lenient name: derive what the file did not say well.
	slug := strings.TrimSpace(strings.ToLower(r.Slug))
	if r.Name == "" {
		if slug != "" {
			r.Name = titleFromSlug(slug)
		} else {
			r.Name = "Imported skill"
		}
	}
	switch {
	case slug == "":
		r.Slug = Slugify(r.Name)
		p.Warnings = append(p.Warnings, fmt.Sprintf("frontmatter has no name; using %q", r.Slug))
	case ValidateSlug(slug) != nil:
		r.Slug = Slugify(slug)
		p.Warnings = append(p.Warnings, fmt.Sprintf("name %q is not a valid skill name; using %q", slug, r.Slug))
	default:
		r.Slug = slug
	}
	if strings.TrimSpace(r.Description) == "" {
		return nil, fmt.Errorf("%w: SKILL.md frontmatter needs a description", ErrInvalid)
	}
	r.Content = strings.TrimSpace(body)
	if r.Content == "" {
		return nil, fmt.Errorf("%w: SKILL.md has no instructions after the frontmatter", ErrInvalid)
	}
	if err := Normalize(r, false); err != nil {
		return nil, err
	}
	return p, nil
}

// splitFrontmatter separates the leading YAML block from the Markdown body. The file must start
// with a `---` line and have a closing one.
func splitFrontmatter(text string) (string, string, error) {
	trimmed := strings.TrimLeft(text, "\n")
	if !strings.HasPrefix(trimmed, "---\n") && trimmed != "---" {
		return "", "", fmt.Errorf("%w: SKILL.md must start with a --- frontmatter block", ErrInvalid)
	}
	rest := strings.TrimPrefix(trimmed, "---\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		if strings.HasPrefix(rest, "---") {
			end = 0
		} else {
			return "", "", fmt.Errorf("%w: SKILL.md frontmatter has no closing ---", ErrInvalid)
		}
	}
	fm := rest[:end]
	body := rest[end:]
	body = strings.TrimPrefix(body, "\n---")
	if i := strings.Index(body, "\n"); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	return fm, body, nil
}

// RenderSkillMD writes the SKILL.md for r: the standard's fields from the columns, with What Iff's
// extras under metadata, and the body. connectors are the linked connectors' names.
func RenderSkillMD(r *models.Ritual, connectors []string) ([]byte, error) {
	meta := map[string]string{}
	for k, v := range r.MetadataMap() {
		meta[k] = v
	}
	if strings.TrimSpace(r.Name) != "" && r.Name != r.Slug {
		meta[MetaDisplayName] = r.Name
	}
	if h := strings.TrimSpace(r.Hotkeys); h != "" {
		meta[MetaHotkeys] = h
	}
	if len(connectors) > 0 {
		meta[MetaConnectors] = strings.Join(connectors, ", ")
	}
	if !r.IsModelInvocable() {
		meta[MetaDisableModelInvocation] = "true"
	}
	if !r.IsUserInvocable() {
		meta[MetaUserInvocable] = "false"
	}
	fm := frontmatter{
		Name:          r.Slug,
		Description:   r.Description,
		License:       r.LicenseText(),
		Compatibility: r.CompatibilityText(),
		AllowedTools:  strings.Join(r.AllowedToolList(), " "),
	}
	if len(meta) > 0 {
		fm.Metadata = meta
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return nil, fmt.Errorf("render SKILL.md frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("render SKILL.md frontmatter: %w", err)
	}
	buf.WriteString("---\n\n")
	buf.WriteString(strings.TrimSpace(r.Content))
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// parseAllowedTools accepts the standard's space-delimited string, a comma-delimited one, or a
// YAML list.
func parseAllowedTools(v any) []string {
	var out []string
	switch t := v.(type) {
	case []any:
		for _, item := range t {
			if s := strings.TrimSpace(yamlScalar(item)); s != "" {
				out = append(out, s)
			}
		}
	default:
		s := strings.NewReplacer(",", " ").Replace(yamlScalar(v))
		out = strings.Fields(s)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// yamlScalar renders a decoded YAML value as the string the spec wants (metadata is string→string).
func yamlScalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, err := yaml.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return strings.TrimSpace(string(b))
	}
}

func parseBool(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "1":
		return true
	case "false", "no", "0":
		return false
	}
	return def
}

// titleFromSlug turns "pdf-forms" into "Pdf forms" for a skill that gave no display name.
func titleFromSlug(slug string) string {
	words := strings.Split(slug, "-")
	if len(words[0]) > 0 {
		words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	}
	return strings.Join(words, " ")
}

func ptr[T any](v T) *T { return &v }
