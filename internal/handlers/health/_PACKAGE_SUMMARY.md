# Package: `internal/handlers/health`

## Role

**Liveness/readiness** endpoint for load balancers: `GET /api/health` checks process and database connectivity within ALB timeout budget.

## Responsibilities

- **`Handler.Check`:** JSON response with per-check status map; DB ping uses short timeout (`dbPingTimeout` — must stay under ALB health check deadline, see const in `handler.go`).

## Dependencies

- **Inbound:** `internal/server` registers `/api/health` on the API subrouter.
- **Outbound:** `database/sql`, `zap`.

## Non-obvious decisions

- **3s DB ping cap** vs common 5s ALB timeout — documented in `handler.go`.

## Testing

- `handler_test.go` — response shape and checks.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — **Infra** (ALB, ECS).
