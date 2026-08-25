# Package: `internal/handlers/role`

## Role

HTTP API for **user roles** (authorization labels) at `/api/roles/...` — listing and management for admin/support flows.

## Responsibilities

- **`Handler`:** Role routes in `handler.go` backed by datastore `role.go`.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/models`, `mux`.

## Non-obvious decisions

- Role checks for admin routes are enforced in middleware + handler — keep consistent with `internal/middleware` context keys.

## Testing

- *(Add tests when role mutations gain validation.)*

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — auth and admin.
