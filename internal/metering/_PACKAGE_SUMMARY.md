# Package: `internal/metering`

## Role

Defines the implementation-independent metering contract used by the agent.

## Responsibilities

- Exposes `Meter`, `Decision`, and `Usage` so agent turns can be gated before inference and recorded afterwards without importing any implementation.
- Provides `NoopMeter`, the allow-all/no-recording fallback for builds with no metering implementation.
- Defines the optional `New` factory (`func(ds, logger) Meter`) used to wire a production implementation; the implementation owns its own configuration (read from the environment).

## Key types and entry points

- `Meter` — concurrent-safe `Check`/`Record` boundary around a metered turn.
- `Decision` — opaque implementation state round-tripped from `Check` to `Record`.
- `NoopMeter` — default when no production meter is linked.
- `New` — set by the private metering implementation at init time when it is linked.

## Dependencies

- **Inbound:** `internal/agent` performs checks and recording; `internal/server` wires the meter; the private metering implementation registers the production factory.
- **Outbound:** `internal/datastore` and `zap` appear only in the factory signature and no-op logging.

## Non-obvious decisions

- The agent reads only `Decision.Allowed` and `Decision.FreeChat`; `State` remains owned by the implementation.
- `New` is intentionally nil when no implementation is linked. `Agent.NewAgent` then chooses `NoopMeter`, so removing the production meter does not require agent or server changes.

## Testing

- Contract behavior is covered through `internal/agent` tests; add direct tests here when the no-op behavior changes.

## Related

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — HTTP layer and agent boundaries.
