package telemetry

// Metric catalog: every metric the API emits is declared here, with its unit, description and
// bucket family. docs/metrics.md lists each one with its attributes and expected series count;
// update it with any change here.
//
// Naming: OpenTelemetry semantic-convention names where one exists (http.*, db.*, gen_ai.*),
// otherwise whatiff.<area>.<thing>. Durations are float seconds ("s"). The Prometheus export
// turns dots into underscores and adds unit and _total suffixes, e.g.
// whatiff.job.duration -> whatiff_job_duration_seconds.

// Histogram declares a histogram. Values are float64: seconds for durations, raw numbers for
// tokens, bytes and counts.
type Histogram struct {
	Name        string
	Unit        string
	Description string
	Buckets     []float64
}

// Counter declares a monotonic counter.
type Counter struct {
	Name        string
	Unit        string
	Description string
}

// UpDownCounter declares a value that goes up and down, such as work in flight.
type UpDownCounter struct {
	Name        string
	Unit        string
	Description string
}

// Gauge declares an observable gauge sampled at export time (see Metrics.RegisterGauges).
type Gauge struct {
	Name        string
	Unit        string
	Description string
}

const unitSeconds = "s"

// --- App ---

var AppStartups = Counter{"whatiff.app.startups", "{startup}", "Process starts."}

// --- HTTP server ---

// HTTPServerDuration is API request latency. Attributes: http.request.method, http.route
// (mux template), http.status_class. Streaming/polling endpoints record their full duration.
var HTTPServerDuration = Histogram{"http.server.request.duration", unitSeconds, "Duration of API requests.", BucketsHTTP}

// --- Outbound dependencies ---

// HTTPClientDuration is one outbound HTTP attempt (so SDK retries show as extra attempts).
// Attributes: dependency, http.request.method, http.status_class, error.type.
var HTTPClientDuration = Histogram{"http.client.request.duration", unitSeconds, "Duration of outbound HTTP attempts, including retries.", BucketsSlow}

// DependencyDuration is one logical call to a dependency that isn't covered by a more specific
// metric (S3, SES, web search, Stripe...). Attributes: dependency, operation, error.type.
var DependencyDuration = Histogram{"whatiff.dependency.duration", unitSeconds, "Duration of calls to external dependencies.", BucketsSlow}

// DBOperationDuration is one ent query or mutation, or one raw SQL statement. Attributes:
// db.collection.name (entity/table), db.operation.name (query, create, update, delete, raw
// statement name), error.type.
var DBOperationDuration = Histogram{"db.client.operation.duration", unitSeconds, "Duration of database operations.", BucketsFast}

// DB connection pool state, sampled at export time from sql.DBStats.
var (
	DBPoolConnections = Gauge{"db.client.connection.count", "{connection}", "Open database connections by state (idle, used)."}
	DBPoolWaits       = Gauge{"whatiff.db.pool.waits", "{wait}", "Total waits for a free database connection since start."}
	DBPoolWaitTime    = Gauge{"whatiff.db.pool.wait_time", unitSeconds, "Total time spent waiting for a free database connection since start."}
)

// --- LLM calls ---

// GenAIOperationDuration is one logical LLM call (all retries included). Attributes:
// gen_ai.provider.name, gen_ai.request.model, gen_ai.operation.name (chat, embeddings,
// generate_image, edit_image, file), call_path, error.type.
var GenAIOperationDuration = Histogram{"gen_ai.client.operation.duration", unitSeconds, "Duration of LLM calls, including retries.", BucketsSlow}

// GenAITimeToFirstToken is time from request to the first streamed output (text or reasoning).
// Attributes: gen_ai.provider.name, gen_ai.request.model, call_path.
var GenAITimeToFirstToken = Histogram{"whatiff.gen_ai.time_to_first_token", unitSeconds, "Time from an LLM request to its first streamed output.", BucketsSlow}

// GenAITokenUsage is the distribution of tokens per call. Attributes: gen_ai.provider.name,
// gen_ai.token.type, call_path. No model label, to keep bucket series down; per-model totals
// come from GenAITokens.
var GenAITokenUsage = Histogram{"gen_ai.client.token.usage", "{token}", "Tokens per LLM call, as reported by the provider.", BucketsTokens}

// GenAITokens totals tokens by model for cost and rate panels (one series per label set, no
// buckets). Attributes: gen_ai.provider.name, gen_ai.request.model, gen_ai.token.type, call_path.
var GenAITokens = Counter{"whatiff.gen_ai.tokens", "{token}", "Tokens used, as reported by the provider."}

// GenAIRetries counts app-level retries and fallbacks of LLM calls. Attributes:
// gen_ai.provider.name, gen_ai.request.model, reason (rate_limited, server_error, truncated...).
var GenAIRetries = Counter{"whatiff.gen_ai.retries", "{retry}", "LLM call retries and fallbacks."}

// GenAISafetyBlocks counts responses refused by provider safety systems. Attributes:
// gen_ai.provider.name, gen_ai.request.model, call_path.
var GenAISafetyBlocks = Counter{"whatiff.gen_ai.safety_blocks", "{block}", "LLM responses blocked by provider safety systems."}

// GenAIContextTokens is the estimated size of each context segment sent with a turn.
// Attributes: segment, call_path.
var GenAIContextTokens = Histogram{"whatiff.gen_ai.context.tokens", "{token}", "Estimated tokens per model context segment.", BucketsTokens}

// --- Agent tools ---

// ToolDuration is one tool call. Attributes: tool (a known tool name, "mcp" or "other"),
// error.type. Its count doubles as the tool call rate.
var ToolDuration = Histogram{"whatiff.agent.tool.duration", unitSeconds, "Duration of agent tool calls.", BucketsSlow}

// ToolCallsPerTurn is the number of tool calls in one generation. Attributes: call_path.
var ToolCallsPerTurn = Histogram{"whatiff.agent.turn.tool_calls", "{call}", "Tool calls per generation.", BucketsCount}

// --- Jobs ---

// JobsEnqueued counts jobs created. Attributes: job_type.
var JobsEnqueued = Counter{"whatiff.jobs.enqueued", "{job}", "Jobs created."}

// JobDuration is how long a job ran, from start to finish. Attributes: job_type, outcome
// (success, failed, cancelled, panic, quota, timeout).
var JobDuration = Histogram{"whatiff.job.duration", unitSeconds, "Job run time.", BucketsJob}

// JobQueueWait is time from creation until a job started running (only meaningful where work
// actually waits, e.g. account imports and scheduled runs). Attributes: job_type.
var JobQueueWait = Histogram{"whatiff.job.queue.wait", unitSeconds, "Time jobs waited before starting.", BucketsJob}

// JobAgeAtStatus is time from job creation to each status change, e.g. how long until a chat
// turn's answer is ready (inference_complete). Attributes: job_type, status.
var JobAgeAtStatus = Histogram{"whatiff.job.age_at_status", unitSeconds, "Time from job creation to each status change.", BucketsJob}

// JobsInFlight is jobs running in this process. Attributes: job_type.
var JobsInFlight = UpDownCounter{"whatiff.jobs.in_flight", "{job}", "Jobs running in this process."}

// Unfinished jobs in the database, sampled at export time. Attributes: job_type, status.
var (
	JobsBacklog   = Gauge{"whatiff.jobs.backlog", "{job}", "Unfinished jobs by type and status."}
	JobsOldestAge = Gauge{"whatiff.jobs.oldest_age", unitSeconds, "Age of the oldest unfinished job by type and status."}
)

// --- Chat turns ---

// ChatTurnStageDuration is one stage of a chat turn (prepare_context, inference, checkpoint...).
// Attributes: stage, call_path (user_chat or agent_job).
var ChatTurnStageDuration = Histogram{"whatiff.chat.turn.stage.duration", unitSeconds, "Duration of chat turn stages.", BucketsSlow}

// ChatCheckpoints counts conversation checkpoints (compaction). Attributes: reason.
var ChatCheckpoints = Counter{"whatiff.chat.checkpoints", "{checkpoint}", "Conversation checkpoints run."}

// ChatCheckpointContextTokens is the estimated context size when a checkpoint runs.
var ChatCheckpointContextTokens = Histogram{"whatiff.chat.checkpoint.context_tokens", "{token}", "Estimated context tokens when a checkpoint runs.", BucketsTokens}

// ChatCheckpointMessages is the number of user messages in the segment a checkpoint compacts.
var ChatCheckpointMessages = Histogram{"whatiff.chat.checkpoint.messages", "{message}", "User messages compacted per checkpoint.", BucketsCount}

// ChatContextItemsPersistFailures counts failed bulk inserts of chat message context items.
// Attributes: operation (create, update).
var ChatContextItemsPersistFailures = Counter{"whatiff.chat.context_items.persist_failures", "{failure}", "Failed saves of chat message context items."}

// QuotaRejections counts work refused because the user is out of quota. Attributes: call_path.
var QuotaRejections = Counter{"whatiff.quota.rejections", "{rejection}", "Requests refused for quota."}

// --- Scheduled agent jobs ---

// SchedulerRuns counts scheduled agent job firings by outcome (ran, failed, skipped_overlap,
// skipped_congestion, deferred, misfire...).
var SchedulerRuns = Counter{"whatiff.scheduler.runs", "{run}", "Scheduled agent job firings by outcome."}

// SchedulerRunDuration is how long a scheduled agent job ran. Attributes: outcome.
var SchedulerRunDuration = Histogram{"whatiff.scheduler.run.duration", unitSeconds, "Scheduled agent job run time.", BucketsJob}

// SchedulerLateness is how long after its scheduled time a run started.
var SchedulerLateness = Histogram{"whatiff.scheduler.lateness", unitSeconds, "Delay between a scheduled time and the run starting.", BucketsJob}

// --- Files and heavy operations ---

// FileUploads counts file attachment uploads. Attributes: kind (image, text, pdf, audio,
// other), outcome (success, failure).
var FileUploads = Counter{"whatiff.file.uploads", "{upload}", "File attachment uploads."}

// FileSize is the size of files and payloads handled by heavy operations. Attributes:
// operation (upload, chat_import, account_import, account_export...), kind.
var FileSize = Histogram{"whatiff.file.size", "By", "Size of files handled.", BucketsBytes}

// FileOperationDuration is one phase of a heavy file operation (normalize, chunk, embed, parse,
// build_zip, upload...). Attributes: operation, stage, error.type.
var FileOperationDuration = Histogram{"whatiff.file.operation.duration", unitSeconds, "Duration of heavy file operation phases.", BucketsJob}

// FileOperationItems is the number of items a heavy operation handled (conversations,
// memories, chunks...). Attributes: operation, kind, outcome (imported, skipped, failed).
var FileOperationItems = Histogram{"whatiff.file.operation.items", "{item}", "Items handled per heavy file operation.", BucketsCount}
