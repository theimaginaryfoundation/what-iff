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
- `Usage.MessageID` is a plain fact the agent owns (the assistant message the turn produced), not metering data.
  It exists so an implementation can persist a link from a recorded turn to the message whose Context X-ray it paid for; `NoopMeter` ignores it and the core reads it back nowhere.
- `source.go` carries where a turn was started from: the entry points set it (`WithTurnSource`: the app, the webhook, or a plugin's own name such as `discord` from `plugins.UserTurn.Source`), the agent copies it onto the turn's own context, and an implementation reads it in `Check` (`TurnSourceFromContext`) and gets it in `Usage.Source`, so turns strangers start through a relay can be metered apart from the owner's own chat traffic.
- Reasoning effort and the Advanced loop (ADR 0x026) are plain facts on `Usage`: `ReasoningEffort` (set only when the user selected an effort, the model honours it, and it is above the model's native level, so a lower effort never costs more than none), `AgentLoop`, `Calls` (each model call's `CallUsage`: model, input, cached input, cache writes, output and reasoning tokens) with `CallsReported` (Calls is complete; do not price `Tokens` on top), and `Step` / `TurnID` (an Advanced run is recorded once per finished step plus a final record, all with the turn's gate Decision; all carry the turn's id).
  `Decision.Err` tells a Check that could not decide from a quota refusal.
  `AdvancedLimiter` (`limits.go`) is an optional Meter capability that sets Advanced limits per account.
  `WithPendingUsage` / `PendingUsageFromContext` (`source.go`) carry a running Advanced turn's not-yet-recorded calls into the `Check` it makes before each further model call, so a meter can stop the run before it spends past the quota.
  `WithAgentLoop` / `AgentLoopFromContext` (`source.go`) let `Check` see an Advanced turn before it runs.
  The public build prices none of it.
- `New` is intentionally nil when no implementation is linked.
  `Agent.NewAgent` then chooses `NoopMeter`, so removing the production meter does not require agent or server changes.

## Testing

- Contract behavior is covered through `internal/agent` tests; add direct tests here when the no-op behavior changes.

## Related

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — HTTP layer and agent boundaries.
