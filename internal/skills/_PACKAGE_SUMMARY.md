# Package: `internal/skills`

## Role

**Pure Agent Skills logic** for What Iff skills (rituals), per [ADR 0x035](../../docs/adr/0x035-agent-skills.md): slug rules, text sanitizing, bounds, SKILL.md parsing and rendering, and bundle zips.
It knows nothing about the database or HTTP; `internal/datastore`, `internal/database` and `internal/handlers/ritual` call it.

## Responsibilities

- **Slugs** (`slug.go`): `Slugify` derives a spec-conforming name (`[a-z0-9-]`, 1–64, no leading, trailing or double hyphens) from a display name; `ValidateSlug` checks one; `UniqueSlug` picks `base`, `base-2`, … against a caller-supplied `taken` predicate.
- **Sanitizing** (`sanitize.go`): `SanitizeText` strips Unicode tag characters, zero-width characters, bidi controls, the BOM, Hangul fillers and C0 controls (tab, newline and carriage return stay); `RejectTags` refuses XML-looking tags in a name or description.
  A variation selector survives only directly after a visible character (the emoji ❤️); runs of them, which can encode hidden bytes, are removed.
- **Bounds and normalization** (`validate.go`): `Normalize` trims and sanitizes a `models.Ritual` in place, optionally derives its slug, and enforces the limits (`MaxDescriptionLen` 1,024, `MaxBodyBytes` 64 KB, license and compatibility 500, allowed tools and metadata counts and lengths).
  Nil pointer fields are left nil so the caller can mean "unchanged".
  `EstimateTokens` is the rough four-bytes-a-token count the editor and index budget use.
- **SKILL.md** (`frontmatter.go`): `ParseSkillMD` reads a file leniently as the standard's client guide recommends (derived name with a warning, unknown keys kept in metadata, only a missing description or body is an error) and `RenderSkillMD` writes one from a ritual.
  Claude Code's top-level `disable-model-invocation` and `user-invocable` are mapped onto the flags and win over our metadata copies.
  A metadata entry that `Normalize` would refuse (an 80-line `hooks:` block, a key with spaces) is dropped with a warning instead of failing the import, and entries past the count limit are dropped the same way.
  What Iff's extras travel under `metadata`: `display_name`, `whatiff-hotkeys`, `whatiff-connectors` (names), `whatiff-disable-model-invocation`, `whatiff-user-invocable`; parsing consumes them back into fields.
  `display_name` is left out only when import would derive the same name from the slug, so every display name round-trips.
- **Bundles** (`bundle.go`): `ReadBundle` unpacks a zip with SKILL.md at the root or inside one top-level folder, skipping archiver junk and refusing traversal and oversize members (`MaxBundleFiles` 200, `MaxBundleBytes` 30 MB, `MaxBundleFileBytes` 10 MB); `BundleFromFiles` does the same for uploaded parts; `WriteBundle` writes `<slug>/SKILL.md` plus files.

## Key types and entry points

- `Normalize`, `Slugify`, `ValidateSlug`, `UniqueSlug`, `SanitizeText`, `RejectTags`.
- `ParseSkillMD` → `*Parsed` (a `models.Ritual` with every pointer field set, connector names, warnings); `RenderSkillMD`.
- `ReadBundle`, `BundleFromFiles`, `WriteBundle`, `Bundle`, `File`.
- `ErrInvalid` wraps every validation failure, so a handler maps any of them to a 400 with the detail as the message.

## Dependencies

- **Inbound:** `internal/datastore`, `internal/database`, `internal/handlers/ritual`, later `internal/agent` (the skills index and `load_skill`).
- **Outbound:** `internal/models`, `gopkg.in/yaml.v3`, `archive/zip`.

## Non-obvious decisions

- **The ritual row is the source of truth; SKILL.md is a serialization.** Parse on import, render on export and container mount, never store the file.
- **`allowed-tools` renders as the standard's space-delimited string**, so `Normalize` refuses whitespace in an entry except inside parentheses (Claude Code's `Bash(git add:*)`); parsing also accepts a comma-delimited string or a YAML list, and splits only outside parentheses.
- **Sanitizing is on write, not on read**, and files are stored as uploaded; only the text fields are cleaned.

## Testing

- `slug_test.go`, `sanitize_test.go`, `frontmatter_test.go`, `bundle_test.go` — table tests, a parse → render → parse round trip, and zip edge cases (enclosing folder, junk entries, traversal, too many files).

## Related documentation

- [ADR 0x035](../../docs/adr/0x035-agent-skills.md) — the design.
- [Agent Skills specification](https://agentskills.io/specification).
