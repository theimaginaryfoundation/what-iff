# Metrics

The API emits OpenTelemetry metrics. Every metric is declared once in
[`internal/telemetry/catalog.go`](../internal/telemetry/catalog.go), with its unit, description and
histogram buckets. This page lists what each one means, its attributes, and what it costs.
Update both when you add or change a metric.

## Turning metrics on

Set `OTEL_METRICS_EXPORTER`:

| Value | What happens |
|---|---|
| `otlp` | Pushes to `OTEL_EXPORTER_OTLP_ENDPOINT` (gRPC) every `OTEL_METRIC_EXPORT_INTERVAL` ms. The default when an endpoint is set. Deployed environments use this, through the ADOT collector, into Managed Prometheus. |
| `prometheus` | Serves `http://localhost:9464/metrics` (`OTEL_EXPORTER_PROMETHEUS_HOST`/`_PORT`). Handy for local work: `curl localhost:9464/metrics \| grep whatiff_`. |
| `console` | Writes each export to stderr as JSON. |
| `none` | The default with no endpoint. Instrumentation records to a no-op provider. |

The export interval defaults to 5 minutes. That's a cost decision, because ingested samples scale
with export frequency. Change it with `OTEL_METRIC_EXPORT_INTERVAL` (milliseconds).

In Prometheus, metric names have dots replaced with underscores and get a unit and `_total`
suffix. For example, `whatiff.job.duration` becomes `whatiff_job_duration_seconds_bucket` and
`whatiff.jobs.enqueued` becomes `whatiff_jobs_enqueued_total`. Attribute keys become labels the
same way (`error.type` becomes `error_type`).

## Conventions

- **Names:** use the OpenTelemetry semantic-convention name where one exists (`http.*`, `db.*`,
  `gen_ai.*`), otherwise `whatiff.<area>.<thing>`.
- **Units:** durations are float seconds.
- **Buckets:** each histogram uses one of the families in
  [`buckets.go`](../internal/telemetry/buckets.go). Never use the SDK default, which stops at 10.
- **Errors:** `error.type` is set only on failures. Its values come from `telemetry.ClassifyError`:
  `canceled`, `timeout`, `rate_limited`, `server_error`, `client_error`, `auth`, `not_found`,
  `network`, `other`. Traffic and error rates come from a histogram's `_count`, so most flows have
  no separate error counter.
- **Label values:** always from a fixed set. Never IDs, URLs, file names, free text, or strings a
  model produced. Tool names are mapped to known tools, `mcp` or `other`. Checkpoint reasons are
  the rule that fired, not the log message.
- **Recording:**
  - Use the `*telemetry.Metrics` a component already has; its methods are no-ops on nil.
  - Otherwise use `telemetry.Global()`.
  - Test with `internal/telemetry/telemetrytest`.

## Catalog

The buckets column refers to the families in `buckets.go`.

### HTTP server

| Metric | Type | Buckets | Attributes |
|---|---|---|---|
| `http.server.request.duration` | histogram, s | HTTP (5ms–30s) | `http.request.method`, `http.route` (mux template), `http.status_class` |

Streaming and polling endpoints record their full duration.

### Outbound dependencies

| Metric | Type | Buckets | Attributes | Recorded by |
|---|---|---|---|---|
| `http.client.request.duration` | histogram, s | slow (0.1–300s) | `dependency`, `http.request.method`, `http.status_class`, `error.type` | `telemetry.NewHTTPTransport` on the shared provider client, local LLM and web search clients |
| `whatiff.dependency.duration` | histogram, s | slow | `dependency`, `operation`, `error.type` | S3/local file store (`storage.Instrument`), email (`email.Instrument`), Parallel web search |
| `db.client.operation.duration` | histogram, s | fast (0.5ms–10s) | `db.collection.name`, `db.operation.name`, `error.type` | ent interceptors and hooks (`datastore.InstrumentEntClient`), plus raw SQL: `file_chunks`/`vector_search` and `scheduler_lock`/`advisory_lock*` |
| `db.client.connection.count` | gauge | — | `db.client.connection.state` (idle, used) | `database.RegisterPoolMetrics` |
| `whatiff.db.pool.waits` | gauge (cumulative) | — | | |
| `whatiff.db.pool.wait_time` | gauge (cumulative), s | — | | |

Notes on these metrics:
- **HTTP client:** there is one sample per HTTP attempt, so SDK retries appear as extra attempts.
  For streamed responses this measures time to the response headers; full LLM call time is in
  `gen_ai.client.operation.duration`.
- **`dependency` values:** `postgres`, `s3`, `local_fs`, `ses`, `parallel`, `openai`, `anthropic`,
  `zai`, `gemini`, `deepseek`, `mistral`, `qwen`, `xiaomi`, `local_llm`, `stripe`, `cognito`,
  `jira`, `fcm`, `c4a`, `other`.
- **DB:**
  - `db.operation.name` is `query`, `create`, `update` or `delete`.
  - An empty result from `First`/`Only` counts as a successful query.
  - Bulk creates and eager-loaded edges count once, as part of the outer call.
- **Pool gauges:** these are sampled at export. The wait metrics are totals since the process
  started, so use `rate()` on them.

### LLM calls

| Metric | Type | Buckets | Attributes |
|---|---|---|---|
| `gen_ai.client.operation.duration` | histogram, s | slow | `gen_ai.provider.name`, `gen_ai.request.model`, `gen_ai.operation.name` (chat, embeddings, generate_image, edit_image, file), `call_path`, `error.type` |
| `whatiff.gen_ai.time_to_first_token` | histogram, s | slow | provider, model, `call_path` |
| `gen_ai.client.token.usage` | histogram, {token} | tokens (16–1M) | provider, `gen_ai.token.type`, `call_path` |
| `whatiff.gen_ai.tokens` | counter, {token} | — | provider, model, `gen_ai.token.type`, `call_path` |
| `whatiff.gen_ai.retries` | counter | — | provider, model, `reason` (rate_limited, server_error, truncated, length) |
| `whatiff.gen_ai.safety_blocks` | counter | — | provider, model, `call_path` |
| `whatiff.gen_ai.context.tokens` | histogram, {token} | tokens | `segment`, `call_path` |

Notes on these metrics:
- **Call duration:**
  - One value per logical call, including app-level retries and their waits.
  - Streams are timed to their final event.
  - The model label stays on error series, so you can see one model failing.
- **Time to first token:** recorded only for streamed calls that succeed, and measured from the
  start of the attempt that succeeded.
- **Token histogram vs. counter:** the histogram has no model label, which keeps its bucket series
  down. Use the counter for per-model totals and cost.
- **Token types:** `input`, `output`, `cached_input` (cache reads, a subset of input) and
  `reasoning` (a subset of output). Don't add a subset to its total.
- **Context tokens:** estimated tokens (cl100k) per context segment sent with a turn.

**`call_path` values:** `user_chat`, `agent_job`, `scratchpad`, `memory`, `conversation_summary`,
`image_ritual`, `chat_name`, `generate_personality`, `expression_grid`, `personality_portrait`,
`schedule_parse`, `subagent`, `mode_select`, `expression_pick`, `unknown`. An `unknown` value is a
labelling gap worth fixing.

### Chat turns and tools

| Metric | Type | Buckets | Attributes |
|---|---|---|---|
| `whatiff.chat.turn.stage.duration` | histogram, s | slow | `stage`, `call_path` (user_chat, agent_job) |
| `whatiff.chat.checkpoints` | counter | — | `reason` (turn_count, last_input_tokens, estimated_context_tokens) |
| `whatiff.chat.checkpoint.context_tokens` | histogram, {token} | tokens | |
| `whatiff.chat.checkpoint.messages` | histogram | counts | |
| `whatiff.chat.context_items.persist_failures` | counter | — | `operation` (create, update) |
| `whatiff.quota.rejections` | counter | — | `call_path` |
| `whatiff.agent.tool.duration` | histogram, s | slow | `tool` (catalog tool name, `mcp`, `other`), `error.type` |
| `whatiff.agent.turn.tool_calls` | histogram | counts | `call_path` |

**Stage values:** `rehydration_wait`, `prepare_context`, `memory_enrichment` (inside
prepare_context), `mood`, `build_context`, `inference`, `expression`, `post_process`, `chat_name`,
`checkpoint_scratchpad`, `checkpoint_memory`, `checkpoint_summary`, `checkpoint_persist`. The
`rehydration_wait` and `expression` stages are recorded only when they actually run.

### Jobs

| Metric | Type | Buckets | Attributes |
|---|---|---|---|
| `whatiff.jobs.enqueued` | counter | — | `job_type` |
| `whatiff.job.duration` | histogram, s | job (0.5s–1h) | `job_type`, `outcome` (success, failed, cancelled, panic, quota, timeout) |
| `whatiff.job.queue.wait` | histogram, s | job | `job_type` |
| `whatiff.job.age_at_status` | histogram, s | job | `job_type`, `status` |
| `whatiff.jobs.in_flight` | up-down counter | — | `job_type` |
| `whatiff.jobs.backlog` | gauge | — | `job_type`, `status` |
| `whatiff.jobs.oldest_age` | gauge, s | — | `job_type`, `status` |

Notes on these metrics:
- **`job_type` values:** `chat_message`, `agent_job_run`, `personality_generation`,
  `expression_grid`, `personality_portrait`, `thread_rehydration`, `chat_import`, `account_import`,
  `account_export`.
- **Age at status:** for chat jobs, `status="inference_complete"` is how long the user waited for
  the answer.
- **Queue wait:** only account imports wait meaningfully today (2 at a time).
- **Backlog gauges:**
  - These cover unfinished jobs across the whole database.
  - Every instance reports the same values, so take the `max` across instances, not the `sum`.
- **Scheduled agent jobs** don't create job rows; see the scheduler metrics below.

### Scheduled agent jobs

| Metric | Type | Buckets | Attributes |
|---|---|---|---|
| `whatiff.scheduler.runs` | counter | — | `outcome` (ran, failed, skipped_overlap, skipped_inactive, skipped_congestion, deferred, load_failed, chat_resolve_failed, misfire) |
| `whatiff.scheduler.run.duration` | histogram, s | job | `outcome` (ran, failed) |
| `whatiff.scheduler.lateness` | histogram, s | job | |

Lateness is how long after the job's planned `next_run_at` a regular firing started. Manual runs
and deferred retries are excluded.

### Files, imports and exports

| Metric | Type | Buckets | Attributes |
|---|---|---|---|
| `whatiff.file.uploads` | counter | — | `kind` (image, text, pdf, audio, other), `outcome` (success, failure) |
| `whatiff.file.size` | histogram, By | bytes (1 KiB–256 MiB) | `operation`, `kind` |
| `whatiff.file.operation.duration` | histogram, s | job | `operation`, `stage`, `error.type` |
| `whatiff.file.operation.items` | histogram | counts | `operation`, `kind`, `outcome` (imported, skipped, failed, exported) |

The operation and stage sets are defined in
[`internal/telemetry/files.go`](../internal/telemetry/files.go):
- **Operations:** `upload`, `chat_import`, `chat_export`, `memory_import`, `account_import`,
  `account_export`.
- **Upload counting:** uploads are counted once per upload across the chat, personality and gallery
  paths.

### App

| Metric | Type | Attributes |
|---|---|---|
| `whatiff.app.startups` | counter | |

## Cost

Managed Prometheus bills per ingested sample, at $0.90 per 10M for the first 2B a month. A
histogram costs (buckets + 2) samples per label combination per export; a counter or gauge costs 1.
At a 5-minute interval, one sample slot costs about 8,760 samples a month.

These are the estimates of steady-state sample slots per instance, based on realistic label
combinations:

| Area | Slots |
|---|---|
| HTTP server (~150 routes × ~1.5 status classes × 14) | ~3,100 |
| Outbound: DB, HTTP client, dependencies, pool | ~3,250 |
| LLM calls and tokens | ~4,100 |
| Jobs, chat stages, tools, scheduler | ~2,200 |
| Files, imports and exports | ~960 |
| **Total** | **~13,600** |

That comes to about 119M samples a month, or roughly $11 per instance-month. Levers, if it grows:
- **Drop labels:** remove a label that doesn't change a decision.
- **Change a family:** move a histogram to a family with fewer buckets.
- **Native histograms:** Managed Prometheus bills a populated native (exponential) histogram bucket
  at 0.25 samples, and empty buckets are free. Moving to exponential histograms could cut
  histogram cost a lot, if the collector's remote-write path supports them.

The per-instance cardinality limit is 1,000 attribute sets per metric. Anything beyond that is
folded into an `otel.metric.overflow` series.

## Queries

Some example PromQL:

```promql
# p95 LLM call latency by model, 1h window
histogram_quantile(0.95, sum by (le, gen_ai_request_model) (rate(gen_ai_client_operation_duration_seconds_bucket[1h])))

# LLM error ratio by model and error type
sum by (gen_ai_request_model, error_type) (rate(gen_ai_client_operation_duration_seconds_count{error_type!=""}[1h]))
  / ignoring(error_type) group_left sum by (gen_ai_request_model) (rate(gen_ai_client_operation_duration_seconds_count[1h]))

# Time until a chat answer is ready (p50/p95)
histogram_quantile(0.95, sum by (le) (rate(whatiff_job_age_at_status_seconds_bucket{job_type="chat_message",status="inference_complete"}[1h])))

# Tokens per hour by model and type
sum by (gen_ai_request_model, gen_ai_token_type) (increase(whatiff_gen_ai_tokens_total[1h]))

# Oldest unfinished job (take max across instances)
max by (job_type) (whatiff_jobs_oldest_age_seconds)
```

With a 5-minute export, use rate windows of at least 15 minutes.
