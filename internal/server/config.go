package server

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host        string
	Port        string
	Environment string
	// EnvironmentExplicit reports whether ENV (or the legacy ENVIRONMENT alias)
	// was explicitly present and non-empty in the environment. Environment
	// silently defaults to "development" when unset, so safety gates
	// (LLMBackend != "vendor", DESTRUCTIVE_MIGRATION) must key off this flag
	// instead of the parsed value — otherwise a forgotten ENV in a real
	// deployment would be fail-open.
	EnvironmentExplicit bool
	// EnvironmentConflict is true when ENV and ENVIRONMENT are both set and
	// disagree. Startup must fail fatally rather than pick one silently.
	EnvironmentConflict bool
	// LLMBackend selects how assistant generation is served: "vendor" (real
	// providers, the default), "mock" (in-process MockAdapter, deny-network
	// transport), or "local" (a real local OpenAI-compatible server, e.g.
	// Ollama or LM Studio). "mock" and "local" are only honored under an
	// explicitly-set local/test ENV; main.go fatally rejects them otherwise.
	LLMBackend string
	// MockLLMMode selects the mock response mode: "echo" (default) or "fixed".
	// Only meaningful when LLMBackend == "mock".
	MockLLMMode string
	// MockLLMFixedResponses is the cycling response list for "fixed" mode
	// (MOCK_LLM_FIXED_RESPONSES, "|"-separated).
	MockLLMFixedResponses []string
	// MockLLMStreamDelay is the optional inter-chunk delay for simulated
	// streaming (MOCK_LLM_STREAM_DELAY_MS). Zero streams without delay.
	MockLLMStreamDelay time.Duration
	// LocalLLMBaseURL is the local OpenAI-compatible server endpoint used
	// when LLMBackend == "local" (LOCAL_LLM_BASE_URL). Defaults to Ollama's
	// endpoint when unset.
	LocalLLMBaseURL string
	// LocalLLMModel is the model name requested from the local server
	// (LOCAL_LLM_MODEL). Required when LLMBackend == "local".
	LocalLLMModel  string
	AllowedOrigins []string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	OpenAIKey      string
	AnthropicKey   string
	// ZAIKey enables z.ai GLM models (Anthropic-compatible endpoint); optional.
	ZAIKey string
	// ZAIBaseURL overrides the z.ai Anthropic-compatible base URL; optional.
	ZAIBaseURL string
	// GeminiKey enables Google Gemini models (OpenAI-compatible Chat Completions); optional.
	GeminiKey string
	// GeminiBaseURL overrides Google's OpenAI-compatible base URL; optional.
	GeminiBaseURL         string
	MistralKey            string
	MistralBaseURL        string
	DeepSeekKey           string
	DeepSeekBaseURL       string
	QwenKey               string
	QwenBaseURL           string
	XiaomiKey             string
	XiaomiBaseURL         string
	TokenEncryptionSecret string
	AllowedEmails         []string
	RequireBilling        bool // Feature flag to enable/disable billing
	// EnableAgentJobsScheduler starts the in-process AgentJob scheduler (single-server MVP).
	EnableAgentJobsScheduler bool
	// AgentJobsSchedulerDistributed enables Postgres-backed leader election so only one
	// instance actively runs the scheduler in multi-instance environments.
	AgentJobsSchedulerDistributed bool
	// AgentJobsSchedulerLockKey is the Postgres advisory lock key used by scheduler leadership.
	AgentJobsSchedulerLockKey int64
	// AgentJobsSchedulerLockRetryInterval controls how often followers retry leadership lock acquisition.
	AgentJobsSchedulerLockRetryInterval time.Duration
	// AgentJobsSchedulerLockRetryJitter adds random delay to lock retries to reduce contention spikes.
	AgentJobsSchedulerLockRetryJitter time.Duration
	StripeSecretKey                   string
	StripePublishableKey              string
	StripeWebhookSecret               string
	// RegressionMode enables admin/debug endpoints (never set in production).
	RegressionMode bool

	// S3 file archival configuration.
	S3FileBucket string
	AWSRegion    string

	// RunMigrations controls whether startup migrations and data backfills run.
	RunMigrations bool
}

// parseMockLLMConfig reads the LLM_BACKEND/MOCK_LLM_* env vars into the
// values NewConfig stores on Config. Kept separate from NewConfig so the
// mock-LLM option set can grow without adding to that function's size.
func parseMockLLMConfig() (backend, mode string, fixedResponses []string, streamDelay time.Duration) {
	backend = strings.ToLower(strings.TrimSpace(os.Getenv("LLM_BACKEND")))
	if backend == "" {
		backend = "vendor"
	}
	mode = strings.ToLower(strings.TrimSpace(os.Getenv("MOCK_LLM_MODE")))
	if mode == "" {
		mode = "echo"
	}
	for _, r := range strings.Split(os.Getenv("MOCK_LLM_FIXED_RESPONSES"), "|") {
		if trimmed := strings.TrimSpace(r); trimmed != "" {
			fixedResponses = append(fixedResponses, trimmed)
		}
	}
	if v := strings.TrimSpace(os.Getenv("MOCK_LLM_STREAM_DELAY_MS")); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed >= 0 {
			streamDelay = time.Duration(parsed) * time.Millisecond
		}
	}
	return backend, mode, fixedResponses, streamDelay
}

func NewConfig() *Config {
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	host := os.Getenv("SERVER_HOST")
	if host == "" {
		host = "localhost"
	}

	// ENV is canonical and is the only source of Environment's value; the
	// legacy ENVIRONMENT variable is no longer read as a fallback, but a
	// non-empty value still counts as an explicit environment declaration so
	// mock-LLM guards do not treat such a deployment as unconfigured. An empty
	// string does not count as explicitly set. If both are set and disagree,
	// EnvironmentConflict is raised and main.go fails startup fatally.
	envValue, envSet := os.LookupEnv("ENV")
	envValue = strings.ToLower(strings.TrimSpace(envValue))
	envSet = envSet && envValue != ""
	legacyValue, legacySet := os.LookupEnv("ENVIRONMENT")
	legacyValue = strings.ToLower(strings.TrimSpace(legacyValue))
	legacySet = legacySet && legacyValue != ""

	environmentConflict := envSet && legacySet && envValue != legacyValue
	environmentExplicit := envSet || legacySet
	environment := envValue
	if environment == "" {
		environment = "development"
	}

	llmBackend, mockLLMMode, mockLLMFixedResponses, mockLLMStreamDelay := parseMockLLMConfig()
	localLLMBaseURL := strings.TrimSpace(os.Getenv("LOCAL_LLM_BASE_URL"))
	localLLMModel := strings.TrimSpace(os.Getenv("LOCAL_LLM_MODEL"))

	openAIKey := os.Getenv("OPENAI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	zaiKey := os.Getenv("ZAI_API_KEY")
	zaiBaseURL := strings.TrimSpace(os.Getenv("ZAI_BASE_URL"))
	geminiKey := os.Getenv("GEMINI_API_KEY")
	geminiBaseURL := strings.TrimSpace(os.Getenv("GEMINI_BASE_URL"))
	mistralKey := os.Getenv("MISTRAL_API_KEY")
	mistralBaseURL := strings.TrimSpace(os.Getenv("MISTRAL_BASE_URL"))
	deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
	deepseekBaseURL := strings.TrimSpace(os.Getenv("DEEPSEEK_BASE_URL"))
	qwenKey := os.Getenv("QWEN_API_KEY")
	qwenBaseURL := strings.TrimSpace(os.Getenv("QWEN_BASE_URL"))
	xiaomiKey := os.Getenv("XIAOMI_API_KEY")
	xiaomiBaseURL := strings.TrimSpace(os.Getenv("XIAOMI_BASE_URL"))
	tokenEncryptionSecret := strings.TrimSpace(os.Getenv("TOKEN_ENCRYPTION_SECRET"))

	// Local-development defaults only. Deployed environments must set
	// ALLOWED_ORIGINS explicitly (comma-separated) — production hostnames are
	// deployment configuration, not compiled-in defaults.
	allowedOrigins := []string{
		"http://localhost:8081",
		"http://localhost:8082",
		"http://localhost:4200",
		"http://localhost:4201",
	}

	if allowedOriginsEnv := strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS")); allowedOriginsEnv != "" {
		allowedOrigins = make([]string, 0)
		for _, origin := range strings.Split(allowedOriginsEnv, ",") {
			trimmed := strings.TrimSpace(origin)
			if trimmed != "" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
	}

	// Parse allowed emails (comma-separated)
	allowedEmails := []string{}
	allowedEmailsEnv := os.Getenv("ALLOWED_EMAILS")
	if allowedEmailsEnv != "" {
		rawEmails := strings.Split(allowedEmailsEnv, ",")
		for _, email := range rawEmails {
			trimmed := strings.TrimSpace(email)
			if trimmed != "" {
				// Normalize to lowercase for case-insensitive comparison
				allowedEmails = append(allowedEmails, strings.ToLower(trimmed))
			}
		}
	}

	// Parse billing feature flag
	requireBilling := false
	if os.Getenv("REQUIRE_BILLING") == "true" {
		requireBilling = true
	}

	enableAgentJobsScheduler := false
	if strings.TrimSpace(os.Getenv("ENABLE_AGENTJOBS_SCHEDULER")) == "true" {
		enableAgentJobsScheduler = true
	}
	agentJobsSchedulerDistributed := false
	if strings.TrimSpace(os.Getenv("AGENTJOBS_SCHEDULER_DISTRIBUTED")) == "true" {
		agentJobsSchedulerDistributed = true
	}

	agentJobsSchedulerLockKey := int64(80920031)
	if v := strings.TrimSpace(os.Getenv("AGENTJOBS_SCHEDULER_LOCK_KEY")); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			agentJobsSchedulerLockKey = parsed
		}
	}

	agentJobsSchedulerLockRetryInterval := 2 * time.Second
	if v := strings.TrimSpace(os.Getenv("AGENTJOBS_SCHEDULER_LOCK_RETRY_INTERVAL")); v != "" {
		if parsed, err := time.ParseDuration(v); err == nil && parsed > 0 {
			agentJobsSchedulerLockRetryInterval = parsed
		}
	}

	agentJobsSchedulerLockRetryJitter := 500 * time.Millisecond
	if v := strings.TrimSpace(os.Getenv("AGENTJOBS_SCHEDULER_LOCK_RETRY_JITTER")); v != "" {
		if parsed, err := time.ParseDuration(v); err == nil && parsed >= 0 {
			agentJobsSchedulerLockRetryJitter = parsed
		}
	}

	// Load Stripe configuration (only needed if billing is required)
	stripeSecretKey := os.Getenv("STRIPE_SECRET_KEY")
	stripePublishableKey := os.Getenv("STRIPE_PUBLISHABLE_KEY")
	stripeWebhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")

	regressionMode := os.Getenv("REGRESSION_MODE") == "true"

	s3FileBucket := os.Getenv("S3_FILE_BUCKET")
	awsRegion := os.Getenv("AWS_REGION")
	if awsRegion == "" {
		awsRegion = "us-east-2"
	}

	return &Config{
		Host:                                host,
		Port:                                port,
		Environment:                         environment,
		EnvironmentExplicit:                 environmentExplicit,
		EnvironmentConflict:                 environmentConflict,
		LLMBackend:                          llmBackend,
		MockLLMMode:                         mockLLMMode,
		MockLLMFixedResponses:               mockLLMFixedResponses,
		MockLLMStreamDelay:                  mockLLMStreamDelay,
		LocalLLMBaseURL:                     localLLMBaseURL,
		LocalLLMModel:                       localLLMModel,
		AllowedOrigins:                      allowedOrigins,
		ReadTimeout:                         15 * time.Second,
		WriteTimeout:                        15 * time.Second,
		IdleTimeout:                         60 * time.Second,
		OpenAIKey:                           openAIKey,
		AnthropicKey:                        anthropicKey,
		ZAIKey:                              zaiKey,
		ZAIBaseURL:                          zaiBaseURL,
		GeminiKey:                           geminiKey,
		GeminiBaseURL:                       geminiBaseURL,
		MistralKey:                          mistralKey,
		MistralBaseURL:                      mistralBaseURL,
		DeepSeekKey:                         deepseekKey,
		DeepSeekBaseURL:                     deepseekBaseURL,
		QwenKey:                             qwenKey,
		QwenBaseURL:                         qwenBaseURL,
		XiaomiKey:                           xiaomiKey,
		XiaomiBaseURL:                       xiaomiBaseURL,
		TokenEncryptionSecret:               tokenEncryptionSecret,
		AllowedEmails:                       allowedEmails,
		RequireBilling:                      requireBilling,
		EnableAgentJobsScheduler:            enableAgentJobsScheduler,
		AgentJobsSchedulerDistributed:       agentJobsSchedulerDistributed,
		AgentJobsSchedulerLockKey:           agentJobsSchedulerLockKey,
		AgentJobsSchedulerLockRetryInterval: agentJobsSchedulerLockRetryInterval,
		AgentJobsSchedulerLockRetryJitter:   agentJobsSchedulerLockRetryJitter,
		StripeSecretKey:                     stripeSecretKey,
		StripePublishableKey:                stripePublishableKey,
		StripeWebhookSecret:                 stripeWebhookSecret,
		RegressionMode:                      regressionMode,
		S3FileBucket:                        s3FileBucket,
		AWSRegion:                           awsRegion,
		RunMigrations:                       strings.TrimSpace(os.Getenv("AUTO_MIGRATE")) == "true",
	}
}

// localEnvironments are the ENV values under which mock/destructive behavior
// may be enabled. Unknown explicit values are treated as non-local (fail
// closed) rather than defaulting permissively.
var localEnvironments = map[string]bool{
	"development": true,
	"test":        true,
	"local":       true,
}

// IsExplicitLocalEnv reports whether ENV (or the legacy ENVIRONMENT alias) was
// explicitly set to a local/test environment. This is the only gate that may
// enable a non-vendor LLMBackend ("mock" or "local") or DESTRUCTIVE_MIGRATION:
// the parsed Environment value defaults to "development" when unset, which
// would be fail-open.
func (c *Config) IsExplicitLocalEnv() bool {
	if !c.EnvironmentExplicit || c.EnvironmentConflict {
		return false
	}
	return localEnvironments[c.Environment]
}

// getenvFirst returns the first non-empty trimmed value for the given keys.
// Supports common .env typos (leading space before the key name).
func getenvFirst(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}
