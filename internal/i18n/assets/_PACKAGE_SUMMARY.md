# Package: `internal/i18n/assets`

## Role

**Embedded translation bytes** for the `i18n` package: `en.toml` and `en-internal.toml` exposed as `//go:embed` variables consumed at compile time.

## Responsibilities

- No runtime logic — only `embed` FS or byte slices for `internal/i18n` to parse.

## Dependencies

- **Inbound:** `internal/i18n` only.
- **Outbound:** None beyond `embed`.

## Non-obvious decisions

- Editing TOML here affects **all** locales merged into English; avoid duplicate keys across the two files.

## Testing

- *(Covered indirectly via `internal/i18n/bundle_test.go`.)*

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md); parent package [`internal/i18n`](../_PACKAGE_SUMMARY.md).
