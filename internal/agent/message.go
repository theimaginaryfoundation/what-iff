package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/filechunker"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
	"github.com/theimaginaryfoundation/what-iff/internal/pushnotify"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"go.uber.org/zap"
)

// defaultModel our custom fine-tuned model
var defaultModel = func() string {
	if value := os.Getenv(defaultModelNameKey); value != "" {
		return value
	}
	return models.DefaultModelName
}()

// JobTypeChatMessage is the job type for chat message processing
const (
	JobTypeChatMessage  = "chat_message"
	JobTypeAgentJobRun  = "agent_job_run"
	defaultModelNameKey = "DEFAULT_MODEL_NAME"

	defaultChatName = "New Chat"

	checkpointMaxAssistantMessagesSinceStart   = 5
	checkpointMaxAssistantMessagesSinceSummary = 20
	checkpointMaxLastInputTokens               = 30_000
	checkpointMaxEstimatedContextTokens        = 32_000
	// checkpointMinTurnsBetweenCheckpoints throttles token-triggered compaction so a
	// burst of tool-heavy turns (e.g. agent job runs with large web-search results)
	// cannot force a checkpoint every turn. Scheduled turn-count checkpointing is
	// unaffected.
	checkpointMinTurnsBetweenCheckpoints = 5
	summaryMemoryBackfillBatchSize       = 50

	MAX_PERSONALITY_FILES       = 40
	maxToolCallRounds           = 10
	jobDraftDeltaFlushMinChars  = 96
	jobDraftDeltaFlushMaxWait   = 250 * time.Millisecond
	jobDraftDeltaPersistTimeout = 2 * time.Second
	// Allow a short detached write window when the generation context is cancelled
	// but we still need to persist terminal job state.
	jobTerminalPersistTimeout = 5 * time.Second
	// metrics
	generateResponseJobDurationKey = "generate_response_job_duration"
	generateResponseDurationKey    = "generate_response_duration"
	postProcessMessageDurationKey  = "post_process_message_duration"
	postProcessMessageCountKey     = "post_process_message_count"
	toolCallCountKey               = "tool_call_count"
	totalToolCallsHistogramKey     = "total_tool_calls"

	safetyViolationAssistantMessage = "⚠️ This message has triggered a safety/ethics violation and cannot be processed"
)

// Retry eligibility errors for chat message retry HTTP API.
var (
	ErrRetryChatMismatch  = errors.New("chat message does not belong to this chat")
	ErrRetryNotUserOrigin = errors.New("only user messages can be retried")
)

// Agent wraps the OpenAI client and provides methods for model evaluation
type Agent struct {
	ds             *datastore.Datastore
	logger         *zap.Logger
	telemetry      *telemetry.Telemetry
	OpenAIProvider *provider.OpenAIProvider
	// ClaudeProvider is non-nil when ANTHROPIC_API_KEY is configured.
	ClaudeProvider *provider.ClaudeProvider
	// ZAIProvider is non-nil when ZAI_API_KEY is configured. It speaks the
	// Anthropic Messages API against z.ai's compatible endpoint (GLM models).
	ZAIProvider *provider.ClaudeProvider
	// GeminiProvider is non-nil when GEMINI_API_KEY is configured. It speaks
	// Google's OpenAI-compatible Chat Completions API (Gemini models).
	GeminiProvider *provider.GeminiProvider
	// MistralProvider is non-nil when MISTRAL_API_KEY is configured.
	MistralProvider *provider.MistralProvider
	// DeepSeekProvider is non-nil when DEEPSEEK_API_KEY is configured.
	DeepSeekProvider *provider.DeepSeekProvider
	// QwenProvider is non-nil when QWEN_API_KEY is configured.
	QwenProvider *provider.QwenProvider
	// XiaomiProvider is non-nil when XIAOMI_API_KEY is configured.
	XiaomiProvider *provider.XiaomiProvider
	// LocalProvider is non-nil when LLMBackend == "local"; talks to a real
	// local OpenAI-compatible server (e.g. Ollama, LM Studio).
	LocalProvider  *provider.LocalProvider
	memoryTool     *tools.VectorStoreMemoryTool
	scratchpadTool *tools.ScratchpadTool
	recallTool     *tools.RecallTool
	listTool       *tools.ListTool
	chunkPipeline  *filechunker.FileChunkPipeline
	fileStore      storage.FileStore
	// expressionPortraitThumbCache caches thumbnails loaded for expression continuity in message context (bounded).
	expressionPortraitThumbCache *expressionPortraitThumbCache
	// tokenCounter is shared for segment token estimates and is safe for concurrent use (stateless tokenizer).
	tokenCounter *provider.TokenCounter

	// meter gates and records metered turns. It is the only metering dependency in
	// the agent: the concrete implementation (a real meter vs. metering.NoopMeter)
	// is chosen at construction and swapped via server wiring, so no implementation
	// detail reaches this package.
	meter metering.Meter
	// pushNotifier delivers a push on autonomous/webhook reply completion. Like
	// meter, the concrete implementation (a real sender vs. pushnotify.NoopNotifier)
	// is chosen at construction and swapped via server wiring, so no push detail
	// reaches this package.
	pushNotifier pushnotify.Notifier
	// lifecycleCtx is cancelled on server/process shutdown. Use for detached writes
	// that should outlive request cancellation but still stop on app shutdown.
	lifecycleCtx context.Context

	// mockLLM routes all assistant generation through an in-process MockAdapter
	// and serves image rituals from an embedded fixture. localLLM routes
	// assistant generation through a real local model server instead, while
	// still serving every other LLM consumer (memory enrichment, chat naming,
	// checkpoint summarization, ...) the same deliberate mock behavior as
	// mockLLM. Both are only enabled under an explicitly-set local/test ENV
	// (enforced fatally in cmd/api-server); at most one is true.
	mockLLM            bool
	mockLLMMode        provider.MockMode
	mockFixedResponses []string
	mockStreamDelay    time.Duration
	localLLM           bool
	localLLMModel      string

	// testHooks holds optional test-only seams (memory/history overrides, image ritual fakes).
	// Must be zero in production; see assertNoTestHooksInProduction.
	testHooks agentTestHooks

	runningJobCancelsMu sync.Mutex
	runningJobCancels   map[uuid.UUID]runningJobCancel
}

// nonVendorLLM reports whether assistant generation is served by anything
// other than a real vendor provider (mock or local). Every LLM consumer
// besides assistant generation itself (memory enrichment, chat naming,
// checkpoint summarization, expression classification, personality/
// expression-grid generation, thread rehydration, ...) keys off this rather
// than mockLLM alone, so LLM_BACKEND=local gets the same deliberate
// skip/fake behavior mock mode has for everything it doesn't itself drive.
func (a *Agent) nonVendorLLM() bool {
	return a.mockLLM || a.localLLM
}

type runningJobCancel struct {
	userID uuid.UUID
	cancel context.CancelFunc
}

// AgentConfig holds immutable configuration set at construction time.
// All fields are read-only after NewAgent returns; setting them here instead
// of via post-construction setters eliminates concurrent-access hazards.
type AgentConfig struct {
	// Meter gates and records billable turns. When nil, NewAgent falls back to
	// metering.NoopMeter (allow-all, no tracking) — the open-source default.
	Meter metering.Meter
	// PushNotifier delivers a push on autonomous/webhook reply completion. When
	// nil, NewAgent falls back to pushnotify.NoopNotifier (sends nothing) — the
	// open-source default.
	PushNotifier pushnotify.Notifier
	// LifecycleContext is cancelled on app shutdown and used for detached work.
	// Nil defaults to context.Background().
	LifecycleContext context.Context

	// HTTPClient, when non-nil, replaces the default HTTP client in every
	// provider SDK client built by NewAgent. Mock mode passes
	// provider.DenyNetworkHTTPClient() so no provider call can leave the
	// process. All provider clients MUST be constructed through NewAgent (or
	// receive this client) — a one-off openai.NewClient(...) elsewhere would be
	// an accidental escape hatch from the no-egress guarantee.
	HTTPClient *http.Client
	// LLMBackend selects assistant generation: "vendor" (default, real
	// providers), "mock" (in-process MockAdapter), or "local" (a real local
	// OpenAI-compatible server).
	LLMBackend string
	// MockLLMMode selects the mock response mode ("echo" default, "fixed").
	MockLLMMode string
	// MockLLMFixedResponses is the cycling response list for "fixed" mode.
	MockLLMFixedResponses []string
	// MockLLMStreamDelay is the optional inter-chunk delay for simulated streaming.
	MockLLMStreamDelay time.Duration
	// LocalLLMBaseURL is the local OpenAI-compatible server endpoint used when
	// LLMBackend == "local". Empty uses provider.DefaultLocalBaseURL.
	LocalLLMBaseURL string
	// LocalLLMModel is the model requested from the local server. Required
	// when LLMBackend == "local".
	LocalLLMModel string

	// ZAIKey enables z.ai GLM models (Anthropic-compatible endpoint) when set.
	ZAIKey string
	// ZAIBaseURL overrides z.ai's Anthropic-compatible base URL. Empty uses the
	// provider default (see server config).
	ZAIBaseURL string
	// GeminiKey enables Google Gemini models (OpenAI-compatible Chat Completions)
	// when set.
	GeminiKey string
	// GeminiBaseURL overrides Google's OpenAI-compatible base URL. Empty uses
	// provider.DefaultGeminiBaseURL.
	GeminiBaseURL string
	// MistralKey enables Mistral models (OpenAI-compatible Chat Completions); optional.
	MistralKey string
	// MistralBaseURL overrides Mistral's OpenAI-compatible base URL; optional.
	MistralBaseURL string
	// DeepSeekKey enables DeepSeek models (OpenAI-compatible Chat Completions); optional.
	DeepSeekKey string
	// DeepSeekBaseURL overrides DeepSeek's OpenAI-compatible base URL; optional.
	DeepSeekBaseURL string
	// QwenKey enables Qwen models via DashScope (OpenAI-compatible); optional.
	QwenKey string
	// QwenBaseURL overrides Qwen's OpenAI-compatible base URL; optional.
	QwenBaseURL string
	// XiaomiKey enables Xiaomi MiMo models (OpenAI-compatible Chat Completions); optional.
	XiaomiKey string
	// XiaomiBaseURL overrides Xiaomi MiMo's OpenAI-compatible base URL; optional.
	XiaomiBaseURL string
}

// NewAgent creates a new agent wired to OpenAI (required) and optionally Claude.
// Pass anthropicKey="" to start without Claude support.
// cfg carries immutable agent configuration; zero value is safe for development.
func NewAgent(ds *datastore.Datastore, logger *zap.Logger, tel *telemetry.Telemetry, apiKey string, fileStore storage.FileStore, anthropicKey string, cfg AgentConfig) *Agent {
	oaiOpts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if cfg.HTTPClient != nil {
		oaiOpts = append(oaiOpts, option.WithHTTPClient(cfg.HTTPClient))
	}
	oaiClient := openai.NewClient(oaiOpts...)

	a := &Agent{
		ds:                           ds,
		logger:                       logger,
		telemetry:                    tel,
		tokenCounter:                 provider.NewTokenCounter(),
		expressionPortraitThumbCache: newExpressionPortraitThumbCache(384),
		OpenAIProvider:               provider.NewOpenAIProvider(ds, &oaiClient, fileStore, tel),
		memoryTool:                   tools.NewVectorStoreMemoryTool(ds, &oaiClient, logger),
		scratchpadTool:               tools.NewScratchpadTool(ds, logger),
		listTool:                     tools.NewListTool(ds, logger),
		chunkPipeline:                newChunkPipelineForMode(cfg.LLMBackend != "vendor", &oaiClient, ds, logger),
		fileStore:                    fileStore,
		meter:                        cfg.Meter,
		pushNotifier:                 cfg.PushNotifier,
		runningJobCancels:            make(map[uuid.UUID]runningJobCancel),
		lifecycleCtx:                 cfg.LifecycleContext,
		mockLLM:                      cfg.LLMBackend == "mock",
		mockLLMMode:                  provider.MockMode(cfg.MockLLMMode),
		mockFixedResponses:           cfg.MockLLMFixedResponses,
		mockStreamDelay:              cfg.MockLLMStreamDelay,
		localLLM:                     cfg.LLMBackend == "local",
		localLLMModel:                cfg.LocalLLMModel,
	}
	if a.lifecycleCtx == nil {
		a.lifecycleCtx = context.Background()
	}
	if a.meter == nil {
		// No metering implementation supplied (e.g. open-source build): every turn
		// is allowed and untracked.
		a.meter = metering.NoopMeter{Logger: logger}
	}
	if a.pushNotifier == nil {
		// No push implementation supplied (e.g. open-source build): completed
		// replies notify nothing.
		a.pushNotifier = pushnotify.NoopNotifier{}
	}

	// recallTool is constructed after `a` so its investigate distiller can reuse the agent's
	// OpenAIProvider (set in the struct literal above).
	a.recallTool = tools.NewRecallTool(ds, &oaiClient, newRecallDistiller(a), a.fileStore, logger)

	if anthropicKey != "" {
		a.ClaudeProvider = provider.NewClaudeProvider(anthropicKey, tel, cfg.HTTPClient)
	} else {
		logger.Info("ANTHROPIC_API_KEY not set; Claude models will be unavailable")
	}

	if cfg.ZAIKey != "" {
		zaiBaseURL := cfg.ZAIBaseURL
		if zaiBaseURL == "" {
			zaiBaseURL = provider.DefaultZAIBaseURL
		}
		a.ZAIProvider = provider.NewClaudeProviderWithBaseURL(cfg.ZAIKey, zaiBaseURL, tel, cfg.HTTPClient)
	} else {
		logger.Info("ZAI_API_KEY not set; z.ai GLM models will be unavailable")
	}

	if cfg.GeminiKey != "" {
		a.GeminiProvider = provider.NewGeminiProvider(cfg.GeminiKey, cfg.GeminiBaseURL, tel, cfg.HTTPClient)
	} else {
		logger.Info("GEMINI_API_KEY not set; Gemini models will be unavailable")
	}

	if cfg.MistralKey != "" {
		a.MistralProvider = provider.NewMistralProvider(cfg.MistralKey, cfg.MistralBaseURL, tel, cfg.HTTPClient)
	} else {
		logger.Info("MISTRAL_API_KEY not set; Mistral models will be unavailable")
	}

	if cfg.DeepSeekKey != "" {
		a.DeepSeekProvider = provider.NewDeepSeekProvider(cfg.DeepSeekKey, cfg.DeepSeekBaseURL, tel, cfg.HTTPClient)
	} else {
		logger.Info("DEEPSEEK_API_KEY not set; DeepSeek models will be unavailable")
	}

	if cfg.QwenKey != "" {
		a.QwenProvider = provider.NewQwenProvider(cfg.QwenKey, cfg.QwenBaseURL, tel, cfg.HTTPClient)
	} else {
		logger.Info("QWEN_API_KEY not set; Qwen models will be unavailable")
	}

	if cfg.XiaomiKey != "" {
		a.XiaomiProvider = provider.NewXiaomiProvider(cfg.XiaomiKey, cfg.XiaomiBaseURL, tel, cfg.HTTPClient)
	} else {
		logger.Info("XIAOMI_API_KEY not set; Xiaomi MiMo models will be unavailable")
	}

	if a.localLLM {
		// LocalProvider deliberately does NOT use cfg.HTTPClient: under
		// LLMBackend=local, cfg.HTTPClient is the deny-network transport (every
		// other consumer stays egress-denied), but the local adapter itself
		// must reach the local server over a real client.
		a.LocalProvider = provider.NewLocalProvider(cfg.LocalLLMBaseURL, tel, nil)
	}

	if a.mockLLM {
		logger.Warn("LLM_BACKEND=mock: assistant generation uses the in-process mock adapter",
			zap.String("mock_mode", string(a.mockLLMMode)),
			zap.Bool("provider_egress_denied", cfg.HTTPClient != nil),
		)
	}
	if a.localLLM {
		logger.Warn("LLM_BACKEND=local: assistant generation uses a real local model server",
			zap.String("local_llm_model", a.localLLMModel),
		)
	}

	assertNoTestHooksInProduction(a)
	return a
}

// newChunkPipelineForMode returns the mock (no-embeddings) pipeline under a
// non-vendor LLMBackend (mock or local), else the real one bound to the
// shared (possibly deny-injected) OpenAI client.
func newChunkPipelineForMode(nonVendorBackend bool, oaiClient *openai.Client, ds *datastore.Datastore, logger *zap.Logger) *filechunker.FileChunkPipeline {
	if nonVendorBackend {
		return filechunker.NewMockFileChunkPipeline(ds, logger)
	}
	return filechunker.NewFileChunkPipeline(oaiClient, ds, logger)
}

// withCallPath attaches telemetry.CallPath to ctx so provider inference metrics are labeled.
// Use this on the outermost Agent entry point for each user- or system-triggered flow; packages
// that call providers without an Agent (e.g. gate, schedule) should call telemetry.WithCallPath directly.
func (a *Agent) withCallPath(ctx context.Context, path telemetry.CallPath) context.Context {
	return telemetry.WithCallPath(ctx, path)
}

func (a *Agent) registerRunningJobCancel(jobID, userID uuid.UUID, cancel context.CancelFunc) {
	if jobID == uuid.Nil || userID == uuid.Nil || cancel == nil {
		return
	}
	a.runningJobCancelsMu.Lock()
	a.runningJobCancels[jobID] = runningJobCancel{userID: userID, cancel: cancel}
	a.runningJobCancelsMu.Unlock()
}

func (a *Agent) unregisterRunningJobCancel(jobID uuid.UUID) {
	if jobID == uuid.Nil {
		return
	}
	a.runningJobCancelsMu.Lock()
	delete(a.runningJobCancels, jobID)
	a.runningJobCancelsMu.Unlock()
}

// CancelJob cancels an in-flight job owned by userID. Missing entries are treated as no-op.
func (a *Agent) CancelJob(_ context.Context, userID, jobID uuid.UUID) error {
	if userID == uuid.Nil || jobID == uuid.Nil {
		return datastore.ErrUnauthorized
	}
	a.runningJobCancelsMu.Lock()
	entry, ok := a.runningJobCancels[jobID]
	a.runningJobCancelsMu.Unlock()
	if !ok {
		return nil
	}
	if entry.userID != userID {
		return datastore.ErrUnauthorized
	}
	entry.cancel()
	return nil
}

// ChunkPipeline returns the file chunk pipeline for asynchronous file processing.
func (a *Agent) ChunkPipeline() *filechunker.FileChunkPipeline { return a.chunkPipeline }

// FileStore returns the file store for S3 archival.
func (a *Agent) FileStore() storage.FileStore { return a.fileStore }

// DataStore returns the agent datastore for extension seams.
func (a *Agent) DataStore() *datastore.Datastore { return a.ds }

// Logger returns the agent logger for extension seams.
func (a *Agent) Logger() *zap.Logger { return a.logger }

// DeleteProviderFileAttachment deletes a provider-managed file by provider file ID.
func (a *Agent) DeleteProviderFileAttachment(ctx context.Context, fileID string) error {
	if a == nil || a.OpenAIProvider == nil {
		return fmt.Errorf("openai provider is not configured")
	}
	return a.OpenAIProvider.DeleteFileAttachment(ctx, fileID)
}

// RecordFileUpload emits a counter for a file-attachment upload attempt.
// status should be "success" or "failure".
func (a *Agent) RecordFileUpload(ctx context.Context, fileType, status string) {
	a.recordCounter(ctx, telemetry.FileAttachmentUploadTotal, 1,
		metric.WithAttributes(
			attribute.String("file_type", fileType),
			attribute.String("status", status),
		))
}

// messageContextBuilder returns a context builder wired to this agent's datastore,
// telemetry, and test-only history override (when set).
func (a *Agent) messageContextBuilder() (*messageContextBuilder, error) {
	return newMessageContextBuilder(a.ds, a.telemetry, a.fileStore, a.testHooks.LoadHistoryOverride, a.expressionPortraitThumbCache)
}

// buildModelContextForChatMessage runs prepareUserMessage (timestamp, rituals) then builds
// the ModelContext for this turn: history, memories, attachment labels, and the final user segment.
// Use this for normal chat and job/ritual paths that should match production user-message handling.
// imageBytes maps attachment ID → raw bytes for Claude vision; pass nil on the OpenAI path.
func (a *Agent) buildModelContextForChatMessage(ctx context.Context, userID uuid.UUID, chatMessage *models.ChatMessage, chatCtx *chatContext, imageBytes map[uuid.UUID][]byte) (*provider.ModelContext, error) {
	userPrompt, err := a.prepareUserMessage(ctx, userID, chatMessage)
	if err != nil {
		return nil, fmt.Errorf("prepare user message: %w", err)
	}
	b, err := a.messageContextBuilder()
	if err != nil {
		return nil, err
	}
	// Attachment labels are only injected when tools are enabled, matching the
	// previous behavior for OpenAI chat turns.
	additionalDevContext := ""
	if additionalDeveloperContextForChat != nil {
		additionalDevContext = additionalDeveloperContextForChat(a, chatCtx.chat)
	}
	return b.build(ctx, messageContextBuildRequest{
		UserID:                     userID,
		Chat:                       chatCtx.chat,
		UserPrompt:                 userPrompt,
		CurrentMessage:             chatMessage,
		Memories:                   chatCtx.memories,
		LiveMemories:               chatCtx.liveMemories,
		ActiveMood:                 chatCtx.activeMood,
		ActiveMoodRituals:          chatCtx.activeMoodRituals,
		IsAutoMood:                 chatCtx.chat.IsAutoMood,
		MoodToolsAvailable:         a.shouldExposeMoodTools(ctx, userID, chatCtx.chat),
		Attachments:                chatMessage.Attachments,
		ImageBytes:                 imageBytes,
		ExpressionsEnabled:         chatCtx.expressionsEnabled,
		IncludeAttachmentContext:   chatCtx.chat.ToolsEnabled,
		AdditionalDeveloperContext: additionalDevContext,
		LoadHistoryImageBytes:      models.UsesAnthropicMessagesAPI(chatCtx.modelProvider, chatCtx.model),
	})
}

// loadImageBytesForClaude downloads raw image bytes for each image attachment on the
// current user message so that Claude can receive them as vision blocks. Missing bytes are logged as warnings; the
// attachment is still included in the user turn but Claude will fall back to text-only.
func (a *Agent) loadImageBytesForClaude(ctx context.Context, userID uuid.UUID, chatMessage *models.ChatMessage) map[uuid.UUID][]byte {
	return loadImageBytesForAttachments(ctx, a.logger, a.fileStore, userID, chatMessage.ChatID, chatMessage.Attachments)
}

// HandleUserMessage handles a user message and initiates the agent processing flow
func (a *Agent) HandleUserMessage(ctx context.Context, request models.ChatMessage) (*models.ChatMessageResponse, error) {
	ctx, ok := middleware.CopyUserToIDContext(ctx, context.Background())
	if !ok {
		return nil, errors.New("user ID not found in context")
	}
	// Extract user ID from context
	userID, _ := middleware.GetUserIDFromContext(ctx)

	// System rituals are UI-only and must never be persisted.
	// Persist only DB-backed rituals, but keep system rituals for in-memory branching.
	dbRituals, systemRituals := SplitRituals(request.Rituals)
	request.Rituals = dbRituals

	// Create a new chat message in the database
	chatMessage, err := a.ds.CreateChatMessage(ctx, userID, request)
	if err != nil {
		return nil, fmt.Errorf("failed to create chat message: %w", err)
	}
	if len(systemRituals) > 0 {
		chatMessage.Rituals = append(chatMessage.Rituals, systemRituals...)
	}

	// Create a job for asynchronous processing
	newJob, err := a.ds.CreateJob(ctx, userID, models.Job{
		JobType:     JobTypeChatMessage,
		Reference:   chatMessage.ID.String(),
		Status:      models.JobStatusPending,
		DraftDeltas: []string{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	// Start the background processing with a cancellable runtime context.
	runCtx, cancel := context.WithCancel(ctx)
	a.registerRunningJobCancel(newJob.ID, userID, cancel)
	go func() {
		defer cancel()
		defer a.unregisterRunningJobCancel(newJob.ID)
		// An unrecovered panic in any goroutine takes down the whole process (and so the
		// pod). Recover here so a failure while processing one message fails just that job
		// instead — e.g. a post-inference checkpoint summary that a provider rejects must
		// not crash every other in-flight chat. Registered after cancel/unregister so it
		// runs first (LIFO) and UpdateJobStatus still sees a live runCtx.
		defer a.recoverAsyncMessageJob(runCtx, userID, newJob.ID, chatMessage.ID)
		_, err := a.handleUserMessage(runCtx, newJob, chatMessage)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				a.logger.Info("async agent message processing cancelled",
					zap.String("job_id", newJob.ID.String()),
					zap.String("chat_message_id", chatMessage.ID.String()),
				)
				return
			}
			a.logger.Error("async agent message processing failed",
				zap.String("job_id", newJob.ID.String()),
				zap.String("chat_message_id", chatMessage.ID.String()),
				zap.Error(err),
			)
		}
	}()

	// Return response with job details
	return &models.ChatMessageResponse{
		ID:    chatMessage.ID,
		JobID: newJob.ID.String(),
		Type:  JobTypeChatMessage,
	}, nil
}

// recoverAsyncMessageJob is the deferred panic guard for async chat-message processing.
// A panic in the background goroutine would otherwise crash the whole process (the pod);
// here it is contained to the single job, which is marked failed and logged with its stack.
// Failing to record the failed status is itself logged and swallowed — recovery must never
// re-panic. runCtx must still be live (call before its cancel runs).
func (a *Agent) recoverAsyncMessageJob(runCtx context.Context, userID, jobID, chatMessageID uuid.UUID) {
	recovered := recover()
	if recovered == nil {
		return
	}
	panicMessage := fmt.Sprintf("panic: %v", recovered)
	if _, failErr := a.ds.UpdateJobStatus(runCtx, userID, jobID, models.JobStatusFailed, panicMessage); failErr != nil {
		a.logger.Error("failed to mark async agent message job failed after panic",
			zap.String("job_id", jobID.String()),
			zap.Error(failErr),
		)
	}
	a.logger.Error("panic recovered in async agent message processing",
		zap.String("job_id", jobID.String()),
		zap.String("chat_message_id", chatMessageID.String()),
		zap.Any("panic", recovered),
		zap.ByteString("stack_trace", debug.Stack()),
	)
}

// RetryUserChatMessage enqueues another chat_message job for an existing user turn.
// When a non-terminal job already exists for that message, returns its job id without creating a duplicate.
func (a *Agent) RetryUserChatMessage(ctx context.Context, chatID, messageID uuid.UUID) (*models.ChatMessageResponse, error) {
	// Detach from request cancellation before enqueue + background execution. User id and timezone
	// are copied into a fresh Background context (see middleware.CopyUserToIDContext).
	detachedCtx, ok := middleware.CopyUserToIDContext(ctx, context.Background())
	if !ok {
		return nil, errors.New("user ID not found in context")
	}
	userID, _ := middleware.GetUserIDFromContext(detachedCtx)

	msg, err := a.ds.GetChatMessage(detachedCtx, userID, messageID)
	if err != nil {
		return nil, err
	}
	if msg.ChatID != chatID {
		return nil, ErrRetryChatMismatch
	}
	if msg.Origin != models.MessageOriginUser {
		return nil, ErrRetryNotUserOrigin
	}

	active, err := a.ds.FindLatestActiveChatMessageJob(detachedCtx, userID, messageID)
	if err != nil {
		return nil, err
	}
	if active != nil {
		return &models.ChatMessageResponse{
			ID:    messageID,
			JobID: active.ID.String(),
			Type:  JobTypeChatMessage,
		}, nil
	}

	if err := a.ds.SetChatMessageLastError(detachedCtx, userID, messageID, nil); err != nil {
		a.logger.Warn("failed to clear last_error_message before retry", zap.Error(err))
	}

	newJob, err := a.ds.CreateJob(detachedCtx, userID, models.Job{
		JobType:     JobTypeChatMessage,
		Reference:   messageID.String(),
		Status:      models.JobStatusPending,
		DraftDeltas: []string{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	runCtx, cancel := context.WithCancel(detachedCtx)
	a.registerRunningJobCancel(newJob.ID, userID, cancel)
	go func(runCtx context.Context, job *models.Job, chat *models.ChatMessage) {
		defer cancel()
		defer a.unregisterRunningJobCancel(job.ID)
		if _, err := a.handleUserMessage(runCtx, job, chat); err != nil {
			if errors.Is(err, context.Canceled) {
				a.logger.Info("async agent message retry cancelled",
					zap.String("job_id", job.ID.String()),
					zap.String("chat_message_id", chat.ID.String()),
				)
				return
			}
			a.logger.Error("async agent message retry failed",
				zap.String("job_id", job.ID.String()),
				zap.String("chat_message_id", chat.ID.String()),
				zap.Error(err),
			)
		}
	}(runCtx, newJob, msg)

	return &models.ChatMessageResponse{
		ID:    messageID,
		JobID: newJob.ID.String(),
		Type:  JobTypeChatMessage,
	}, nil
}

// chatContext holds the context needed for processing a chat message
type chatContext struct {
	chat                   *models.Chat
	memories               []string
	liveMemories           []*models.Memory
	memoryEnrichmentFailed bool
	model                  string
	modelProvider          string
	// modelSubscriptionTier is the model's raw SubscriptionTier string
	// ("low"/"medium"/"high"/"ultra"), passed to the meter which classifies it for
	// free-chat gating. Empty means unknown; the meter treats that conservatively.
	modelSubscriptionTier string
	activeMood            *models.Mood
	activeMoodRituals     []*models.Ritual
	// expressionsEnabled mirrors personality.ExpressionsEnabled; when false,
	// expression picking is skipped for this turn.
	expressionsEnabled bool
	// webSearchCount is set after the provider turn for native web search metering.
	webSearchCount int
}

// handleUserMessage handles the agent processing flow for a user message
func (a *Agent) handleUserMessage(ctx context.Context, chatJob *models.Job, chatMessage *models.ChatMessage) (*models.ChatMessage, error) {
	assertNoTestHooksInProduction(a)
	ctx = a.withCallPath(ctx, telemetry.CallPathUserChat)

	// Update job status to processing
	if err := a.updateJobStatus(ctx, chatJob, models.JobStatusProcessing); err != nil {
		a.logger.Error("failed to update job status", zap.Error(err))
		a.setJobStatusFailed(ctx, chatJob, err)
		return nil, err
	}
	// Defensive reset: guarantee each turn starts from a clean streaming buffer,
	// even if any previous run left residual draft chunks.
	if err := a.ds.ClearJobDraftDeltas(ctx, chatJob.UserID, chatJob.ID); err != nil {
		a.logger.Warn("failed to clear job draft deltas before generation",
			zap.String("job_id", chatJob.ID.String()),
			zap.Error(err),
		)
	}
	a.logger.Info("starting job for user message",
		zap.String("user_id", chatJob.UserID.String()),
		zap.String("chat_message_id", chatMessage.ID.String()))

	// Rehydration gate: if this thread was just restored from import and its summary is still being
	// generated, stall here until it settles so the turn runs against the checkpoint summary + recent
	// window rather than the full (possibly huge) history. No-op for native/ready threads.
	a.WaitForThreadRehydration(ctx, chatJob.UserID, chatMessage.ChatID)

	// Prepare chat context (chat, memories, model)
	chatCtx, err := a.prepareChatContext(ctx, chatJob.UserID, chatMessage)
	if err != nil {
		a.logger.Error("failed to prepare chat context", zap.Error(err))
		a.setJobStatusFailed(ctx, chatJob, err)
		return nil, err
	}
	// Resolve active mood and load mood-driven rituals before determining action type
	// (a mood may inject image-generation rituals). Mode skills are injected into the
	// mode context segment, not the user message.
	chatCtx.activeMood = a.resolveActiveMood(ctx, chatJob.UserID, chatCtx, chatMessage.Message, chatMessage.ID)
	moodRituals := a.loadMoodRituals(ctx, chatJob.UserID, chatCtx.activeMood)
	chatCtx.activeMoodRituals = moodRituals

	// Determine action type: image generation rituals override the base chat type.
	turnActionType := models.ActionTypeChatMessage
	if hasSystemRitual(mergeRitualSets(chatMessage.Rituals, moodRituals), SystemRitualIDImageGenerate) {
		turnActionType = models.ActionTypeImageGeneration
	}

	// Quota gate — checked after prepareChatContext so we know the model tier.
	// The check is fuzzy by design (see metering.Meter.Check); the meter's atomic
	// quota consumption at Record time is the precise enforcer.
	qd := a.meter.Check(ctx, chatJob.UserID, chatCtx.modelSubscriptionTier, turnActionType)
	if !qd.Allowed {
		quotaErr := fmt.Errorf("%w for user %s", ErrQuotaExceeded, chatJob.UserID)
		a.logger.Warn("quota check failed, rejecting message", zap.String("user_id", chatJob.UserID.String()))
		a.setJobStatusFailed(ctx, chatJob, quotaErr)
		return nil, quotaErr
	}
	var imageBytes map[uuid.UUID][]byte
	if models.UsesAnthropicMessagesAPI(chatCtx.modelProvider, chatCtx.model) ||
		models.ChatCompletionsSupportsVision(chatCtx.modelProvider, chatCtx.model) ||
		hasImageAttachmentsWithoutFileID(chatMessage.Attachments) {
		imageBytes = a.loadImageBytesForClaude(ctx, chatJob.UserID, chatMessage)
	}
	modelContext, err := a.buildModelContextForChatMessage(ctx, chatJob.UserID, chatMessage, chatCtx, imageBytes)
	if err != nil {
		a.logger.Error("failed to build model context", zap.Error(err))
		a.setJobStatusFailed(ctx, chatJob, err)
		return nil, fmt.Errorf("failed to build model context: %w", err)
	}

	agentMessage, result, err := a.generateAssistantForMessage(ctx, chatJob.UserID, chatJob, chatMessage, chatCtx, modelContext)
	if err != nil {
		if violation, ok := provider.IsSafetyViolationError(err); ok {
			// Recovery only succeeds when handleSafetyViolation returns nil error.
			// If it returns an error, we intentionally fail the job below.
			agentMessage, result, err = a.handleSafetyViolation(ctx, chatJob.UserID, chatMessage, chatCtx, violation)
		}
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			persistCtx, cancel := context.WithTimeout(context.Background(), jobTerminalPersistTimeout)
			defer cancel()
			resultID, cerr := a.setJobStatusCancelledWithPartial(persistCtx, chatJob, chatMessage, chatCtx)
			if cerr != nil {
				a.logger.Error("failed to persist cancelled chat job state", zap.Error(cerr))
				a.setJobStatusFailed(persistCtx, chatJob, cerr)
				return nil, cerr
			}
			a.recordCancelledChatUsage(persistCtx, chatJob, chatMessage, chatCtx, modelContext, qd, err, resultID)
			return nil, context.Canceled
		}
		a.logger.Error("failed to generate assistant response", zap.Error(err))
		a.setJobStatusFailedWithPartial(ctx, chatJob, chatMessage, chatCtx, err)
		return nil, err
	}
	// Persist inference milestone (job → inference_complete, tokens, chat metadata); expression/compaction follow only on success.
	if err := a.persistInferencePhase(ctx, chatJob, chatMessage, chatCtx.chat, chatCtx, result, agentMessage, true); err != nil {
		a.logger.Error("failed to persist inference phase", zap.Error(err))
		if agentMessage != nil {
			if ierr := a.advanceJobInferenceComplete(ctx, chatJob, agentMessage.ID); ierr != nil {
				a.logger.Error("failed to recover job after inference persist failure", zap.Error(ierr))
				a.setJobStatusFailed(ctx, chatJob, err)
				return nil, err
			}
			a.persistUserTurnAndChatAfterInference(ctx, chatJob.UserID, chatMessage, chatCtx.chat, chatCtx, result, true)
			if cerr := a.ds.SetChatMessageLastError(ctx, chatJob.UserID, chatMessage.ID, nil); cerr != nil {
				a.logger.Warn("failed to clear user message last_error_message after inference recovery", zap.Error(cerr))
			}
			a.runUserChatPostInferencePhases(ctx, chatJob, chatMessage, agentMessage, chatCtx, modelContext, qd)
			return agentMessage, nil
		}
		a.setJobStatusFailed(ctx, chatJob, err)
		return nil, err
	}

	a.runUserChatPostInferencePhases(ctx, chatJob, chatMessage, agentMessage, chatCtx, modelContext, qd)
	return agentMessage, nil
}

// runUserChatPostInferencePhases runs expression picking, finalizeChat (checkpoints, memories, usage metering),
// and job completion advances. Call only after persistInferencePhase succeeds; do not call when the job is failed.
func (a *Agent) runUserChatPostInferencePhases(
	ctx context.Context,
	chatJob *models.Job,
	chatMessage, agentMessage *models.ChatMessage,
	chatCtx *chatContext,
	modelContext *provider.ModelContext,
	qd metering.Decision,
) {
	if chatJob == nil {
		return
	}
	if chatJob.Status == models.JobStatusFailed || chatJob.Status == models.JobStatusCancelled {
		a.logger.Warn("skipping post-inference phases for terminal non-success job",
			zap.String("job_id", chatJob.ID.String()),
			zap.String("user_id", chatJob.UserID.String()),
			zap.String("status", string(chatJob.Status)),
		)
		return
	}

	a.recordTime(ctx, generateResponseJobDurationKey, time.Since(chatJob.CreatedAt))

	if err := a.applyExpressionPhase(ctx, chatJob.UserID, chatJob, chatCtx, modelContext, chatMessage.Message, agentMessage); err != nil {
		a.logger.Error("expression phase failed", zap.Error(err))
	}

	a.finalizeChat(ctx, chatJob.UserID, chatMessage, agentMessage, chatCtx, modelContext, qd)

	if err := a.advanceChatJobStatus(ctx, chatJob, models.JobStatusCompactionComplete); err != nil {
		a.logger.Error("failed to advance job to compaction_complete", zap.Error(err))
	}
	if err := a.advanceChatJobStatus(ctx, chatJob, models.JobStatusComplete); err != nil {
		a.logger.Error("failed to finalize job", zap.Error(err))
	}
}

// generateAssistantForMessage runs the provider dispatch and, on success, captures the
// per-turn Context X-ray (segment/token breakdown) onto the assistant message. The
// dispatch itself lives in dispatchAssistantGeneration so this wrapper is the single place
// every provider path funnels through for that capture.
func (a *Agent) generateAssistantForMessage(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatMessage *models.ChatMessage, chatCtx *chatContext, modelContext *provider.ModelContext) (*models.ChatMessage, *provider.GenerateResponse, error) {
	start := time.Now()
	defer func() {
		a.recordTime(ctx, generateResponseDurationKey, time.Since(start))
	}()

	a.recordModelContextSegmentEstimates(ctx, modelContext)

	agentMessage, result, err := a.dispatchAssistantGeneration(ctx, userID, chatJob, chatMessage, chatCtx, modelContext)
	if err == nil && agentMessage != nil {
		a.persistContextBreakdown(ctx, userID, agentMessage, chatCtx, modelContext, result)
	}
	return agentMessage, result, err
}

// dispatchAssistantGeneration routes a turn to the correct provider path (mock, local,
// image ritual, or a real vendor). See generateAssistantForMessage for the surrounding
// telemetry and context-breakdown capture.
func (a *Agent) dispatchAssistantGeneration(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatMessage *models.ChatMessage, chatCtx *chatContext, modelContext *provider.ModelContext) (*models.ChatMessage, *provider.GenerateResponse, error) {
	// Branch: mock mode serves every turn in-process. Placed before the
	// image-ritual branch so mock mode guarantees no provider or
	// image-generation network call; image rituals take a fixture branch inside
	// handleImageGenerateRitual.
	if a.mockLLM {
		return a.generateAssistantForMessageMock(ctx, userID, chatJob, chatMessage, chatCtx, modelContext)
	}
	// Branch: local mode routes every turn to the real local model server
	// (ignoring the chat's chosen vendor model, since only one local model is
	// configured), same placement rationale as mock mode above.
	if a.localLLM {
		return a.generateAssistantForMessageLocal(ctx, userID, chatJob, chatMessage, chatCtx, modelContext)
	}
	// Branch: system ritual image generation (bypasses OpenAI native image_generation tool).
	if hasSystemRitual(chatMessage.Rituals, SystemRitualIDImageGenerate) {
		return a.handleImageGenerateRitual(ctx, userID, chatMessage, chatCtx, modelContext)
	}
	if models.IsGeminiModel(chatCtx.modelProvider, chatCtx.model) {
		return a.generateAssistantForMessageGemini(ctx, userID, chatJob, chatMessage, chatCtx, modelContext)
	}
	if models.UsesOpenAIChatCompletionsAPI(chatCtx.modelProvider, chatCtx.model) {
		return a.generateAssistantForMessageOpenAIChatCompletions(ctx, userID, chatJob, chatMessage, chatCtx, modelContext)
	}
	if models.UsesAnthropicMessagesAPI(chatCtx.modelProvider, chatCtx.model) {
		return a.generateAssistantForMessageClaude(ctx, userID, chatJob, chatMessage, chatCtx, modelContext)
	}
	return a.generateAssistantForMessageOpenAI(ctx, userID, chatJob, chatMessage, chatCtx, modelContext)
}

// generationOptions parameterizes runGeneration over the small ways the five
// generateAssistantForMessage* paths differ: the label used in error/save
// messages, an optional provider-specific tool-call merge (native web search
// results), and an optional post-save step (OpenAI's attachment persistence).
type generationOptions struct {
	provider       string
	mergeToolCalls func(toolCalls []*models.ToolCall) []*models.ToolCall
	afterSave      func(ctx context.Context, agentMessage *models.ChatMessage)
}

// runGeneration drives an already-configured AgentAdapter through the shared
// draft-buffer → handleAgentLoop → tool-call merge → saveAgentResponse
// pipeline used by every assistant-generation path — OpenAI, Claude, Gemini,
// the OpenAI-compatible Chat Completions providers, and the mock adapter.
// Centralizing it here means the real and mock paths cannot drift on the
// invariants that matter (final text equals concatenated deltas, tool-call
// persistence, draft/final reconciliation, save shape): callers differ only
// in how their AgentAdapter and generationOptions are constructed.
func (a *Agent) runGeneration(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatMessage *models.ChatMessage, chatCtx *chatContext, adapter provider.AgentAdapter, opts generationOptions) (*models.ChatMessage, *provider.GenerateResponse, error) {
	draftBuffer := newJobDraftDeltaBuffer(a.lifecycleCtx, a.ds, a.logger, chatJob, jobDraftDeltaFlushMinChars, jobDraftDeltaFlushMaxWait)
	adapter.SetTextDeltaHandler(draftBuffer.HandleDelta)
	defer draftBuffer.Flush()

	result, toolCalls, generatedAttachments, err := a.handleAgentLoop(ctx, chatCtx, adapter)
	if err != nil {
		return nil, nil, fmt.Errorf("%s agent loop failed: %w", opts.provider, err)
	}
	if result == nil {
		return nil, nil, fmt.Errorf("%s agent loop returned nil response with no error", opts.provider)
	}
	if streamed := strings.TrimSpace(draftBuffer.allText); streamed != "" {
		result.Text = streamed
	}
	chatCtx.webSearchCount = adapter.WebSearchCompletedCount()

	if opts.mergeToolCalls != nil {
		toolCalls = opts.mergeToolCalls(toolCalls)
	}
	toolCalls = append(toolCalls, memoryToolCallsForChatContext(chatCtx)...)
	a.recordToolCalls(ctx, toolCalls)

	personalityName := a.resolvePersonalityName(ctx, userID, chatCtx.chat.PersonalityID)
	moodID := activeMoodID(chatCtx.activeMood)
	agentMessage, err := a.saveAgentResponse(ctx, userID, chatMessage.ChatID, result, toolCalls, generatedAttachments, chatCtx.model, personalityName, moodID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to save %s agent response: %w", opts.provider, err)
	}

	if opts.afterSave != nil {
		opts.afterSave(ctx, agentMessage)
	}

	return agentMessage, result, nil
}

// generateAssistantForMessageMock mirrors the real provider paths but drives a
// per-request MockAdapter through the same shared runGeneration pipeline, so
// mock mode exercises the code real chat runs (streamed-text reconciliation,
// tool-call recording, personality/mood, save). Image rituals route to
// handleImageGenerateRitual, which persists an embedded fixture PNG through
// the real save path under mock mode.
func (a *Agent) generateAssistantForMessageMock(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatMessage *models.ChatMessage, chatCtx *chatContext, modelContext *provider.ModelContext) (*models.ChatMessage, *provider.GenerateResponse, error) {
	if hasSystemRitual(chatMessage.Rituals, SystemRitualIDImageGenerate) {
		return a.handleImageGenerateRitual(ctx, userID, chatMessage, chatCtx, modelContext)
	}

	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{
		Mode:           a.mockLLMMode,
		EchoText:       chatMessage.Message,
		FixedResponses: a.mockFixedResponses,
		ChunkDelay:     a.mockStreamDelay,
	})
	return a.runGeneration(ctx, userID, chatJob, chatMessage, chatCtx, adapter, generationOptions{provider: "mock"})
}

func mergedRitualIDsForTools(chatMessage *models.ChatMessage, activeMood *models.Mood) []uuid.UUID {
	if chatMessage == nil {
		return moodRitualIDs(activeMood)
	}
	dbRituals, _ := SplitRituals(chatMessage.Rituals)
	ritualIDs := GetRitualIds(dbRituals)
	ritualIDs = append(ritualIDs, moodRitualIDs(activeMood)...)
	return ritualIDs
}

func mergeRitualSets(base []*models.Ritual, extra []*models.Ritual) []*models.Ritual {
	if len(extra) == 0 {
		return base
	}
	out := make([]*models.Ritual, 0, len(base)+len(extra))
	seen := make(map[uuid.UUID]struct{}, len(base)+len(extra))
	for _, r := range base {
		if r == nil {
			continue
		}
		out = append(out, r)
		seen[r.ID] = struct{}{}
	}
	for _, r := range extra {
		if r == nil {
			continue
		}
		if _, exists := seen[r.ID]; exists {
			continue
		}
		out = append(out, r)
		seen[r.ID] = struct{}{}
	}
	return out
}

func hasImageAttachmentsWithoutFileID(attachments []*models.FileAttachment) bool {
	for _, att := range attachments {
		if att == nil || !strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
			continue
		}
		if att.FileID == nil || strings.TrimSpace(*att.FileID) == "" {
			return true
		}
	}
	return false
}

func (a *Agent) loadMoodRituals(ctx context.Context, userID uuid.UUID, activeMood *models.Mood) []*models.Ritual {
	if activeMood == nil || len(activeMood.RitualIDs) == 0 {
		return nil
	}
	rituals, err := a.ds.GetRitualsByIDs(ctx, userID, activeMood.RitualIDs)
	if err != nil {
		a.logger.Warn("failed to load mood rituals", zap.String("mood_id", activeMood.ID.String()), zap.Error(err))
		return nil
	}
	return rituals
}

func (a *Agent) shouldExposeMoodTools(ctx context.Context, userID uuid.UUID, chat *models.Chat) bool {
	if chat == nil || chat.PersonalityID == uuid.Nil {
		return false
	}
	if !chat.IsAutoMood {
		return false
	}
	moods, err := a.ds.GetMoodsForPersonality(ctx, userID, chat.PersonalityID)
	if err != nil {
		a.logger.Warn("failed to load moods for tool visibility",
			zap.String("chat_id", chat.ID.String()),
			zap.Error(err))
		return false
	}
	return len(moods) > 0
}

// openAIResponseParamsForChat assembles tools (when enabled) and delegates to ModelContext.BuildOpenAIResponseParams.
func (a *Agent) openAIResponseParamsForChat(ctx context.Context, chatCtx *chatContext, userID uuid.UUID, chatMessage *models.ChatMessage, modelCtx *provider.ModelContext) responses.ResponseNewParams {
	var toolParams []responses.ToolUnionParam
	parallel := false
	policy := a.buildTurnToolPolicy(ctx, chatCtx, userID, chatMessage)
	if policy.toolsEnabled {
		parallel = true
		chatTools := getChatTools(ToolConfig{
			DisabledTools: policy.disabledTools,
		})
		agentTools := getAgentToolsList(policy.disabledTools, policy.showMoodTools)
		mcpTools := a.getChatMCPTools(ctx, userID, chatMessage.ChatID, policy.ritualIDs, chatCtx.model)
		toolParams = provider.BuildOpenAITools(chatCtx.model, chatTools, agentTools, mcpTools)
	}
	a.recordToolDefinitionEstimate(modelCtx, toolParams)
	var include []responses.ResponseIncludable
	if policy.toolsEnabled && !policy.disabledTools[tools.ToolNameWebSearch] {
		include = []responses.ResponseIncludable{
			responses.ResponseIncludableWebSearchCallResults,
			responses.ResponseIncludableWebSearchCallActionSources,
		}
	}
	return modelCtx.BuildOpenAIResponseParams(provider.OpenAIResponseParamsOptions{
		Model:             chatCtx.model,
		SafetyUserID:      userID.String(),
		MaxOutputTokens:   provider.DefaultMaxContentLength,
		ParallelToolCalls: parallel,
		Tools:             toolParams,
		Instructions:      "",
		Include:           include,
	})
}

func (a *Agent) generateAssistantForMessageOpenAI(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatMessage *models.ChatMessage, chatCtx *chatContext, modelContext *provider.ModelContext) (*models.ChatMessage, *provider.GenerateResponse, error) {
	params := a.openAIResponseParamsForChat(ctx, chatCtx, userID, chatMessage, modelContext)
	adapter := provider.NewOpenAIAdapter(a.OpenAIProvider, params)

	return a.runGeneration(ctx, userID, chatJob, chatMessage, chatCtx, adapter, generationOptions{
		provider: "OpenAI",
		mergeToolCalls: func(toolCalls []*models.ToolCall) []*models.ToolCall {
			return mergeWebSearchToolCalls(toolCalls, webSearchToolCallsFromOpenAIResponses(adapter.AllRawResponses()...))
		},
		// Persist any image/code-interpreter attachments (OpenAI-specific). The
		// unified loop returns a provider-agnostic GenerateResponse, so we
		// retrieve the raw response from the adapter to pass provider-specific
		// metadata to SaveMessageAttachments.
		afterSave: func(ctx context.Context, agentMessage *models.ChatMessage) {
			if rawResp := adapter.LastRawResponse(); rawResp != nil {
				a.OpenAIProvider.SaveMessageAttachments(ctx, userID, agentMessage.ID, rawResp)
			}
		},
	})
}

// claudeProviderForModel selects the Anthropic-Messages-API provider for the model
// and reports whether it is native Anthropic. z.ai GLM models share the wire format
// but use a different client (a.ZAIProvider) and do not support Anthropic-native tool
// features (web search, beta MCP).
func (a *Agent) claudeProviderForModel(chatCtx *chatContext) (prov *provider.ClaudeProvider, nativeAnthropic bool, err error) {
	if models.IsZAIModel(chatCtx.modelProvider, chatCtx.model) {
		if a.ZAIProvider == nil {
			return nil, false, fmt.Errorf("z.ai model %q requested but ZAI_API_KEY is not configured", chatCtx.model)
		}
		return a.ZAIProvider, false, nil
	}
	if a.ClaudeProvider == nil {
		return nil, false, fmt.Errorf("Claude model %q requested but ANTHROPIC_API_KEY is not configured", chatCtx.model)
	}
	return a.ClaudeProvider, true, nil
}

func (a *Agent) generateAssistantForMessageClaude(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatMessage *models.ChatMessage, chatCtx *chatContext, modelContext *provider.ModelContext) (*models.ChatMessage, *provider.GenerateResponse, error) {

	claudeProvider, nativeAnthropic, err := a.claudeProviderForModel(chatCtx)
	if err != nil {
		return nil, nil, err
	}

	// Build the full message list from DB history + context injections.
	// User images use base64 blocks when `UserMessageImage.RawBytes` is populated
	// (see `loadImageBytesForClaude`); otherwise the user turn falls back to text-only.
	claudeParams := modelContext.BuildClaudeParams(chatCtx.model)

	policy := a.buildTurnToolPolicy(ctx, chatCtx, userID, chatMessage)
	// Anthropic-native features (beta MCP, native web search) are not available on
	// z.ai's compatible endpoint — gate them to native Anthropic only.
	var mcpConfig *provider.ClaudeMCPConfig
	if policy.toolsEnabled && nativeAnthropic {
		mcpConfig = a.getChatClaudeMCPConfig(ctx, userID, chatMessage.ChatID, policy.ritualIDs)
	}

	claudeFunctionTools := claudeFunctionTools(tools.AgentFunctionToolSpecs(policy.showMoodTools))
	a.recordToolDefinitionEstimate(modelContext, claudeFunctionTools)
	webSearchEnabled := policy.toolsEnabled && nativeAnthropic && !policy.disabledTools[tools.ToolNameWebSearch]
	adapter := provider.NewClaudeAdapter(claudeProvider, claudeParams, claudeFunctionTools, webSearchEnabled, mcpConfig, policy.disabledTools)

	return a.runGeneration(ctx, userID, chatJob, chatMessage, chatCtx, adapter, generationOptions{
		provider: "Claude",
		mergeToolCalls: func(toolCalls []*models.ToolCall) []*models.ToolCall {
			toolCalls = mergeWebSearchToolCalls(toolCalls, webSearchToolCallsFromClaudeMessages(adapter.AllRawMessages()...))
			return mergeWebSearchToolCalls(toolCalls, webSearchToolCallsFromClaudeBetaMessages(adapter.AllRawBetaMessages()...))
		},
	})
}

// generateAssistantForMessageGemini drives a chat turn through Google's
// OpenAI-compatible Chat Completions API. It mirrors the Claude path but omits
// Anthropic-native features (web search, MCP).
func (a *Agent) generateAssistantForMessageGemini(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatMessage *models.ChatMessage, chatCtx *chatContext, modelContext *provider.ModelContext) (*models.ChatMessage, *provider.GenerateResponse, error) {
	if a.GeminiProvider == nil {
		return nil, nil, fmt.Errorf("Gemini model %q requested but GEMINI_API_KEY is not configured", chatCtx.model)
	}

	geminiParams := modelContext.BuildGeminiParams(chatCtx.model)

	policy := a.buildTurnToolPolicy(ctx, chatCtx, userID, chatMessage)
	geminiFunctionTools := geminiFunctionTools(tools.AgentFunctionToolSpecs(policy.showMoodTools))
	a.recordToolDefinitionEstimate(modelContext, geminiFunctionTools)
	toolNames := make([]string, 0, len(geminiFunctionTools))
	for _, t := range geminiFunctionTools {
		if t.OfFunction != nil {
			toolNames = append(toolNames, t.OfFunction.Function.Name)
		}
	}
	a.logger.Info("gemini turn starting",
		zap.String("model", chatCtx.model),
		zap.String("chat_id", chatMessage.ChatID.String()),
		zap.Strings("tools", toolNames),
	)
	adapter := provider.NewGeminiAdapter(a.GeminiProvider, geminiParams, geminiFunctionTools, policy.disabledTools, a.logger)

	return a.runGeneration(ctx, userID, chatJob, chatMessage, chatCtx, adapter, generationOptions{provider: "Gemini"})
}

// generateAssistantForMessageLocal drives a chat turn through a real local
// OpenAI-compatible Chat Completions server (LLM_BACKEND=local), following
// the same draft-buffer → handleAgentLoop → saveAgentResponse pipeline as
// every other provider path. Unlike vendor paths, it always targets
// a.localLLMModel rather than chatCtx.model — only one local model is
// configured, so there is no per-chat model routing to honor. Image rituals
// still take the mock fixture branch (image generation stays a "deliberate
// mock behavior" consumer, same as under LLM_BACKEND=mock).
func (a *Agent) generateAssistantForMessageLocal(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatMessage *models.ChatMessage, chatCtx *chatContext, modelContext *provider.ModelContext) (*models.ChatMessage, *provider.GenerateResponse, error) {
	if hasSystemRitual(chatMessage.Rituals, SystemRitualIDImageGenerate) {
		return a.handleImageGenerateRitual(ctx, userID, chatMessage, chatCtx, modelContext)
	}
	if a.LocalProvider == nil {
		return nil, nil, fmt.Errorf("LLM_BACKEND=local but no local model server is configured")
	}

	renderCtx := modelContext.Clone()
	renderCtx.PrepareForTextOnlyChatCompletions()
	params := renderCtx.BuildOpenAIChatCompletionParams(a.localLLMModel)

	policy := a.buildTurnToolPolicy(ctx, chatCtx, userID, chatMessage)
	functionTools := openAIChatCompletionFunctionTools(tools.AgentFunctionToolSpecs(policy.showMoodTools))
	a.recordToolDefinitionEstimate(modelContext, functionTools)

	adapter := provider.NewLocalAdapter(a.LocalProvider, params, functionTools, policy.disabledTools)
	draftBuffer := newJobDraftDeltaBuffer(a.lifecycleCtx, a.ds, a.logger, chatJob, jobDraftDeltaFlushMinChars, jobDraftDeltaFlushMaxWait)
	adapter.SetTextDeltaHandler(draftBuffer.HandleDelta)
	defer draftBuffer.Flush()

	result, toolCalls, generatedAttachments, err := a.handleAgentLoop(ctx, chatCtx, adapter)
	if err != nil {
		return nil, nil, fmt.Errorf("local model agent loop failed: %w", err)
	}
	if result == nil {
		return nil, nil, fmt.Errorf("local model agent loop returned nil response with no error")
	}
	if streamed := strings.TrimSpace(draftBuffer.allText); streamed != "" {
		result.Text = streamed
	}
	chatCtx.webSearchCount = adapter.WebSearchCompletedCount()

	toolCalls = append(toolCalls, memoryToolCallsForChatContext(chatCtx)...)
	a.recordToolCalls(ctx, toolCalls)

	personalityName := a.resolvePersonalityName(ctx, userID, chatCtx.chat.PersonalityID)
	moodID := activeMoodID(chatCtx.activeMood)
	agentMessage, err := a.saveAgentResponse(ctx, userID, chatMessage.ChatID, result, toolCalls, generatedAttachments, chatCtx.model, personalityName, moodID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to save local model agent response: %w", err)
	}

	return agentMessage, result, nil
}

// generateAssistantForMessageOpenAIChatCompletions drives a chat turn through an
// OpenAI-compatible Chat Completions API (Mistral, DeepSeek, Qwen, Xiaomi MiMo).
// Text-only models strip multimodal segments via PrepareForTextOnlyChatCompletions;
// vision-capable models (Gemini on its own path; Qwen 3.7+/Mistral medium+ heuristics
// here) keep images. Gemini uses a separate path for tool-call compatibility.
func (a *Agent) generateAssistantForMessageOpenAIChatCompletions(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatMessage *models.ChatMessage, chatCtx *chatContext, modelContext *provider.ModelContext) (*models.ChatMessage, *provider.GenerateResponse, error) {
	renderCtx := modelContext.Clone()
	if !models.ChatCompletionsSupportsVision(chatCtx.modelProvider, chatCtx.model) {
		renderCtx.PrepareForTextOnlyChatCompletions()
	}
	params := renderCtx.BuildOpenAIChatCompletionParams(chatCtx.model)

	policy := a.buildTurnToolPolicy(ctx, chatCtx, userID, chatMessage)
	functionTools := openAIChatCompletionFunctionTools(tools.AgentFunctionToolSpecs(policy.showMoodTools))
	a.recordToolDefinitionEstimate(modelContext, functionTools)

	adapter, err := a.openAIChatCompletionsAdapter(chatCtx, params, functionTools, policy.disabledTools)
	if err != nil {
		return nil, nil, err
	}

	return a.runGeneration(ctx, userID, chatJob, chatMessage, chatCtx, adapter, generationOptions{provider: string(chatCtx.modelProvider)})
}

func (a *Agent) openAIChatCompletionsAdapter(chatCtx *chatContext, params openai.ChatCompletionNewParams, functionTools []openai.ChatCompletionToolUnionParam, disabledTools map[string]bool) (provider.AgentAdapter, error) {
	switch models.ProviderForModel(chatCtx.modelProvider, chatCtx.model) {
	case models.ModelProviderMistral:
		if a.MistralProvider == nil {
			return nil, fmt.Errorf("Mistral model %q requested but MISTRAL_API_KEY is not configured", chatCtx.model)
		}
		return provider.NewMistralAdapter(a.MistralProvider, params, functionTools, disabledTools), nil
	case models.ModelProviderDeepSeek:
		if a.DeepSeekProvider == nil {
			return nil, fmt.Errorf("DeepSeek model %q requested but DEEPSEEK_API_KEY is not configured", chatCtx.model)
		}
		return provider.NewDeepSeekAdapter(a.DeepSeekProvider, params, functionTools, disabledTools), nil
	case models.ModelProviderQwen:
		if a.QwenProvider == nil {
			return nil, fmt.Errorf("Qwen model %q requested but QWEN_API_KEY is not configured", chatCtx.model)
		}
		return provider.NewQwenAdapter(a.QwenProvider, params, functionTools, disabledTools), nil
	case models.ModelProviderXiaomi:
		if a.XiaomiProvider == nil {
			return nil, fmt.Errorf("Xiaomi model %q requested but XIAOMI_API_KEY is not configured", chatCtx.model)
		}
		return provider.NewXiaomiAdapter(a.XiaomiProvider, params, functionTools, disabledTools), nil
	default:
		return nil, fmt.Errorf("unsupported OpenAI Chat Completions provider %q for model %q", chatCtx.modelProvider, chatCtx.model)
	}
}

func (a *Agent) recordToolCalls(ctx context.Context, toolCalls []*models.ToolCall) {
	a.recordCountHistogram(ctx, totalToolCallsHistogramKey, int64(len(toolCalls)))
	for _, toolCall := range toolCalls {
		attrs := metric.WithAttributes(attribute.String("type", toolCall.ToolName), attribute.Bool("error", toolCall.ToolError != ""))
		a.recordCounter(ctx, toolCallCountKey, 1, attrs)
	}
}

func (a *Agent) recordTime(ctx context.Context, name string, duration time.Duration, attributes ...metric.RecordOption) {
	if a.telemetry == nil || a.telemetry.Metrics == nil {
		return
	}
	a.telemetry.Metrics.RecordTime(ctx, name, duration, attributes...)
}

func (a *Agent) recordCounter(ctx context.Context, name string, count int64, attributes ...metric.AddOption) {
	if a.telemetry == nil || a.telemetry.Metrics == nil {
		return
	}
	a.telemetry.Metrics.RecordCounter(ctx, name, count, attributes...)
}

func (a *Agent) recordCountHistogram(ctx context.Context, name string, count int64, attributes ...metric.RecordOption) {
	if a.telemetry == nil || a.telemetry.Metrics == nil {
		return
	}
	a.telemetry.Metrics.RecordCountHistogram(ctx, name, count, attributes...)
}

func (a *Agent) recordModelContextSegmentEstimates(ctx context.Context, modelContext *provider.ModelContext) {
	if a.telemetry == nil || a.telemetry.Metrics == nil || modelContext == nil {
		return
	}
	counter := a.tokenCounter
	if counter == nil {
		counter = provider.NewTokenCounter()
	}
	estimates := modelContext.EstimatedTokensBySegment(counter)
	if len(estimates) == 0 {
		return
	}
	m := make(map[string]int64, len(estimates))
	for k, v := range estimates {
		if v > 0 {
			m[string(k)] = int64(v)
		}
	}
	a.telemetry.Metrics.RecordSegmentTokenEstimates(ctx, m, telemetry.CallPathFromContext(ctx))
}

// recordToolDefinitionEstimate records the cl100k estimate for schemas passed out-of-band
// from ModelContext. Vendor usage remains authoritative; the X-ray's remainder absorbs
// provider framing, image input, and any tokenization differences.
func (a *Agent) recordToolDefinitionEstimate(modelContext *provider.ModelContext, definitions any) {
	if modelContext == nil || definitions == nil {
		return
	}
	encoded, err := json.Marshal(definitions)
	encodedText := string(encoded)
	if err != nil || len(encoded) == 0 || encodedText == "null" || encodedText == "[]" {
		return
	}
	counter := a.tokenCounter
	if counter == nil {
		counter = provider.NewTokenCounter()
	}
	if tokens, err := counter.CountTokens(encodedText); err == nil {
		modelContext.SetAdditionalTokenEstimate(provider.SegmentKindToolDefinitions, tokens)
	}
}

// buildContextBreakdown snapshots the model context into a per-turn, per-segment token
// breakdown (the "Context X-ray"). Context segments and tool definitions are best-effort
// cl100k estimates; when vendor input usage is available, the total is authoritative and
// an explicit remainder captures provider framing, image input, and other un-attributable cost.
func (a *Agent) buildContextBreakdown(chatCtx *chatContext, modelContext *provider.ModelContext, inputTokens int64, toolCalls []*models.ToolCall) *modeltypes.ContextBreakdown {
	if modelContext == nil {
		return nil
	}
	counter := a.tokenCounter
	if counter == nil {
		counter = provider.NewTokenCounter()
	}
	stats := modelContext.SegmentBreakdown(counter)
	if len(stats) == 0 {
		return nil
	}
	segments := make([]modeltypes.ContextSegmentStat, 0, len(stats))
	total := 0
	for _, s := range stats {
		segments = append(segments, modeltypes.ContextSegmentStat{
			Kind:      string(s.Kind),
			Segments:  s.Segments,
			Tokens:    s.Tokens,
			Cacheable: s.Cacheable,
			Images:    s.Images,
		})
		total += s.Tokens
	}
	currentToolTokens, currentToolSegments := a.countCurrentToolCallTokens(toolCalls)
	if currentToolTokens > 0 {
		merged := false
		for i := range segments {
			if segments[i].Kind != string(provider.SegmentKindToolResult) {
				continue
			}
			segments[i].Tokens += currentToolTokens
			segments[i].Segments += currentToolSegments
			merged = true
			break
		}
		if !merged {
			segments = append(segments, modeltypes.ContextSegmentStat{
				Kind:     string(provider.SegmentKindToolResult),
				Segments: currentToolSegments,
				Tokens:   currentToolTokens,
			})
		}
		total += currentToolTokens
	}
	if inputTokens > 0 {
		// ContextBreakdown uses ints for JSON/UI compatibility. Clamp only an
		// implausibly large provider report rather than allowing a narrow-int
		// overflow to turn billed usage negative.
		maxInt := int64(^uint(0) >> 1)
		if inputTokens > maxInt {
			inputTokens = maxInt
		}
		billed := int(inputTokens)
		if remainder := billed - total; remainder > 0 {
			segments = append(segments, modeltypes.ContextSegmentStat{
				Kind:     "vendor_prompt_other",
				Segments: 1,
				Tokens:   remainder,
			})
		}
		total = billed
	}
	breakdown := &modeltypes.ContextBreakdown{
		Version:     modeltypes.ContextBreakdownVersion,
		Segments:    segments,
		TotalTokens: total,
		// Use the same ceiling as checkpoint policy so the Context X-ray gauge
		// continues to reflect compaction behavior as the window evolves.
		BudgetTokens: checkpointMaxLastInputTokens,
		CapturedAt:   time.Now().UTC(),
	}
	if chatCtx != nil {
		breakdown.Model = chatCtx.model
		breakdown.Provider = chatCtx.modelProvider
	}
	return breakdown
}

// countCurrentToolCallTokens estimates the dynamic tool-call material replayed during
// this turn. Tool calls are persisted on the assistant message only after generation;
// counting them here keeps the X-ray faithful without changing the model context used
// for inference. Provider-specific wrappers remain in vendor_prompt_other.
func (a *Agent) countCurrentToolCallTokens(toolCalls []*models.ToolCall) (tokens, segments int) {
	if len(toolCalls) == 0 {
		return 0, 0
	}
	counter := a.tokenCounter
	if counter == nil {
		counter = provider.NewTokenCounter()
	}
	for _, toolCall := range toolCalls {
		if toolCall == nil {
			continue
		}
		segments++
		for _, content := range []string{toolCall.ToolInput, toolCall.ToolOutput, toolCall.ToolError} {
			if content == "" {
				continue
			}
			if n, err := counter.CountTokens(content); err == nil {
				tokens += n
			}
		}
	}
	return tokens, segments
}

// persistContextBreakdown captures and stores the model-context X-ray for an assistant
// message. Best-effort: failures are logged and never surfaced to the turn, and the value
// is also set on the in-memory message so callers that keep using it stay consistent.
func (a *Agent) persistContextBreakdown(ctx context.Context, userID uuid.UUID, agentMessage *models.ChatMessage, chatCtx *chatContext, modelContext *provider.ModelContext, result *provider.GenerateResponse) {
	if agentMessage == nil {
		return
	}
	inputTokens := int64(0)
	if result != nil {
		inputTokens = result.InputTokens
	}
	breakdown := a.buildContextBreakdown(chatCtx, modelContext, inputTokens, agentMessage.ToolCalls)
	if breakdown == nil {
		return
	}
	agentMessage.ContextBreakdown = breakdown
	if err := a.ds.SetChatMessageContextBreakdown(ctx, userID, agentMessage.ID, breakdown); err != nil {
		a.logger.Warn("failed to persist context breakdown",
			zap.String("chat_message_id", agentMessage.ID.String()),
			zap.Error(err),
		)
	}
}

// updateJobStatus updates the job status in the database
func (a *Agent) updateJobStatus(ctx context.Context, job *models.Job, status models.JobStatus) error {
	job.Status = status
	updatedJob, err := a.ds.UpdateJob(ctx, job.UserID, *job)
	if err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}
	*job = *updatedJob
	return nil
}

type jobDraftDeltaBuffer struct {
	persistParent context.Context
	ds            *datastore.Datastore
	logger        *zap.Logger
	userID        uuid.UUID
	jobID         uuid.UUID
	minChunkChars int
	maxWait       time.Duration

	mu        sync.Mutex
	pending   string
	allText   string
	lastFlush time.Time
}

func newJobDraftDeltaBuffer(
	persistParent context.Context,
	ds *datastore.Datastore,
	logger *zap.Logger,
	job *models.Job,
	minChunkChars int,
	maxWait time.Duration,
) *jobDraftDeltaBuffer {
	if job == nil || ds == nil || job.ID == uuid.Nil || job.UserID == uuid.Nil {
		return &jobDraftDeltaBuffer{}
	}
	if persistParent == nil {
		persistParent = context.Background()
	}
	if minChunkChars <= 0 {
		minChunkChars = 1
	}
	if maxWait <= 0 {
		maxWait = 250 * time.Millisecond
	}
	return &jobDraftDeltaBuffer{
		persistParent: persistParent,
		ds:            ds,
		logger:        logger,
		userID:        job.UserID,
		jobID:         job.ID,
		minChunkChars: minChunkChars,
		maxWait:       maxWait,
		lastFlush:     time.Now(),
	}
}

func (b *jobDraftDeltaBuffer) HandleDelta(delta string) {
	if b == nil || b.ds == nil || delta == "" {
		return
	}
	var flushChunk string
	now := time.Now()
	b.mu.Lock()
	b.pending += delta
	b.allText += delta
	shouldFlush := len(b.pending) >= b.minChunkChars || now.Sub(b.lastFlush) >= b.maxWait
	if shouldFlush {
		flushChunk = b.pending
		b.pending = ""
		b.lastFlush = now
	}
	b.mu.Unlock()

	if flushChunk != "" {
		b.persist(flushChunk)
	}
}

func (b *jobDraftDeltaBuffer) Flush() {
	if b == nil || b.ds == nil {
		return
	}
	b.mu.Lock()
	flushChunk := b.pending
	b.pending = ""
	b.lastFlush = time.Now()
	b.mu.Unlock()
	if flushChunk != "" {
		b.persist(flushChunk)
	}
}

func (b *jobDraftDeltaBuffer) persist(chunk string) {
	if chunk == "" || b.ds == nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(b.persistParent, jobDraftDeltaPersistTimeout)
	defer cancel()
	if err := b.ds.AppendJobDraftDeltas(writeCtx, b.userID, b.jobID, []string{chunk}); err != nil && b.logger != nil {
		b.logger.Warn("failed to append job draft delta",
			zap.String("job_id", b.jobID.String()),
			zap.Int("chunk_chars", len(chunk)),
			zap.Error(err))
	}
}

// memoryEnrichmentToolCallName is the display-only pseudo tool-call name used to surface
// the automatic per-turn memory enrichment in the chat UI. It is not a real dispatched
// tool; toolContextPolicyFor excludes it from cross-turn persistence since the same
// memories are re-fetched fresh each turn.
const memoryEnrichmentToolCallName = "Load Memory"

func memoryToolCall(memories []string) *models.ToolCall {
	output := fmt.Sprintf("Retrieved memories:\n\n %s", strings.Join(memories, "\n\n"))
	return &models.ToolCall{
		ToolName:   memoryEnrichmentToolCallName,
		ToolOutput: output,
	}
}

const memoryEnrichmentFailureMessage = "Failed to retrieve memories"

func failedMemoryEnrichmentToolCall() *models.ToolCall {
	return &models.ToolCall{
		ToolName:  memoryEnrichmentToolCallName,
		ToolError: memoryEnrichmentFailureMessage,
	}
}

func additionalContextItemsFromChatContext(chatCtx *chatContext) []models.AdditionalContextItem {
	if chatCtx == nil || len(chatCtx.memories) == 0 {
		return nil
	}

	out := make([]models.AdditionalContextItem, 0, len(chatCtx.memories))
	for _, formatted := range chatCtx.memories {
		if strings.TrimSpace(formatted) == "" {
			continue
		}
		if strings.HasPrefix(formatted, "Note on recalled memories:") {
			continue
		}
		item := models.AdditionalContextItem{Type: models.AdditionalContextTypeMemory, Content: formatted}
		if strings.HasPrefix(formatted, "The user's name is ") {
			out = append(out, item)
			continue
		}
		if mem := matchLiveMemoryByFormattedContent(formatted, chatCtx.liveMemories); mem != nil {
			id := mem.ID
			item.MemoryID = &id
			item.Scope = normalizeMemoryScope(mem.Scope)
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func memoryToolCallsForChatContext(chatCtx *chatContext) []*models.ToolCall {
	if chatCtx == nil {
		return nil
	}
	if chatCtx.memoryEnrichmentFailed {
		return []*models.ToolCall{failedMemoryEnrichmentToolCall()}
	}
	if len(chatCtx.memories) > 0 {
		return []*models.ToolCall{memoryToolCall(chatCtx.memories)}
	}
	return nil
}

// prepareChatContext prepares the chat context including chat, memories, and model selection
func (a *Agent) prepareChatContext(ctx context.Context, userID uuid.UUID, chatMessage *models.ChatMessage) (*chatContext, error) {
	// Get parent chat
	parentChat, err := a.ds.GetChat(ctx, userID, chatMessage.ChatID)
	if err != nil {
		return nil, fmt.Errorf("failed to get chat: %w", err)
	}

	// Get relevant memories.
	memories, liveMemories, memoryEnrichmentFailed := a.getMemoriesBestEffort(ctx, userID, chatMessage.ChatID, parentChat.PersonalityID, chatMessage.Message)
	// Resolve model from the chat's model_id (authoritative). Do not trust model_name
	// alone — it can be stale, and a missing edge used to fall through to defaultModel
	// (gpt-5.1) even when the user selected a different provider.
	model, modelProvider, modelSubscriptionTier := a.resolveModelForChat(ctx, parentChat)
	if err := a.assertUserCanRunChatModel(ctx, userID, parentChat, modelProvider); err != nil {
		return nil, err
	}

	// ExpressionsEnabled comes from the personality edge already eager-loaded by GetChat.
	expressionsEnabled := parentChat.PersonalityExpressionsEnabled

	return &chatContext{
		chat:                   parentChat,
		memories:               memories,
		liveMemories:           liveMemories,
		memoryEnrichmentFailed: memoryEnrichmentFailed,
		model:                  model,
		modelProvider:          modelProvider,
		modelSubscriptionTier:  modelSubscriptionTier,
		expressionsEnabled:     expressionsEnabled,
	}, nil
}

// resolveModelForChat loads the effective model name, provider, and tier
// for a chat turn. model_id on the chat row is authoritative; model_name is a fallback
// only when the ID lookup fails. The returned tier is the model's raw
// SubscriptionTier string ("" when unknown); the meter classifies it for gating.
func (a *Agent) resolveModelForChat(ctx context.Context, parentChat *models.Chat) (modelName, modelProvider, subscriptionTier string) {
	modelName = defaultModel
	modelProvider = string(models.ModelProviderOpenAI)

	if parentChat == nil {
		return modelName, modelProvider, subscriptionTier
	}

	var dbModel *models.Model
	if a.ds != nil && parentChat.ModelID != uuid.Nil {
		if m, err := a.ds.GetModel(ctx, parentChat.ModelID); err == nil {
			dbModel = m
		} else {
			a.logger.Warn("chat model_id lookup failed; falling back to model_name or default",
				zap.String("chat_id", parentChat.ID.String()),
				zap.String("model_id", parentChat.ModelID.String()),
				zap.Error(err),
			)
		}
	}
	if dbModel == nil && a.ds != nil {
		if name := strings.TrimSpace(parentChat.ModelName); name != "" {
			if m, err := a.ds.GetModelByName(ctx, name); err == nil {
				dbModel = m
			} else if !errors.Is(err, datastore.ErrModelNotFound) {
				a.logger.Warn("chat model_name lookup failed",
					zap.String("chat_id", parentChat.ID.String()),
					zap.String("model_name", name),
					zap.Error(err),
				)
			}
		}
	}
	if dbModel != nil {
		provider := string(models.ProviderForModel(dbModel.Provider, dbModel.Name))
		return dbModel.Name, provider, dbModel.SubscriptionTier
	}

	if name := strings.TrimSpace(parentChat.ModelName); name != "" {
		// No DB row: keep the chat's model name but default provider/tier. Do not
		// infer provider from the name here — stale or orphan names must not route
		// to experimental providers without a catalog row.
		modelName = name
		return modelName, modelProvider, subscriptionTier
	}

	if parentChat.ModelID != uuid.Nil {
		a.logger.Warn("chat has model_id but model row is missing; using default model",
			zap.String("chat_id", parentChat.ID.String()),
			zap.String("model_id", parentChat.ModelID.String()),
			zap.String("default_model", defaultModel),
		)
	}
	return modelName, modelProvider, subscriptionTier
}

func (a *Agent) assertUserCanRunChatModel(ctx context.Context, userID uuid.UUID, parentChat *models.Chat, modelProvider string) error {
	if a.ds == nil || parentChat == nil || !models.IsExperimentalProvider(modelProvider) {
		return nil
	}
	if parentChat.ModelID == uuid.Nil {
		return nil
	}
	u, err := a.ds.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to load user for experimental model check: %w", err)
	}
	if u.EnableExperimentalModels {
		return nil
	}
	a.logger.Info("experimental model not allowed for user",
		zap.String("user_id", userID.String()),
		zap.String("chat_id", parentChat.ID.String()),
		zap.String("model_provider", modelProvider),
	)
	return fmt.Errorf("experimental model provider %q: %w", modelProvider, datastore.ErrExperimentalModelNotAllowed)
}

func (a *Agent) getMemoriesForEnrichment(ctx context.Context, userID uuid.UUID, chatID uuid.UUID, personalityID uuid.UUID, userMessage string) ([]string, []*models.Memory, error) {
	if a.testHooks.GetMemoriesOverride != nil {
		formatted, err := a.testHooks.GetMemoriesOverride(ctx, userID, chatID, personalityID, userMessage)
		return formatted, nil, err
	}
	// Mock/local mode: memory enrichment needs a provider call (query inference +
	// embeddings), so it is a deliberate no-op rather than a surprise
	// deny-transport failure mid-flow.
	if a.nonVendorLLM() {
		a.logger.Debug("mock/local mode: skipping memory enrichment", zap.String("chat_id", chatID.String()))
		return nil, nil, nil
	}
	return a.getMemories(ctx, userID, chatID, personalityID, userMessage)
}

// getMemoriesBestEffort attempts memory enrichment and degrades gracefully on any failure.
// When it fails, it logs the error and returns an empty memory list along with a failure flag.
func (a *Agent) getMemoriesBestEffort(ctx context.Context, userID uuid.UUID, chatID uuid.UUID, personalityID uuid.UUID, userMessage string) ([]string, []*models.Memory, bool) {
	memories, liveMemories, err := a.getMemoriesForEnrichment(ctx, userID, chatID, personalityID, userMessage)
	if err != nil {
		// Note: the underlying memory retrieval path logs errors at the failure site(s).
		// Keep this at Debug to avoid duplicate error logs while still attaching user/chat context.
		a.logger.Info("memory enrichment failed; proceeding without memories",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err),
		)
		return []string{}, nil, true
	}
	return memories, liveMemories, false
}

// appendMemoryMessages adds memory messages to the input message list
func appendMemoryMessages(messages []responses.ResponseInputItemUnionParam, memories []string) []responses.ResponseInputItemUnionParam {
	if len(memories) > 0 {
		memoryText := fmt.Sprintf("Here are potentially relevant memories:\n\n %s", strings.Join(memories, "\n\n"))
		messages = append(messages, responses.ResponseInputItemParamOfMessage(memoryText, provider.RoleDeveloper))
	}
	return messages
}

// appendAttachmentMessages adds attachment information to the input message list
func appendAttachmentMessages(messages []responses.ResponseInputItemUnionParam, attachments []*models.FileAttachment) []responses.ResponseInputItemUnionParam {
	attachmentLabels := buildAttachmentLabels(attachments)
	if len(attachmentLabels) > 0 {
		attachmentText := fmt.Sprintf("The user has attached the following files to their message: %s", strings.Join(attachmentLabels, ", "))
		messages = append(messages, responses.ResponseInputItemParamOfMessage(attachmentText, provider.RoleDeveloper))
	}
	return messages
}

// buildAttachmentLabels creates human-readable labels for attachments
func buildAttachmentLabels(attachments []*models.FileAttachment) []string {
	labels := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.FileID != nil {
			labels = append(labels, fmt.Sprintf("%s (file_id: %s)", attachment.Name, *attachment.FileID))
		}
	}
	return labels
}

// prepareUserMessage prepares the user message, enriching it with rituals if applicable.
// The message is prefixed with a [sys:RFC3339] timestamp derived from chatMessage.SentAt.
// Falls back to time.Now() only when SentAt is zero (e.g. ephemeral/job-constructed messages).
func (a *Agent) prepareUserMessage(ctx context.Context, userID uuid.UUID, chatMessage *models.ChatMessage) (string, error) {
	tz, _ := middleware.GetClientTimezoneFromContext(ctx)
	normalizedTZ := normalizeTimezoneName(tz)

	sentAt := chatMessage.SentAt
	if sentAt.IsZero() {
		sentAt = time.Now()
	}

	body := chatMessage.Message
	if len(chatMessage.Rituals) > 0 {
		enrichedMessage, err := a.EnrichUserMessageWithRituals(ctx, userID, chatMessage)
		if err != nil {
			a.logger.Error("failed to enrich user message with rituals", zap.Error(err))
		} else {
			body = enrichedMessage
		}
	}

	return formatUserMessageWithTime(sentAt, normalizedTZ, body), nil
}

var tzLocationCache sync.Map // map[string]*time.Location

// formatUserMessageWithTime prefixes body with a [sys:RFC3339] timestamp tag.
// If t is zero the body is returned unchanged (no prefix injected for unknown times).
func formatUserMessageWithTime(t time.Time, tz string, body string) string {
	if t.IsZero() {
		return body
	}
	loc := resolveTimezoneLocation(tz)
	return fmt.Sprintf("[sys:%s] %s", t.In(loc).Format(time.RFC3339), body)
}

func normalizeTimezoneName(tz string) string {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return "UTC"
	}
	return tz
}

func resolveTimezoneLocation(tz string) *time.Location {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return time.UTC
	}

	if cached, ok := tzLocationCache.Load(tz); ok {
		if loc, ok := cached.(*time.Location); ok && loc != nil {
			return loc
		}
	}

	loc, err := time.LoadLocation(tz)
	if err != nil || loc == nil {
		// If tz is invalid or not available in the runtime, fall back to UTC.
		loc = time.UTC
	}
	tzLocationCache.Store(tz, loc)
	return loc
}

// saveAgentResponse saves the agent's response message and tool calls using the
// provider-agnostic GenerateResponse. OpenAI-native attachment persistence is
// handled separately by the OpenAI adapter path.
func (a *Agent) saveAgentResponse(ctx context.Context, userID, chatID uuid.UUID, result *provider.GenerateResponse, toolCalls []*models.ToolCall, generatedAttachments []*models.FileAttachment, generationModel string, generationPersonality string, generationMoodID *uuid.UUID) (*models.ChatMessage, error) {
	agentMessage, err := a.ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID:                chatID,
		Message:               result.Text,
		Origin:                models.MessageOriginAssistant,
		ResponseID:            &result.ID,
		Tokens:                result.OutputTokens,
		GenerationModel:       generationModel,
		GenerationPersonality: generationPersonality,
		GenerationMoodID:      generationMoodID,
		ToolCalls:             toolCalls,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create chat message: %w", err)
	}

	// Persist any tool-generated attachments, attaching them to this message.
	persistAttempts := 0
	persistSuccesses := 0
	var persistFirstErr error
	for _, a0 := range generatedAttachments {
		if a0 == nil {
			continue
		}
		content := strings.TrimSpace(a0.FileContent)
		if content == "" {
			a.logger.Warn("skipping empty tool-generated attachment",
				zap.String("chat_message_id", agentMessage.ID.String()),
			)
			continue
		}
		name := strings.TrimSpace(a0.Name)
		if name == "" {
			name = fmt.Sprintf("image_%s.png", uuid.NewString())
		}
		fileType := strings.TrimSpace(a0.FileType)
		if fileType == "" {
			fileType = "image/png"
		}
		persistAttempts++
		created, err := a.ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
			Name:          name,
			FileType:      fileType,
			ChatMessageID: &agentMessage.ID,
		})
		if err != nil {
			if persistFirstErr == nil {
				persistFirstErr = err
			}
			a.logger.Error("failed to persist tool-generated attachment",
				zap.String("chat_message_id", agentMessage.ID.String()),
				zap.String("name", name),
				zap.Error(err),
			)
			continue
		}
		persistSuccesses++

		rawBytes, decodeErr := base64.StdEncoding.DecodeString(content)
		if decodeErr != nil || len(rawBytes) == 0 {
			a.logger.Warn("failed to decode tool-generated attachment content",
				zap.String("chat_message_id", agentMessage.ID.String()),
				zap.String("attachment_id", created.ID.String()),
				zap.String("name", name),
				zap.Error(decodeErr),
			)
			continue
		}

		var s3Key string
		if strings.HasPrefix(fileType, models.ImageMIMEPrefix) {
			// Keep generated images on the canonical gallery path (full-size + thumb).
			imageutil.UploadBytesToGalleryPath(ctx, a.fileStore, a.logger, userID, created.ID, name, fileType, rawBytes)
			s3Key = storage.FileKeyForImage(userID, created.ID, name)
		} else {
			chatIDRef := agentMessage.ChatID
			s3Key = storage.FileKeyForAttachment(userID, created.ID, name, fileType, &chatIDRef, nil)
			if uploadErr := a.fileStore.UploadFile(ctx, s3Key, rawBytes, fileType); uploadErr != nil {
				a.logger.Error("failed to upload tool-generated attachment",
					zap.String("chat_message_id", agentMessage.ID.String()),
					zap.String("attachment_id", created.ID.String()),
					zap.String("name", name),
					zap.String("s3_key", s3Key),
					zap.Error(uploadErr),
				)
				continue
			}
		}

		if err := a.ds.SetFileAttachmentS3Key(ctx, userID, created.ID, s3Key); err != nil {
			a.logger.Warn("failed to persist attachment s3_key after tool-generated attachment save",
				zap.String("attachment_id", created.ID.String()),
				zap.String("s3_key", s3Key),
				zap.Error(err))
		}
	}

	// Reload message so returned model includes attachments created post-save.
	if refreshed, err := a.ds.GetChatMessage(ctx, userID, agentMessage.ID); err == nil && refreshed != nil {
		agentMessage = refreshed
	} else if err != nil {
		a.logger.Error("failed to reload assistant message after saving attachments",
			zap.String("chat_message_id", agentMessage.ID.String()),
			zap.Error(err),
		)
	}

	// Surface complete attachment failure so callers can react (e.g., mark job failed / retry).
	if persistAttempts > 0 && persistSuccesses == 0 {
		return agentMessage, fmt.Errorf("failed to persist all tool-generated attachments: %w", persistFirstErr)
	}
	return agentMessage, nil
}

func (a *Agent) resolvePersonalityName(ctx context.Context, userID, personalityID uuid.UUID) string {
	if personalityID == uuid.Nil {
		return ""
	}
	personality, err := a.ds.GetPersonality(ctx, userID, personalityID)
	if err != nil || personality == nil {
		return ""
	}
	return strings.TrimSpace(personality.Name)
}

// finalizeChat finalizes the chat by generating a name if needed and extracting memories
func (a *Agent) finalizeChat(ctx context.Context, userID uuid.UUID, chatMessage, agentMessage *models.ChatMessage, chatCtx *chatContext, modelContext *provider.ModelContext, qd metering.Decision) {
	start := time.Now()
	defer func() {
		a.recordTime(ctx, postProcessMessageDurationKey, time.Since(start))
	}()
	// Generate chat name if it's still the default
	if chatCtx.chat.Name == defaultChatName {
		chatName, err := a.generateChatName(ctx, chatMessage.Message)
		if err != nil {
			a.logger.Error("failed to generate chat name", zap.Error(err))
		} else {
			chatCtx.chat.Name = chatName
			_, err = a.ds.UpdateChat(ctx, userID, *chatCtx.chat)
			if err != nil {
				a.logger.Error("failed to update chat name", zap.Error(err))
			}
		}
	}

	// Perform any post-message processing (checkpointing, scratchpad updates, memory extraction, etc.)

	a.postMessageProcessing(ctx, userID, chatMessage, agentMessage, chatCtx, modelContext, models.ActionTypeChatMessage, qd, false)
}

func isFirstCheckpoint(chatCtx *chatContext) bool {
	return chatCtx.chat.CheckpointSummary == ""
}

func maxTurnsBeforeCheckpoint(chatCtx *chatContext) int {
	if isFirstCheckpoint(chatCtx) {
		return checkpointMaxAssistantMessagesSinceStart
	}
	return checkpointMaxAssistantMessagesSinceSummary
}

func (a *Agent) postMessageProcessing(ctx context.Context, userID uuid.UUID, chatMessage, agentMessage *models.ChatMessage, chatCtx *chatContext, modelContext *provider.ModelContext, actionType string, qd metering.Decision, skipUsageRecording bool) {
	if !skipUsageRecording {
		// Record usage event (fire-and-forget, never blocks normal flow).
		//
		// When the generate_image tool is invoked during a chat or job turn, this
		// records the LLM turn cost (chat_message / job_run) while generateImageTool
		// separately records per-image image_generation events. Both charges are
		// intentional: the LLM call that decided to use the tool has its own cost,
		// and each generated image is an additional billable action.
		// Resolve the image-generation reclassification agent-side so the meter
		// receives an authoritative action type and stays free of ritual knowledge.
		recordAction := actionType
		if hasSystemRitual(chatMessage.Rituals, SystemRitualIDImageGenerate) {
			recordAction = models.ActionTypeImageGeneration
		}
		a.meter.Record(ctx, qd, metering.Usage{
			UserID:     userID,
			ActionType: recordAction,
			Model:      chatCtx.model,
			ChatID:     chatMessage.ChatID.String(),
			Tokens:     chatMessage.Tokens,
		})

		if actionType == models.ActionTypeChatMessage && chatCtx != nil && chatCtx.webSearchCount > 0 {
			// The meter prices web search by count and skips a zero charge.
			a.meter.Record(ctx, qd, metering.Usage{
				UserID:         userID,
				ActionType:     models.ActionTypeWebSearch,
				Model:          chatCtx.model,
				ChatID:         chatMessage.ChatID.String(),
				WebSearchCount: chatCtx.webSearchCount,
			})
		}
	}

	// Mock/local mode: checkpoint summarization requires provider calls (summary
	// inference, scratchpad/memory archival), so compaction is intentionally
	// skipped rather than allowed to hit the deny transport mid-checkpoint.
	if a.nonVendorLLM() {
		a.logger.Debug("mock/local mode: skipping checkpoint evaluation", zap.String("chat_id", chatMessage.ChatID.String()))
		return
	}

	assistantMessageCount, err := a.ds.GetChatMessageCount(ctx, userID, chatMessage.ChatID, models.MessageOriginFilterAssistant)
	if err != nil {
		a.logger.Error("failed to get assistant message count", zap.Error(err))
		return
	}

	// Token-based heuristics: chatMessage.Tokens holds the last call's input token usage.
	lastInputTokens := int(chatMessage.Tokens)

	estimatedContextTokens := 0
	if n, err := a.OpenAIProvider.CountTokens(chatCtx.chat.CheckpointSummary); err == nil {
		estimatedContextTokens += n
	}
	if n, err := a.OpenAIProvider.CountTokens(chatCtx.chat.Scratchpad); err == nil {
		estimatedContextTokens += n
	}
	decision := decideCheckpoint(checkpointPolicy{
		MinAssistantMessagesSinceCheckpoint: maxTurnsBeforeCheckpoint(chatCtx),
		MaxLastInputTokens:                  checkpointMaxLastInputTokens,
		MaxEstimatedContextTokens:           checkpointMaxEstimatedContextTokens,
		MinTurnsBetweenCheckpoints:          checkpointMinTurnsBetweenCheckpoints,
	}, checkpointInputs{
		TotalAssistantMessages:          assistantMessageCount,
		CheckpointAssistantMessageCount: chatCtx.chat.CheckpointUserMessageCount, // Backward-compatible storage field; currently tracks checkpoint message count.
		LastInputTokens:                 lastInputTokens,
		EstimatedContextTokens:          estimatedContextTokens,
	})

	if !decision.ShouldCheckpoint {
		return
	}
	attrs := metric.WithAttributes(
		telemetry.InputTokenAttr(),
	)
	a.recordCountHistogram(ctx, telemetry.Tokens, int64(estimatedContextTokens), attrs)
	a.recordCountHistogram(ctx, postProcessMessageCountKey, int64(chatCtx.chat.CheckpointUserMessageCount))
	a.logger.Debug("checkpointing chat",
		zap.String("chat_id", chatMessage.ChatID.String()),
		zap.String("reason", decision.Reason),
		zap.String("user", userID.String()),
		zap.Int("assistant_messages_since_checkpoint", decision.AssistantMessagesSinceCheck),
		zap.Int("assistant_message_count", assistantMessageCount),
		zap.Int("last_input_tokens", lastInputTokens),
		zap.Int("estimated_context_tokens", estimatedContextTokens),
	)
	// Only genuine OpenAI (Responses API) chats can thread checkpoints off a
	// PreviousResponseID. Anthropic, z.ai (GLM) and Gemini chats all rebuild
	// context from the DB via the "Claude" checkpoint path, whose summarizer runs
	// on GPT and whose scratchpad/memory archival runs on the Anthropic provider
	// (a no-op that logs when ANTHROPIC_API_KEY is unset).
	if models.UsesAnthropicMessagesAPI(chatCtx.modelProvider, chatCtx.model) || models.UsesOpenAIChatCompletionsAPI(chatCtx.modelProvider, chatCtx.model) {
		a.runCheckpointClaude(ctx, userID, chatMessage, agentMessage, chatCtx, assistantMessageCount, modelContext, decision.Reason)
	} else {
		a.runCheckpointOpenAI(ctx, userID, chatMessage, agentMessage, chatCtx, assistantMessageCount, modelContext, decision.Reason)
	}
}

// runCheckpointOpenAI performs the scratchpad → memory → summary checkpoint sequence for
// OpenAI-model chats using PreviousResponseID threading off the last agent response.
func (a *Agent) runCheckpointOpenAI(ctx context.Context, userID uuid.UUID, chatMessage, agentMessage *models.ChatMessage, chatCtx *chatContext, assistantMessageCount int, inferenceModelContext *provider.ModelContext, reason string) {
	// OpenAI checkpointing relies on PreviousResponseID chaining.
	if agentMessage == nil || agentMessage.ResponseID == nil || strings.TrimSpace(*agentMessage.ResponseID) == "" {
		a.logger.Error("cannot checkpoint OpenAI without a non-empty PreviousResponseID",
			zap.String("chat_id", chatMessage.ChatID.String()))
		return
	}

	// 1) Scratchpad generation
	var newScratchpadContent string
	var newScratchpadResponseID *string
	hasScratchpad := false
	if chatCtx.chat.PersonalityID != uuid.Nil {
		newScratchpad, err := a.updateScratchpad(ctx, userID, agentMessage.ResponseID, chatCtx)
		if err != nil {
			a.logger.Error("failed to update scratchpad during checkpoint", zap.Error(err))
		} else {
			newScratchpadContent = newScratchpad.Content
			newScratchpadResponseID = newScratchpad.ResponseID
			hasScratchpad = true
		}
	}

	// Open the compaction audit record now that the new scratchpad exists but BEFORE memory
	// extraction, so every merge/link/create event groups under this compaction.
	compactionEventID := a.beginCompactionEvent(ctx, userID, chatCtx, inferenceModelContext, chatMessage.ChatID, "OpenAI", reason, agentMessage.ID, newScratchpadContent, hasScratchpad)

	// 2) Memory extraction — hangs off the scratchpad response so the model sees the scratchpad
	// delta (old vs new) without advancing the user thread pointer. If scratchpad generation
	// failed, intentionally defer both extraction and roll-forward dedupe to the next checkpoint:
	// compaction requires that delta, and a later checkpoint safely retries it.
	if hasScratchpad {
		a.extractMemoriesWithScratchpadDelta(ctx, userID, chatMessage.ChatID, newScratchpadResponseID, inferenceModelContext, chatCtx, compactionEventID)
	}

	// 3) Conversation summarization
	summary, err := a.summarizeConversationForCheckpoint(ctx, userID, chatCtx, checkpointSummarySource{
		PreviousResponseID: agentMessage.ResponseID,
	})
	if err != nil {
		a.logger.Error("failed to summarize conversation for checkpoint", zap.Error(err))
		return
	}

	if a.persistCheckpointSummary(ctx, userID, chatMessage.ChatID, summary, assistantMessageCount, "OpenAI", agentMessage.ID) {
		a.finishCompactionEvent(ctx, userID, compactionEventID, summary)
	}
}

// runCheckpointClaude performs the scratchpad → memory → summary checkpoint sequence for
// Claude-model chats by rebuilding conversation context from the DB (no PreviousResponseID).
func (a *Agent) runCheckpointClaude(ctx context.Context, userID uuid.UUID, chatMessage, agentMessage *models.ChatMessage, chatCtx *chatContext, assistantMessageCount int, modelContext *provider.ModelContext, reason string) {
	if agentMessage == nil {
		a.logger.Warn("skipping Claude checkpoint: assistant message is nil",
			zap.String("chat_id", chatMessage.ChatID.String()))
		return
	}
	if modelContext == nil {
		a.logger.Error("skipping Claude checkpoint: model context is nil",
			zap.String("chat_id", chatMessage.ChatID.String()))
		return
	}
	// 1) Scratchpad generation.
	//
	// The scratchpad and memory steps mutate the ModelContext they are given
	// (updateScratchpadClaude appends the scratchpad-update *prompt* turn, and the
	// memory step appends the scratchpad content + extraction prompt). Those
	// instruction turns were found to confuse the summarizer into writing about the
	// scratchpad instead of the conversation. So run the scratchpad → memory steps on
	// a clone and keep the original modelContext pristine for the summarizer (step 3).
	// Inference modelContext predates the assistant reply; include it so scratchpad/memory
	// match what OpenAI sees via PreviousResponseID on the same turn.
	archivalCtx := checkpointArchivalContext(modelContext, agentMessage.Message)
	var newScratchpadContent string
	hasScratchpad := false
	var scratchpadCtx *provider.ModelContext
	if chatCtx.chat.PersonalityID != uuid.Nil {
		scratchpadCtx = archivalCtx.Clone()
		newScratchpad, err := a.updateScratchpadClaude(ctx, userID, chatCtx, scratchpadCtx)
		if err != nil {
			a.logger.Error("failed to update scratchpad during Claude checkpoint", zap.Error(err))
		} else {
			newScratchpadContent = newScratchpad.Content
			hasScratchpad = true
			scratchpadCtx.Append(provider.SegmentKindScratchpad, provider.RoleDeveloper, newScratchpad.Content, false)
		}
	}

	// Open the compaction audit record before memory extraction so merge events group under it.
	compactionEventID := a.beginCompactionEvent(ctx, userID, chatCtx, modelContext, chatMessage.ChatID, "Claude", reason, agentMessage.ID, newScratchpadContent, hasScratchpad)

	// 2) Memory extraction — old scratchpad is chatCtx.chat.Scratchpad (pre-turn value); new
	// scratchpad is the just-generated content. If scratchpad generation failed, intentionally
	// defer both extraction and roll-forward dedupe to the next checkpoint: compaction requires
	// that delta, and a later checkpoint safely retries it.
	if hasScratchpad {
		if err := a.extractMemoriesWithScratchpadDeltaClaude(ctx, userID, chatMessage.ChatID, scratchpadCtx, modelContext, chatCtx, compactionEventID); err != nil {
			a.logger.Error("failed to extract memories during Claude checkpoint", zap.Error(err))
		}
	}

	// 3) Conversation summarization — uses pristine inference modelContext plus the
	// assistant reply (explicit OpenAI input items; same prompt as the threaded path).
	summary, err := a.summarizeConversationForCheckpoint(ctx, userID, chatCtx, checkpointSummarySource{
		ModelContext:   modelContext,
		AssistantReply: agentMessage.Message,
	})
	if err != nil {
		a.logger.Error("failed to summarize conversation for Claude checkpoint", zap.Error(err))
		return
	}

	if a.persistCheckpointSummary(ctx, userID, chatMessage.ChatID, summary, assistantMessageCount, "Claude", agentMessage.ID) {
		a.finishCompactionEvent(ctx, userID, compactionEventID, summary)
	}
}

// persistCheckpointSummary writes the live checkpoint state. It returns false only when that
// authoritative chat write fails; best-effort summary-memory failures do not prevent the checkpoint
// from completing.
func (a *Agent) persistCheckpointSummary(ctx context.Context, userID, chatID uuid.UUID, summary string, assistantMessageCount int, providerName string, assistantMessageID uuid.UUID) bool {
	if a.memoryTool != nil {
		embeddingVector, err := a.memoryTool.CreateEmbedding(ctx, summary)
		if err != nil {
			a.logger.Error("failed to create summary memory embedding",
				zap.String("provider", providerName),
				zap.String("chat_id", chatID.String()),
				zap.Error(err))
		} else if err := a.ds.UpsertChatSummaryMemory(ctx, userID, chatID, summary, embeddingVector); err != nil {
			a.logger.Error("failed to upsert checkpoint summary memory",
				zap.String("provider", providerName),
				zap.String("chat_id", chatID.String()),
				zap.Error(err))
		}
	}

	// Preserve the legacy checkpoint column and clear response_id atomically so
	// conversation continuity survives summary-memory write failures.
	if err := a.ds.UpdateChatCheckpointStateAndClearResponseID(ctx, userID, chatID, summary, assistantMessageCount); err != nil {
		a.logger.Error("failed to persist chat checkpoint state and clear response id after checkpoint",
			zap.String("provider", providerName),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		return false
	}
	if assistantMessageID != uuid.Nil {
		if err := a.ds.SetChatMessageCheckpointCompletedAt(ctx, userID, assistantMessageID, time.Now().UTC()); err != nil {
			a.logger.Warn("failed to set checkpoint_completed_at on assistant message",
				zap.String("message_id", assistantMessageID.String()),
				zap.Error(err))
		}
	}
	return true
}

type SummaryMemoryBackfillStats struct {
	Processed int
	Skipped   int
	Failed    int
}

// StartSummaryMemoryBackfill copies legacy chat checkpoint summaries into
// singleton Summary memories in the background.
func (a *Agent) StartSummaryMemoryBackfill(ctx context.Context) {
	go func() {
		stats := a.BackfillSummaryMemories(ctx, summaryMemoryBackfillBatchSize)
		a.logger.Info("summary memory backfill finished",
			zap.Int("processed", stats.Processed),
			zap.Int("skipped", stats.Skipped),
			zap.Int("failed", stats.Failed))
	}()
}

func (a *Agent) BackfillSummaryMemories(ctx context.Context, batchSize int) SummaryMemoryBackfillStats {
	if batchSize <= 0 {
		batchSize = summaryMemoryBackfillBatchSize
	}

	stats := SummaryMemoryBackfillStats{}
	if a.memoryTool == nil {
		a.logger.Warn("summary memory backfill skipped because memory tool is unavailable")
		stats.Skipped++
		return stats
	}

	for {
		candidates, err := a.ds.ListChatsMissingSummaryMemory(ctx, batchSize)
		if err != nil {
			a.logger.Error("failed to list summary memory backfill candidates", zap.Error(err))
			stats.Failed++
			return stats
		}
		if len(candidates) == 0 {
			return stats
		}

		successesThisBatch := 0
		for _, candidate := range candidates {
			if strings.TrimSpace(candidate.Summary) == "" {
				stats.Skipped++
				continue
			}

			embeddingVector, err := a.memoryTool.CreateEmbedding(ctx, candidate.Summary)
			if err != nil {
				a.logger.Error("failed to create embedding for summary memory backfill",
					zap.String("chat_id", candidate.ChatID.String()),
					zap.Error(err))
				stats.Failed++
				continue
			}

			if err := a.ds.UpsertChatSummaryMemory(ctx, candidate.UserID, candidate.ChatID, candidate.Summary, embeddingVector); err != nil {
				a.logger.Error("failed to upsert summary memory during backfill",
					zap.String("chat_id", candidate.ChatID.String()),
					zap.Error(err))
				stats.Failed++
				continue
			}

			stats.Processed++
			successesThisBatch++
		}

		if len(candidates) < batchSize || successesThisBatch == 0 {
			return stats
		}
	}
}

// setJobStatusFailedWithPartial preserves already-streamed text before marking a chat turn
// failed. The user message keeps the failure banner; the partial assistant message remains
// readable rather than disappearing with the draft buffer.
func (a *Agent) setJobStatusFailedWithPartial(
	ctx context.Context,
	chatJob *models.Job,
	userMessage *models.ChatMessage,
	chatCtx *chatContext,
	cause error,
) {
	if chatJob == nil {
		a.logger.Error("cannot persist failed chat job with partial response: job is nil", zap.Error(cause))
		return
	}
	if cause == nil {
		a.logger.Error("cannot persist failed chat job with partial response: cause is nil",
			zap.String("job_id", chatJob.ID.String()))
		return
	}
	if userMessage == nil {
		a.logger.Warn("cannot preserve streamed response without the triggering user message",
			zap.String("job_id", chatJob.ID.String()))
		a.setJobStatusFailed(ctx, chatJob, cause)
		return
	}

	generationModel := ""
	generationPersonality := ""
	var moodID *uuid.UUID
	if chatCtx != nil {
		generationModel = chatCtx.model
		moodID = activeMoodID(chatCtx.activeMood)
		if chatCtx.chat != nil {
			generationPersonality = a.resolvePersonalityName(ctx, chatJob.UserID, chatCtx.chat.PersonalityID)
		}
	}
	failureMessage := strings.TrimSpace(cause.Error())
	updated, _, err := a.ds.FinalizeFailedChatJobWithPartial(
		ctx,
		chatJob.UserID,
		chatJob.ID,
		userMessage.ChatID,
		generationModel,
		generationPersonality,
		failureMessage,
		moodID,
	)
	if err != nil {
		a.logger.Error("failed to persist failed chat job with partial response", zap.Error(err))
		a.setJobStatusFailed(ctx, chatJob, cause)
		return
	}
	*chatJob = *updated
	if failureMessage == "" {
		return
	}
	if err := a.ds.SetChatMessageLastError(ctx, chatJob.UserID, userMessage.ID, &failureMessage); err != nil {
		a.logger.Warn("failed to set user message last_error_message for failed job", zap.Error(err))
	}
}

func (a *Agent) setJobStatusFailed(ctx context.Context, chatJob *models.Job, cause error) {
	if chatJob == nil {
		return
	}
	toSave := *chatJob
	toSave.Status = models.JobStatusFailed
	toSave.Error = cause.Error()
	updated, uerr := a.ds.UpdateJob(ctx, chatJob.UserID, toSave)
	if uerr != nil {
		a.logger.Error("failed to update job", zap.Error(uerr))
		return
	}
	if cerr := a.ds.ClearJobDraftDeltas(ctx, chatJob.UserID, chatJob.ID); cerr != nil {
		a.logger.Warn("failed to clear job draft deltas on failed job", zap.Error(cerr))
	}
	*chatJob = *updated

	if chatJob.JobType != JobTypeChatMessage || cause == nil {
		return
	}
	ref := strings.TrimSpace(chatJob.Reference)
	if ref == "" {
		return
	}
	userMsgID, perr := uuid.Parse(ref)
	if perr != nil {
		return
	}
	errText := strings.TrimSpace(cause.Error())
	if errText == "" {
		return
	}
	if serr := a.ds.SetChatMessageLastError(ctx, chatJob.UserID, userMsgID, &errText); serr != nil {
		a.logger.Warn("failed to set user message last_error_message for failed job", zap.Error(serr))
	}
}

func (a *Agent) setJobStatusCancelledWithPartial(ctx context.Context, chatJob *models.Job, userMessage *models.ChatMessage, chatCtx *chatContext) (*uuid.UUID, error) {
	if chatJob == nil || userMessage == nil {
		return nil, nil
	}
	generationPersonality := ""
	generationModel := ""
	var moodID *uuid.UUID
	if chatCtx != nil {
		generationPersonality = a.resolvePersonalityName(ctx, chatJob.UserID, chatCtx.chat.PersonalityID)
		generationModel = chatCtx.model
		moodID = activeMoodID(chatCtx.activeMood)
	}
	updated, resultID, finalizeErr := a.ds.FinalizeCancelledChatJobWithPartial(
		ctx,
		chatJob.UserID,
		chatJob.ID,
		userMessage.ChatID,
		generationModel,
		generationPersonality,
		moodID,
	)
	if finalizeErr != nil {
		return nil, finalizeErr
	}
	*chatJob = *updated
	return resultID, nil
}

func estimateOpenAICancelUsage(modelContext *provider.ModelContext, partialOutputText string, counter *provider.TokenCounter) (inputTokens int64, outputTokens int64, ok bool) {
	if modelContext == nil || counter == nil {
		return 0, 0, false
	}
	segmentEstimates := modelContext.EstimatedTokensBySegment(counter)
	var inputEstimate int64
	for _, n := range segmentEstimates {
		if n > 0 {
			inputEstimate += int64(n)
		}
	}
	if inputEstimate <= 0 {
		return 0, 0, false
	}
	var outputEstimate int64
	if n, err := counter.CountTokens(partialOutputText); err == nil && n > 0 {
		outputEstimate = int64(n)
	}
	return inputEstimate, outputEstimate, true
}

func cancelledInputTokensForBilling(
	err error,
	chatCtx *chatContext,
	modelContext *provider.ModelContext,
	partialOutputText string,
	counter *provider.TokenCounter,
) (tokens int64, source string, outputTokens int64, outputKnown bool, providerUsageAvailable bool) {
	const fallbackInputTokens int64 = 1
	usage, ok := provider.ExtractCancelUsage(err)
	if ok && usage.InputTokens > 0 {
		return usage.InputTokens, usage.Source, usage.OutputTokens, true, usage.Available
	}
	if chatCtx != nil && !models.IsAnthropicModel(chatCtx.modelProvider, chatCtx.model) {
		if in, out, estimated := estimateOpenAICancelUsage(modelContext, partialOutputText, counter); estimated {
			return in, "openai_local_estimate", out, true, false
		}
	}
	if ok && usage.Source != "" {
		source = usage.Source
	} else {
		source = "fallback_min_1"
	}
	return fallbackInputTokens, source, 0, false, false
}

func (a *Agent) recordCancelledChatUsage(
	ctx context.Context,
	chatJob *models.Job,
	userMessage *models.ChatMessage,
	chatCtx *chatContext,
	modelContext *provider.ModelContext,
	qd metering.Decision,
	cause error,
	partialAssistantID *uuid.UUID,
) {
	if chatJob == nil || userMessage == nil || chatCtx == nil {
		return
	}
	var partialMsg *models.ChatMessage
	partialText := ""
	if partialAssistantID != nil && *partialAssistantID != uuid.Nil {
		msg, err := a.ds.GetChatMessage(ctx, chatJob.UserID, *partialAssistantID)
		if err != nil {
			a.logger.Warn("failed to load cancelled partial assistant message for billing estimate",
				zap.String("message_id", partialAssistantID.String()),
				zap.Error(err))
		} else {
			partialMsg = msg
			partialText = msg.Message
		}
	}
	inputTokens, tokenSource, outputTokens, outputKnown, providerUsageAvailable := cancelledInputTokensForBilling(
		cause,
		chatCtx,
		modelContext,
		partialText,
		a.tokenCounter,
	)
	metadata := map[string]interface{}{
		"cancelled":                true,
		"token_source":             tokenSource,
		"input_tokens":             inputTokens,
		"provider_usage_available": providerUsageAvailable,
	}
	if outputKnown {
		metadata["output_tokens"] = outputTokens
	}
	recordAction := models.ActionTypeChatMessage
	if hasSystemRitual(userMessage.Rituals, SystemRitualIDImageGenerate) {
		recordAction = models.ActionTypeImageGeneration
	}
	a.meter.Record(ctx, qd, metering.Usage{
		UserID:     chatJob.UserID,
		ActionType: recordAction,
		Model:      chatCtx.model,
		ChatID:     userMessage.ChatID.String(),
		Tokens:     inputTokens,
		Metadata:   metadata,
	})
	if partialMsg == nil || !outputKnown {
		return
	}
	partialMsg.Tokens = outputTokens
	if _, err := a.ds.UpdateChatMessage(ctx, chatJob.UserID, *partialMsg); err != nil {
		a.logger.Warn("failed to update cancelled partial assistant output tokens",
			zap.String("message_id", partialMsg.ID.String()),
			zap.Error(err))
	}
}
