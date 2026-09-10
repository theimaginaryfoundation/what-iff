package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/embedding"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agentjobscheduler "github.com/theimaginaryfoundation/what-iff/internal/agentjobs/scheduler"
	"github.com/theimaginaryfoundation/what-iff/internal/buildinfo"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/email"
	"github.com/theimaginaryfoundation/what-iff/internal/featuregate"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/accountexport"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/agentjob"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/chat"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/health"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/imagegallery"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/job"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/mcpserver"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/memory"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/model"
	moodhandler "github.com/theimaginaryfoundation/what-iff/internal/handlers/mood"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/personality"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/ritual"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/role"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/search"
	toolshandler "github.com/theimaginaryfoundation/what-iff/internal/handlers/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/user"
	versionhandler "github.com/theimaginaryfoundation/what-iff/internal/handlers/version"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/webhook"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/pushnotify"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/userhooks"

	"github.com/gorilla/mux"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/zap"
)

type Server struct {
	config    *Config
	logger    *zap.Logger
	telemetry *telemetry.Telemetry
	router    *mux.Router
	server    *http.Server
	db        *ent.Client
	sqlDB     *sql.DB

	agentJobScheduler       *agentjobscheduler.Manager
	agentJobSchedulerCancel context.CancelFunc

	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
}

func NewServer(config *Config, logger *zap.Logger, tel *telemetry.Telemetry, db *ent.Client, sqlDB *sql.DB) *Server {
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	s := &Server{
		config:    config,
		logger:    logger,
		telemetry: tel,
		router:    mux.NewRouter(),
		db:        db,
		sqlDB:     sqlDB,

		lifecycleCtx:    lifecycleCtx,
		lifecycleCancel: lifecycleCancel,
	}

	s.setupMiddleware()
	s.setupRoutes()

	s.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%s", config.Host, config.Port),
		Handler:      s.router,
		WriteTimeout: config.WriteTimeout,
		ReadTimeout:  config.ReadTimeout,
		IdleTimeout:  config.IdleTimeout,
	}

	return s
}

func (s *Server) setupMiddleware() {
	// otelmux normally emits three HTTP server histograms (request/response size + duration)
	// with the same label cardinality as our custom metric. We only need latency here;
	// use a noop MeterProvider for the middleware so traces stay on the real tracer
	// while metricsMiddleware remains the sole HTTP histogram source.
	s.router.Use(otelmux.Middleware("chat-api", otelmux.WithMeterProvider(noopmetric.NewMeterProvider())))
	s.router.Use(s.metricsMiddleware)
	s.router.PathPrefix("/").Methods(http.MethodOptions).HandlerFunc(s.handlePreflight)
	s.router.Use(s.corsMiddleware)
}

func (s *Server) setupRoutes() {
	// Create data providers
	dataStore, err := datastore.NewDatastore(s.db, s.sqlDB, s.logger, s.config.TokenEncryptionSecret, s.telemetry.Metrics)
	if err != nil {
		s.logger.Fatal("failed to configure token encryption", zap.Error(err))
	}

	fileStore, err := storage.NewFileStore(context.Background(), s.config.S3FileBucket, s.config.AWSRegion, s.logger)
	if err != nil {
		s.logger.Fatal("failed to initialize S3 file store", zap.Error(err))
	}

	// Under a non-vendor LLM_BACKEND every provider SDK client (agent +
	// memory/admin handlers) is built on the deny-network transport: "no
	// provider egress" is enforced at the transport, not by dummy keys.
	// main.go has already fatally rejected mock/local outside an
	// explicitly-set local/test ENV. Local mode still needs its own real
	// egress to reach the local server — that client is constructed
	// separately in agent.NewAgent, not via this shared deny transport.
	var providerHTTPClient *http.Client
	if s.config.LLMBackend != "vendor" {
		providerHTTPClient = provider.DenyNetworkHTTPClient()
	}

	agentCfg := agent.AgentConfig{
		LifecycleContext: s.lifecycleCtx,
		HTTPClient:       providerHTTPClient,
		LLMBackend:       s.config.LLMBackend,
		MockLLMMode:      s.config.MockLLMMode,
		MockLLMFixedResponses: append([]string(nil),
			s.config.MockLLMFixedResponses...),
		MockLLMStreamDelay: s.config.MockLLMStreamDelay,
		LocalLLMBaseURL:    s.config.LocalLLMBaseURL,
		LocalLLMModel:      s.config.LocalLLMModel,
		ZAIKey:             s.config.ZAIKey,
		ZAIBaseURL:         s.config.ZAIBaseURL,
		GeminiKey:          s.config.GeminiKey,
		GeminiBaseURL:      s.config.GeminiBaseURL,
		MistralKey:         s.config.MistralKey,
		MistralBaseURL:     s.config.MistralBaseURL,
		DeepSeekKey:        s.config.DeepSeekKey,
		DeepSeekBaseURL:    s.config.DeepSeekBaseURL,
		QwenKey:            s.config.QwenKey,
		QwenBaseURL:        s.config.QwenBaseURL,
		XiaomiKey:          s.config.XiaomiKey,
		XiaomiBaseURL:      s.config.XiaomiBaseURL,
	}
	// The concrete meter is provided by metering.New, which the private metering
	// implementation registers via a blank import in cmd/api-server; it reads its
	// own configuration from the environment. When that package is absent (e.g.
	// open-source build), metering.New is nil and NewAgent falls back to
	// metering.NoopMeter — no wiring required here.
	if metering.New != nil {
		agentCfg.Meter = metering.New(dataStore, s.logger)
	}
	// Optional push-notification sender (linked privately; nil in the open-source
	// build, which notifies nothing). Registered via a blank import in
	// cmd/api-server, same as the meter; it reads its own credentials from the
	// environment. When absent, NewAgent falls back to pushnotify.NoopNotifier.
	if pushnotify.New != nil {
		agentCfg.PushNotifier = pushnotify.New(dataStore, s.logger)
	}
	// Optional feature-entitlement gate (linked privately; nil in the open-source
	// build, where every feature is available). Registered via a blank import in
	// cmd/api-server, same as the meter.
	if featuregate.New != nil {
		featuregate.Active = featuregate.New(dataStore)
	}
	// Optional post-registration hook (linked privately; nil in the open-source
	// build, where registration has no extra side effects). Registered via a blank
	// import in cmd/api-server, same as the meter and the entitlement gate.
	if userhooks.New != nil {
		userhooks.OnRegistered = userhooks.New(dataStore, s.logger)
	}
	agent := agent.NewAgent(dataStore, s.logger, s.telemetry, s.config.OpenAIKey, fileStore, s.config.AnthropicKey, agentCfg)
	agent.StartSummaryMemoryBackfill(context.Background())
	if s.config.EnableAgentJobsScheduler {
		instanceID := "unknown"
		if host, hostErr := os.Hostname(); hostErr == nil && host != "" {
			instanceID = host
		}

		mgr, err := agentjobscheduler.NewManager(dataStore, agent, s.logger, agentjobscheduler.Config{
			Distributed:       s.config.AgentJobsSchedulerDistributed,
			LockKey:           s.config.AgentJobsSchedulerLockKey,
			LockRetryInterval: s.config.AgentJobsSchedulerLockRetryInterval,
			LockRetryJitter:   s.config.AgentJobsSchedulerLockRetryJitter,
			InstanceID:        instanceID,
		})
		if err != nil {
			s.logger.Fatal("failed to initialize agent job scheduler", zap.Error(err))
		}
		schedCtx, cancel := context.WithCancel(context.Background())
		s.agentJobScheduler = mgr
		s.agentJobSchedulerCancel = cancel
		if s.config.AgentJobsSchedulerDistributed {
			go mgr.Run(schedCtx)
		} else {
			mgr.Start(schedCtx)
		}
		s.logger.Info("AgentJobs scheduler enabled",
			zap.Bool("enabled", true),
			zap.Bool("distributed", s.config.AgentJobsSchedulerDistributed),
			zap.Int64("lock_key", s.config.AgentJobsSchedulerLockKey),
		)
	} else {
		s.logger.Info("AgentJobs scheduler disabled (ENABLE_AGENTJOBS_SCHEDULER!=true)")
	}

	// Create handlers
	userHandler := user.NewHandler(dataStore, s.logger, s.config.AllowedEmails, s.config.Environment)
	jobHandler := job.NewHandlerWithCanceller(dataStore, agent, s.logger)
	memoryHandler := memory.NewHandler(dataStore, s.logger, s.config.OpenAIKey, providerHTTPClient)

	// Account export: async export runs in-process here in the main app; the bundle lands in the
	// file store and its download link is delivered ONLY out-of-band (a deliberate control — app
	// access alone cannot exfiltrate the account). The concrete email transport is provided by
	// email.New, which a private implementation registers via a blank import in cmd/api-server and
	// which reads its own configuration from the environment. When that package is absent (e.g. the
	// open-source build), email.New is nil and we fall back to email.NoopSender, which logs the link.
	var exportSender email.Sender = email.NoopSender{Logger: s.logger}
	if email.New != nil {
		if snd := email.New(s.logger); snd != nil {
			exportSender = snd
		}
	}
	accountExportHandler := accountexport.NewHandler(dataStore, s.logger, fileStore, exportSender, s.config.OpenAIKey)
	mcpServerHandler := mcpserver.NewHandler(dataStore, s.logger)
	modelHandler := model.NewHandler(dataStore, s.logger)
	personalityHandler := personality.NewHandler(dataStore, s.logger, agent)
	chatHandler := chat.NewHandler(dataStore, s.logger, agent, chat.HandlerConfig{
		RequireBilling: s.config.RequireBilling,
	})
	agentJobHandler := agentjob.NewHandler(dataStore, agent, s.agentJobScheduler, s.logger)
	ritualHandler := ritual.NewHandler(dataStore, s.logger)
	fileAttachmentHandler := fileattachment.NewHandler(dataStore, s.logger, agent)
	imageGalleryHandler := imagegallery.NewHandler(dataStore, s.logger, agent.FileStore(), agent)
	moodHandler := moodhandler.NewHandler(dataStore, s.logger, agent.FileStore())
	roleHandler := role.NewHandler(dataStore, s.logger)
	webhookHandler := webhook.NewHandler(dataStore, agent, s.logger)
	toolsHandler := toolshandler.NewHandler(s.logger)
	searchHandler := search.NewHandler(dataStore, s.logger)
	// Setup API routes
	apiRouter := s.router.PathPrefix("/api").Subrouter()

	// Health check with database connectivity verification
	healthHandler := health.NewHandler(s.sqlDB, s.logger)
	apiRouter.HandleFunc("/health", healthHandler.Check).Methods("GET")

	// Build provenance, stamped at compile time via -ldflags (see internal/buildinfo)
	versionHandler := versionhandler.NewHandler(buildinfo.Get(), s.logger)
	apiRouter.HandleFunc("/version", versionHandler.Get).Methods("GET")

	// Public routes
	apiRouter.HandleFunc("/user/register", userHandler.Register).Methods("POST")
	apiRouter.HandleFunc("/user/login", userHandler.Login).Methods("POST")
	apiRouter.HandleFunc("/user/refresh", userHandler.RefreshToken).Methods("POST")

	webhookRouter := apiRouter.PathPrefix("/webhooks").Subrouter()
	webhookRouter.Use(middleware.WebhookAuthMiddleware(dataStore, s.logger))
	webhookHandler.RegisterWebhookRoutes(webhookRouter)

	// Protected routes
	authRouter := apiRouter.NewRoute().Subrouter()
	authRouter.Use(middleware.AuthMiddleware(s.db, dataStore, s.logger))

	// Diagnostic route: confirms the request is authenticated and echoes the user id.
	authRouter.HandleFunc("/test/auth", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := middleware.GetUserIDFromContext(r.Context())
		if !ok {
			s.logger.Error("Test handler: No user ID in context (auth middleware should have set this)")
			handlerutils.RespondWithError(w, s.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Something went wrong. Please try again", nil)
			return
		}

		s.logger.Info("Test handler: Successfully authenticated", zap.String("user_id", userID.String()))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf(`{"status":"authenticated","user_id":"%s"}`, userID.String())))
	}).Methods("GET")

	// Register protected routes
	userHandler.RegisterRoutes(authRouter)
	webhookHandler.RegisterTokenRoutes(authRouter)
	jobHandler.RegisterRoutes(authRouter)
	chatHandler.RegisterRoutes(authRouter)
	agentJobHandler.RegisterRoutes(authRouter)
	memoryHandler.RegisterRoutes(authRouter)
	accountExportHandler.RegisterRoutes(authRouter)
	mcpServerHandler.RegisterRoutes(authRouter)
	modelRouter := apiRouter.PathPrefix("/model").Subrouter()
	modelRouter.Use(middleware.OptionalAuthMiddleware(s.db, dataStore, s.logger))
	modelRouter.HandleFunc("", modelHandler.ListModels).Methods("GET")
	personalityHandler.RegisterRoutes(authRouter)
	ritualHandler.RegisterRoutes(authRouter)
	fileAttachmentHandler.RegisterRoutes(authRouter)
	imageGalleryHandler.RegisterRoutes(authRouter)
	moodHandler.RegisterRoutes(authRouter)
	toolsHandler.RegisterRoutes(authRouter)
	searchHandler.RegisterRoutes(authRouter)

	// The /api/v1 surface. Authentication only — every route mounted here
	// applies its own authorization.
	//
	// SECURITY: routes under /api/v1 that are staff-only MUST be registered
	// through a handler's RegisterRoutes method that creates a subrouter with
	// RequireRole("admin", "super_admin") applied. Do NOT register such routes
	// directly on apiV1Router: that bypasses the role check and creates a
	// security vulnerability. This holds for plugin-registered routes too — the
	// seam hands out this router, not a pre-authorized one.
	apiV1Router := apiRouter.PathPrefix("/v1").Subrouter()
	apiV1Router.Use(middleware.AuthMiddleware(s.db, dataStore, s.logger))

	// Optional features that this repository does not ship register their routes
	// here (see internal/plugins). No plugin is linked by default, so this is a
	// no-op; a build that blank-imports one in cmd/api-server gets its routes.
	plugins.Apply(plugins.Deps{
		DataStore:       dataStore,
		Logger:          s.logger,
		PublicRouter:    apiRouter,
		AuthRouter:      authRouter,
		V1AuthRouter:    apiV1Router,
		FileStore:       fileStore,
		CreateEmbedding: pluginEmbedder(s.config.OpenAIKey, providerHTTPClient),
	})

	// Register role routes (protected with admin role middleware)
	// Role routes are prefixed with /api/v1/roles
	//
	// SECURITY: as noted on apiV1Router above - all role management routes must
	// be registered through the handler's RegisterRoutes method, which applies
	// RequireRole("admin", "super_admin") middleware.
	roleHandler.RegisterRoutes(apiV1Router)
}

// pluginEmbedder builds the embedding function handed to plugins through
// plugins.Deps, or returns nil when no key is configured so a plugin can tell
// the feature is unavailable rather than failing per request.
//
// httpClient is the deny-network transport under a non-vendor LLM_BACKEND.
// Constructing the client here rather than passing the key through the seam is
// what keeps that policy enforceable: a plugin receives the ability to embed,
// not the credential to reach the provider however it likes (ADR 0x018).
func pluginEmbedder(openAIKey string, httpClient *http.Client) func(context.Context, string) ([]float32, error) {
	if strings.TrimSpace(openAIKey) == "" {
		return nil
	}
	opts := []option.RequestOption{option.WithAPIKey(openAIKey)}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	client := openai.NewClient(opts...)
	return func(ctx context.Context, input string) ([]float32, error) {
		return embedding.CreateEmbedding(ctx, &client, input)
	}
}

func (s *Server) Start() error {
	s.logger.Info("Starting server", zap.String("address", s.server.Addr))
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.lifecycleCancel != nil {
		s.lifecycleCancel()
	}
	if s.agentJobSchedulerCancel != nil {
		s.agentJobSchedulerCancel()
	}
	if s.agentJobScheduler != nil {
		s.agentJobScheduler.Stop(ctx)
	}
	return s.server.Shutdown(ctx)
}

// Middleware functions
func (s *Server) isAllowedOrigin(origin string) bool {
	for _, allowed := range s.config.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		if origin != "" && s.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		// Keep in sync with handlePreflight (and any frontend usage).
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, Content-Length, Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")

	// Check if origin is allowed
	if origin != "" && s.isAllowedOrigin(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	w.Header().Set("Access-Control-Allow-Methods", "DELETE, GET, HEAD, OPTIONS, PATCH, POST, PUT")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusOK)
}

// =============================================================================
// Metrics middleware
// =============================================================================

// statusRecorder wraps http.ResponseWriter to capture the written status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip OPTIONS — preflight has its own handler and skews latency stats
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		s.recordHTTP(
			r.Context(),
			r.Method,
			metricHTTPRoute(r),
			rec.status,
			time.Since(start),
		)
	})
}

const metricRouteUnmatched = "unmatched"

// metricHTTPRoute returns a stable, low-cardinality route label: mux path template (preferred),
// else path regexp, else "unmatched". Never uses the raw URL path (IDs would explode series count).
func metricHTTPRoute(r *http.Request) string {
	matched := mux.CurrentRoute(r)
	if matched == nil {
		return metricRouteUnmatched
	}
	if tmpl, err := matched.GetPathTemplate(); err == nil {
		if s := normalizeMetricRoutePattern(tmpl); s != "" {
			return s
		}
	}
	if re, err := matched.GetPathRegexp(); err == nil {
		re = strings.TrimSpace(re)
		if re != "" {
			return re
		}
	}
	return metricRouteUnmatched
}

// normalizeMetricRoutePattern trims, ensures a leading slash, and collapses duplicate slashes.
func normalizeMetricRoutePattern(s string) string {
	s = strings.TrimSpace(s)
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

// httpStatusClass maps a numeric status to a coarse bucket (reduces metric cardinality vs per-code labels).
func httpStatusClass(code int) string {
	switch {
	case code >= 100 && code < 200:
		return "1xx"
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}

// recordHTTP records request latency with method, mux route template, and status class (1xx–5xx).
func (s *Server) recordHTTP(ctx context.Context, method, route string, status int, duration time.Duration) {
	if s.telemetry == nil || s.telemetry.Metrics == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("http.method", method),
		attribute.String("http.route", route),
		attribute.String("http.status_class", httpStatusClass(status)),
	)
	s.telemetry.Metrics.RecordTime(ctx, "http_server_request_duration", duration, attrs)
}
