# Package: `internal/server`

## Role

HTTP **API server**: Gorilla mux router, OpenTelemetry/metrics middleware, CORS, dependency construction (datastore, S3, agent, scheduler), and registration of all `internal/handlers/*` routes under `/api`.

## Responsibilities

- **`Server`:** Holds config, logger, telemetry, Ent client, `sql.DB`, HTTP server, and optional **agent job scheduler** lifecycle.
- **`NewServer`:** Builds router, middleware (`setupMiddleware`), routes (`setupRoutes`), timeouts from `Config`.
- **`setupRoutes`:** Creates `datastore.Datastore`, `storage.FileStore`, a `metering.Meter` (production implementation when linked, otherwise no-op), `agent.Agent`, scheduler (`internal/agentjobs/scheduler` when enabled), then mounts handlers: health, chat, memory, personality, ritual, job, file attachment, user, model, agent job, MCP, role, etc. — a linked plugin may register additional routes (see `internal/plugins`). Sub-routers use `/api` prefix; some routes use `/api/v1/...` (see `server.go` comments).
- **`Config` (`config.go`):** Env-driven environment name (`ENV`, then `ENVIRONMENT`), host/port, CORS, OpenAI/Anthropic keys, S3, Stripe keys, **agent job scheduler** flags (in-process vs distributed lock), free-tier message limit, token encryption secret, and a `RequireBilling` feature flag — consumed by a private billing implementation when one is linked; a no-op otherwise.

## Key types and entry points

| Symbol | Notes |
|--------|--------|
| `Server` | `ListenAndServe`, `Shutdown`, router access if needed. |
| `NewServer` | Single composition root for the API process. |
| `Config` / `NewConfig` | Environment wiring for containerized/local deployment. |

## Dependencies

- **Inbound:** `cmd/api-server` constructs `Server`.
- **Outbound:** Nearly all of `internal/*` — handlers, datastore, agent, middleware, storage, telemetry, scheduler.

## Non-obvious decisions

- **Explicit-ENV gating (ADR 0x018):** `Config` captures whether `ENV`/`ENVIRONMENT` was explicitly present (`EnvironmentExplicit`, empty string ≠ set) and whether the two conflict (`EnvironmentConflict`). `IsExplicitLocalEnv()` is the only gate allowed to enable a non-vendor `LLMBackend` (`mock`/`local`) or `DESTRUCTIVE_MIGRATION` — the parsed `Environment` defaults to `development` when unset, which would be fail-open. `cmd/api-server` fatals on conflict, an unrecognized `LLM_BACKEND`, `local` without `LOCAL_LLM_MODEL`, or a non-vendor backend / destructive flag outside an explicit local/test env.
- **LLM backend wiring:** `Config.LLMBackend` (`vendor` default / `mock` / `local`, env `LLM_BACKEND`) plus `LocalLLMBaseURL`/`LocalLLMModel`. When non-vendor, `setupRoutes` builds `provider.DenyNetworkHTTPClient()` and threads it into `agent.AgentConfig.HTTPClient`, the memory handler constructor, and the embedder handed to plugins through `plugins.Deps.CreateEmbedding`, so every provider SDK client is egress-denied at the transport (the local backend's own client is constructed separately inside `agent.NewAgent` with real egress).
- **Metering wiring:** `setupRoutes` calls `metering.New(dataStore, logger)` when it is non-nil. The private metering implementation registers `metering.New` via `init()` (reading its own configuration from the environment) and is linked by `cmd/api-server`; when omitted (e.g. the open-source build), `metering.New` is nil and `Agent.NewAgent` falls back to `metering.NoopMeter`.
- **Scheduler:** `EnableAgentJobsScheduler`, `AgentJobsSchedulerDistributed`, advisory lock key and retry tuning — only one leader should run scheduled jobs in multi-instance deployments.
- **Route ordering:** Comments in `server.go` document mux precedence (specific paths before `/{id}` patterns).
- **HTTP metrics:** `otelmux` uses a **noop** `MeterProvider` so it does not emit semconv HTTP histograms (body size ×2 + duration); `metricsMiddleware` alone records `http_server_request_duration` with **`http.route`** = mux path template (normalized) or path regexp, else **`unmatched`** — never the raw URL path (avoids ID-shaped cardinality); **`http.status_class`** is `1xx`–`5xx` or `other`.

## Testing

- `config_test.go` — configuration parsing behavior, including explicit-ENV/`LLM_BACKEND` gating cases.
- `server_test.go` — `httpStatusClass`, route normalization / `metricHTTPRoute`.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **Backend**, **Infra & Operations**.
