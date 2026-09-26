package telemetry

import (
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

// Attribute keys. Keys defined by OpenTelemetry semantic conventions keep their dotted names
// (error.type, http.*, db.*, gen_ai.*); app-specific keys are short snake_case. Every value must
// come from a bounded set: never IDs, raw user input, URLs or model-emitted strings.
const (
	// AttrErrorType is set only on failures (semconv). Values come from ClassifyError.
	AttrErrorType = attribute.Key("error.type")

	AttrHTTPMethod      = attribute.Key("http.request.method")
	AttrHTTPRoute       = attribute.Key("http.route")
	AttrHTTPStatusClass = attribute.Key("http.status_class")

	AttrDBCollection = attribute.Key("db.collection.name")
	AttrDBOperation  = attribute.Key("db.operation.name")
	// AttrDBConnectionState is idle or used, on db.client.connection.count.
	AttrDBConnectionState = attribute.Key("db.client.connection.state")

	AttrGenAIProvider  = attribute.Key("gen_ai.provider.name")
	AttrGenAIModel     = attribute.Key("gen_ai.request.model")
	AttrGenAIOperation = attribute.Key("gen_ai.operation.name")
	AttrGenAITokenType = attribute.Key("gen_ai.token.type")

	// AttrDependency names an external dependency (see the Dependency* constants).
	AttrDependency = attribute.Key("dependency")
	// AttrOperation names a bounded operation on a dependency or flow (e.g. "put_object").
	AttrOperation = attribute.Key("operation")
	// AttrCallPath is the feature that issued an LLM call (see CallPath).
	AttrCallPath = attribute.Key("call_path")
	AttrJobType  = attribute.Key("job_type")
	AttrStatus   = attribute.Key("status")
	// AttrOutcome is a bounded result for flows that have more outcomes than ok/error (e.g.
	// success, failed, cancelled, quota for jobs; ran, skipped_overlap for scheduled runs).
	AttrOutcome = attribute.Key("outcome")
	AttrStage   = attribute.Key("stage")
	AttrReason  = attribute.Key("reason")
	AttrTool    = attribute.Key("tool")
	AttrSegment = attribute.Key("segment")
	AttrKind    = attribute.Key("kind")
)

// Dependency names for AttrDependency. Add new ones here so dashboards can rely on the set.
const (
	DependencyPostgres  = "postgres"
	DependencyS3        = "s3"
	DependencyLocalFS   = "local_fs"
	DependencySES       = "ses"
	DependencyParallel  = "parallel"
	DependencyOpenAI    = "openai"
	DependencyAnthropic = "anthropic"
	DependencyZAI       = "zai"
	DependencyGemini    = "gemini"
	DependencyDeepSeek  = "deepseek"
	DependencyMistral   = "mistral"
	DependencyQwen      = "qwen"
	DependencyXiaomi    = "xiaomi"
	DependencyLocalLLM  = "local_llm"
	DependencyStripe    = "stripe"
	DependencyCognito   = "cognito"
	DependencyJira      = "jira"
	DependencyFCM       = "fcm"
	DependencyC4A       = "c4a"
	DependencyOther     = "other"
)

// Token types for AttrGenAITokenType. "input" and "output" are semconv; cached input and
// reasoning are recorded separately so cache hit rates and thinking cost are visible. Cached
// input is a subset of input, and reasoning a subset of output, so don't add them together.
const (
	TokenTypeInput       = "input"
	TokenTypeOutput      = "output"
	TokenTypeCachedInput = "cached_input"
	TokenTypeReasoning   = "reasoning"
)

// ErrorAttrs returns the error.type attribute for err, or nothing when err is nil, so success
// and failure share a histogram and error rates come from its count.
func ErrorAttrs(err error) []attribute.KeyValue {
	if t := ClassifyError(err); t != "" {
		return []attribute.KeyValue{AttrErrorType.String(t)}
	}
	return nil
}

// FileKind maps a MIME type to a coarse kind for the kind attribute on file metrics, so an
// unexpected MIME type can't add series.
func FileKind(mimeType string) string {
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(mt, "image/"):
		return "image"
	case mt == "application/pdf":
		return "pdf"
	case strings.HasPrefix(mt, "audio/"):
		return "audio"
	case strings.HasPrefix(mt, "text/"), mt == "application/json", mt == "application/xml",
		mt == "application/x-yaml", mt == "application/yaml", mt == "application/javascript":
		return "text"
	default:
		return "other"
	}
}
