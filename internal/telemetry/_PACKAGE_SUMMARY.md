# Package: `internal/telemetry`

## Role

**OpenTelemetry** setup: OTLP exporters (traces/metrics), meter and tracer providers, and helpers used when constructing the `Telemetry` value passed into the agent and server.

## Responsibilities

- Initialize global or scoped OTel providers per process configuration.
- **`Telemetry` type** (`telemetry.go`) — holds logger/meter/tracer handles consumed by `internal/agent` (required for `messageContextBuilder` when non-nil logger is mandatory).
- **`files.go`** — bounded operation, stage, item-kind and outcome constants for the file metrics, plus helpers (`RecordFileSize`, `TimeFileStage`, `RecordFileItems`, `RecordFileUpload`) used by uploads, imports and exports.
- **`httptransport.go`** — `HTTPTransport` / `InstrumentHTTPClient` wrap an `http.RoundTripper` to record `http.client.request.duration` per attempt (so SDK retries are separate samples) with `dependency` from the request host (built-in vendor hosts plus `WithDependencyHost` overrides, else `other`), a bounded method, `http.status_class` and `error.type`.
  A RoundTripper returns at response headers, so streamed LLM responses record time to headers; whole-call LLM time comes from the gen_ai metrics.

## Dependencies

- **Inbound:** `cmd/api-server`, `internal/server`.
- **Outbound:** `go.opentelemetry.io/*`, gRPC exporters.

## Non-obvious decisions

- Agent code assumes telemetry can be non-nil with a **non-nil logger** for chat context construction — see architecture doc and `internal/agent` summary.
- When OTLP is enabled, `MeterProvider` uses a **5-minute** periodic export (`sdkmetric.WithInterval`) and **`WithCardinalityLimit(1000)`** per instrument (overflow aggregates to `otel.metric.overflow`); both are fixed in code, not env.
- `CallPath` values are a controlled low-cardinality enum used for inference labeling; includes delegated subagent traffic (`subagent`) and per-turn side calls (`mode_select`, `expression_pick`) in addition to normal user/job/scratchpad paths.

## Testing

- `metrics_test.go` — metric registration or behavior.
- `telemetry_test.go` — `Init` early-exit when OTLP endpoint unset.
- `httptransport_test.go` — status class, error type, host-to-dependency mapping and retries as separate attempts.

## Related documentation

- [Metrics catalog](../../docs/metrics.md) — every metric, its attributes, exporters, cost model and example queries.
- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — observability in production stacks.
