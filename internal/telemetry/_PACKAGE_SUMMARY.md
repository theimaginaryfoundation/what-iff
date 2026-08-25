# Package: `internal/telemetry`

## Role

**OpenTelemetry** setup: OTLP exporters (traces/metrics), meter and tracer providers, and helpers used when constructing the `Telemetry` value passed into the agent and server.

## Responsibilities

- Initialize global or scoped OTel providers per process configuration.
- **`Telemetry` type** (`telemetry.go`) — holds logger/meter/tracer handles consumed by `internal/agent` (required for `messageContextBuilder` when non-nil logger is mandatory).

## Dependencies

- **Inbound:** `cmd/api-server`, `internal/server`.
- **Outbound:** `go.opentelemetry.io/*`, gRPC exporters.

## Non-obvious decisions

- Agent code assumes telemetry can be non-nil with a **non-nil logger** for chat context construction — see architecture doc and `internal/agent` summary.
- When OTLP is enabled, `MeterProvider` uses a **5-minute** periodic export (`sdkmetric.WithInterval`) and **`WithCardinalityLimit(1000)`** per instrument (overflow aggregates to `otel.metric.overflow`); both are fixed in code, not env.
- `CallPath` values are a controlled low-cardinality enum used for inference labeling; includes delegated subagent traffic (`subagent`) in addition to normal user/job/scratchpad/gate paths.

## Testing

- `metrics_test.go` — metric registration or behavior.
- `telemetry_test.go` — `Init` early-exit when OTLP endpoint unset.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — observability in production stacks.
