# Package: `internal/logging`

## Role

Process-wide **zap** logger construction: development vs production config, `LOG_LEVEL` override, shared from `cmd/api-server` startup.

## Responsibilities

- **`NewLogger`:** Reads `ENV` and `LOG_LEVEL`, returns configured `*zap.Logger`.

## Dependencies

- **Inbound:** `cmd/api-server`, Lambdas, workers.
- **Outbound:** `go.uber.org/zap` only.

## Non-obvious decisions

- `ENV=dev` / `development` selects development encoder; otherwise production JSON.

## Testing

- *(No dedicated tests — thin wrapper; change carefully.)*

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — operations/logging expectations.
