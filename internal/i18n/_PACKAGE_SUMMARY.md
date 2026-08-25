# Package: `internal/i18n`

## Role

**Internationalization** for API and log strings: English TOML bundles embedded at compile time, exposed via `go-i18n` helpers (`T`, `T1`, …).

## Responsibilities

- Load **`en.toml`** (customer-facing) and **`en-internal.toml`** (structured log messages) from `internal/i18n/assets` at `init()`.
- Merge rules: both registered as `en.toml` virtual names so keys merge; **duplicate keys panic at startup** (see package comment in `bundle.go`).

## Key types and entry points

- `T`, `T1`, etc. — see `bundle.go` for exact API.

## Dependencies

- **Inbound:** Handlers and middleware for user-visible errors; logging code for internal templates.
- **Outbound:** `internal/i18n/assets`, `github.com/nicksnyder/go-i18n/v2`, TOML.

## Non-obvious decisions

- Package doc in `bundle.go` is the canonical description of the two-file split and merge semantics.

## Testing

- `bundle_test.go` — lookup behavior.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — product-facing API behavior.
