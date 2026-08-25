# Package: `internal/providers`

## Role

**Narrow provider interfaces** that abstract datastore operations for specific features — e.g. **agent jobs** — so handlers can depend on an interface while tests inject fakes.

## Responsibilities

- **`AgentJobProvider`** (`agentjob.go`): CRUD and schedule updates for `models.AgentJob` scoped by user id.

## Dependencies

- **Inbound:** Handlers that need interface-based DI (`internal/handlers/agentjob` typically implements this via `datastore.Datastore`).
- **Outbound:** `internal/models` only in the interface definition.

## Non-obvious decisions

- **vs `internal/datastore`:** This package defines **contracts**; `datastore` implements them with Ent. Prefer adding methods on `Datastore` and satisfying interfaces here when splitting test doubles.

## Testing

- *(Interface only — tests live on implementations and handlers.)*

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **Data Access** pattern.
