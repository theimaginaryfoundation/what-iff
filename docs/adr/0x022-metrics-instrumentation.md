# ADR 0x022: Metrics instrumentation conventions and cost budget

- **Status:** Accepted
- **Date:** 2026-09-26
- **Deciders:** What Iff maintainers

## Context

The API had OpenTelemetry wired up (OTLP to an ADOT collector, then Amazon Managed Prometheus and Grafana), but hardly any metrics went through it.
There were ten metric names in total, with no dashboards or alerts defined anywhere.
What existed was hard to use:

1. **Histograms were flat.**
   Every histogram used the SDK's default buckets, which stop at 10.
   Durations were recorded as integer milliseconds, so every call between 10ms and 10s shared a handful of buckets, and everything slower landed in the overflow bucket.
   Most backend work (LLM calls, chat turns, checkpoints) takes 1–60s, so latency graphs were useless.
   Token counts used the same default buckets and had the same problem.
2. **Coverage was sparse.**
   No outbound call was timed except inference.
   The database, S3, email, web search and the job system reported nothing.
   Token metrics had no provider or model label, and one metric name (`token_count`) carried three different meanings.
3. **Naming was ad hoc.**
   Names, units and label keys followed no pattern (`_total` on some counters, `_count` on histograms, `http.method` next to `token_io`).
   A label on one metric took model-generated tool names, so its cardinality was unbounded.
4. **Only some code could record.**
   Handlers, the scheduler, storage, web search and the private plugins had no metrics handle.

The project runs on a small budget.
A monitoring bill over roughly $100 a month would hurt, so cost is a design constraint, not an afterthought.

## Decision

Instrument the API broadly, under the conventions below.
The full catalog, with every metric, its attributes and example queries, is in [`docs/metrics.md`](../metrics.md).
That file and [`internal/telemetry/catalog.go`](../../internal/telemetry/catalog.go) are the source of truth.
This ADR records why they look the way they do.

### Conventions

- **Declare every metric once.**
  `catalog.go` declares each metric with its name, unit, description and bucket family.
  Call sites refer to those declarations, never to string names, so a metric can't drift between call sites or be created twice with different shapes.
- **Names.**
  Use the OpenTelemetry semantic-convention name where one exists: `http.server.request.duration`, `http.client.request.duration`, `db.client.operation.duration`, `gen_ai.client.operation.duration`, `gen_ai.client.token.usage`.
  Everything else is `whatiff.<area>.<thing>`.
  The Prometheus export turns dots into underscores and adds unit and `_total` suffixes.
- **Units.**
  Durations are float seconds, which is the semantic-convention unit, and they keep sub-millisecond precision for database calls.
- **Buckets.**
  Every histogram picks one of seven explicit families in `internal/telemetry/buckets.go`: HTTP, fast, slow, job, tokens, bytes and counts.
  Each family is sized to the range its metrics actually span.
  The slow family, used for LLM and outbound calls, has 18 boundaries from 0.1s to 300s, packed densely between 1s and 60s.
  Families are kept short, because every bucket costs a series (see Cost).
- **Errors.**
  `error.type` is set only on failures, and its values come from a fixed set (`canceled`, `timeout`, `rate_limited`, `server_error`, `client_error`, `auth`, `not_found`, `network`, `other`), produced by `telemetry.ClassifyError`.
  Success and failure share one histogram, so traffic and error rates both come from its `_count` and most flows need no separate error counter.
  SDK error types register how to extract their HTTP status (`RegisterStatusCodeFunc`), so classification is status-based everywhere.
- **Label values.**
  Label values always come from a fixed set.
  Never IDs, URLs, file names, free text, or strings a model produced.
  Where a source isn't bounded, it's mapped:
  - Tool names become a known tool name, `mcp` or `other`.
  - A checkpoint's reason becomes the rule that fired, not its log message.
  - MIME types become a coarse file kind.
- **Model labels.**
  The model label is allowed on LLM metrics, because the model catalog bounds it.
  It stays on call-duration error series, so a single misbehaving model is visible.
  It's left off the bucketed token histogram; a token counter carries per-model totals at one sample per series.
- **Recording.**
  Every `*telemetry.Metrics` method is a no-op on a nil receiver, so optional wiring needs no checks.
  Code that isn't handed a recorder uses `telemetry.Global()`, which lets subsystems and plugins record without threading new constructor parameters.
  `internal/telemetry/telemetrytest` records in memory, so tests can assert what was emitted.
- **Metrics only.**
  Tracing isn't expanded, to avoid per-trace cost in X-Ray.
  The existing server spans are unchanged.

### Exporters

`OTEL_METRICS_EXPORTER` selects the exporter:
- `otlp` pushes to the collector. It's the default when an endpoint is set, and what deployments use.
- `prometheus` serves `/metrics` locally.
- `console` writes each export to stderr.
- `none` is the default otherwise, and makes instrumentation cost nothing.

The open-source build therefore works with no hosted services, and local debugging is `curl localhost:9464/metrics`.

The export interval stays at **5 minutes**, which is a cost decision (see Cost).
`OTEL_METRIC_EXPORT_INTERVAL` overrides it, for example for local debugging.

### What's instrumented

- **Outbound calls:**
  - Every ent query and mutation, labelled by entity and operation, plus raw SQL and connection pool gauges.
  - Every vendor HTTP attempt, through one shared instrumented transport, so SDK retries appear as extra attempts.
  - S3 and the local file store, email, and web search.
- **LLM calls:**
  - Logical call duration, including app-level retries.
  - Time to first token for streamed calls.
  - Input, output, cached-input and reasoning tokens.
  - Retries and safety blocks.
  - Embeddings, images and files.
- **Jobs:**
  - Enqueue counts, age at each status change, run time and outcome, in-flight counts, and backlog and oldest-age gauges.
  - Chat turn stage timings, checkpoint reasons, quota rejections, and per-tool durations.
  - Scheduled agent job outcomes, durations and lateness.
- **Heavy file operations:** sizes, phase durations and item counts for uploads, chunking, imports and exports.

**Streaming responses** record their whole duration, from request to the final event.
Completion time matters more than time to first byte for this product.
Time to first token is recorded separately for streamed LLM calls.
The HTTP attempt metric only sees time to response headers on streams, which its documentation says.

## Options considered

- **Exponential (native) histograms instead of explicit buckets.**
  Managed Prometheus bills a populated native-histogram bucket at 0.25 samples, and empty buckets are free.
  That would make histograms much cheaper and give finer resolution.
  Rejected for now: the collector's remote-write support for them and Grafana's handling weren't verified.
  It's the first cost lever to revisit.
- **A shorter export interval (60s or less).**
  Rejected.
  Samples ingested scale linearly with export frequency, so 60s would cost 5× as much.
  Five-minute resolution is enough to see trends, capacity and regressions, and alert windows can be sized for it.
- **Contrib instrumentation (`otelhttp`, `otelsql`).**
  Rejected in favour of small in-repo wrappers:
  - The HTTP transport and ent interceptors control exactly which labels exist.
  - They apply our bucket families.
  - They label by dependency and entity rather than raw host or statement.
  - They emit no spans.
  - The contrib packages default to buckets that stop at 10s and add request/response size histograms that cost series without answering a question we have.
- **Per-status-code labels.**
  Rejected in favour of status class (`2xx`–`5xx`) plus `error.type`, which give the same operational signal with far fewer series.
- **A model label on every LLM metric.**
  Rejected for the token histogram, where it would multiply bucket series.
  The token counter carries it cheaply.
- **Threading a `*Metrics` through every constructor.**
  Rejected in favour of `telemetry.Global()` for components that don't already hold one.
  OpenTelemetry's own API is global in the same way.
  Tests that use the global recorder swap it with `telemetrytest.UseGlobal`.

## Cost

Managed Prometheus bills per ingested sample: $0.90 per 10M for the first 2B a month (checked 2026-09-26).
Each export sends:
- one sample per series for a counter or gauge
- (buckets + 2) samples per label combination for a histogram, counting the `+Inf` bucket, `_sum` and `_count`

At a 5-minute interval, one sample slot costs about 8,760 samples a month, or about $0.00079.

These are the estimates of steady-state slots per instance, from realistic label combinations rather than the theoretical cross product:

| Area | Slots |
|---|---|
| HTTP server | ~3,100 |
| Outbound (DB, HTTP client, dependencies, pool) | ~3,250 |
| LLM calls and tokens | ~4,100 |
| Jobs, chat stages, tools, scheduler | ~2,200 |
| Files, imports and exports | ~960 |
| **Total** | **~13,600** |

That comes to about 119M samples a month, or roughly **$11 per instance-month**.
Storage ($0.03/GB-month) and query costs ($0.10 per billion samples processed) are small at this volume.
Two instances in production plus dev comes to roughly $30–35 a month, well inside the budget.

**Budget policy.**
- **Estimate new metrics:** any new metric or label comes with a slot estimate in its PR (label combinations × samples per combination).
- **Keep a margin:** the total should stay comfortably under what $100 a month buys, about 1.1B samples a month across all instances.
- **Know the cap:** the per-instrument cardinality limit (1,000 attribute sets per instance) stays as a safety net; anything above it is folded into an `otel.metric.overflow` series.

**Levers if cost grows**, in order:
1. Native histograms.
2. Dropping a label that doesn't change a decision.
3. Moving a histogram to a shorter bucket family.
4. Dropping the per-task `hostname` resource label at the collector, if series churn on deploys becomes noticeable.

## Consequences

- **Breaking rename.**
  Every metric has a new name, unit or labels, and existing Grafana panels need rebuilding.
  That was accepted because there was only a handful of panels.
- **Coarse resolution.**
  Five-minute resolution limits how fast alerts can fire: rate windows should be at least 15 minutes, and very short incidents may not show up.
  Paging on sub-5-minute blips is out of scope for this setup.
- **Backlog gauges.**
  These are database-wide, so every instance reports the same values; dashboards take the `max`, not the `sum`.
- **Nested DB calls.**
  Bulk creates and eager-loaded edges count once, as part of the outer call.
- **Scheduled agent jobs** create no job rows, so they have their own scheduler metrics rather than job lifecycle metrics.
- **Maintenance.**
  Adding a metric now means a catalog entry, a bounded label set, a test with `telemetrytest`, a row in `docs/metrics.md`, and a cost estimate.
  That's a small amount of ceremony that keeps the catalog honest.

## Follow-ups

- **Private overlay:** instrument Stripe, Jira, c4a, Cognito and push notifications with the same helpers.
- **Collector:** add batch and memory-limit processors to the ADOT config, and remove the unused `AMP_METRICS_NAMESPACE` setting.
- **Dashboards as code:** define dashboards, recording rules and SLO burn-rate alerts in the private infrastructure repo.
- **Native histograms:** verify end-to-end support and switch the heaviest histograms.
