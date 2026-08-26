package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/auth"

	// No billing meter is linked in the open-source build: metering.New stays nil
	// and the server falls back to metering.NoopMeter (every turn allowed, nothing
	// recorded). A private build links a production meter via its own blank import,
	// which registers a constructor into metering.New. See package metering.
	"github.com/theimaginaryfoundation/what-iff/internal/database"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/logging"
	"github.com/theimaginaryfoundation/what-iff/internal/server"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

const (
	gracefulShutdownTimeout = 15 * time.Second
)

func envBool(name string) (bool, bool) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return false, false
	}

	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, false
	}

	return parsed, true
}

// deps bundles api-server's external dependencies behind seams so setup can
// be driven end-to-end in tests without a real logger, telemetry backend, or
// database. defaultDeps wires them to the real world; main is a thin
// exit-code wrapper around setup plus the serve loop.
//
// config itself is not seamed: server.NewConfig reads straight from the
// process environment, so tests drive it the same way production does (via
// os.Setenv/t.Setenv) rather than re-mocking its internals.
type deps struct {
	newLogger      func() (*zap.Logger, error)
	initTelemetry  func(context.Context, *zap.Logger) (*telemetry.Telemetry, error)
	newDBClient    func(*zap.Logger) (*ent.Client, *sql.DB, error)
	migrateSchema  func(context.Context, *ent.Client, ...schema.MigrateOption) error
	ensureSeedData func(context.Context, *ent.Client, *zap.Logger) error
	newServer      func(*server.Config, *zap.Logger, *telemetry.Telemetry, *ent.Client, *sql.DB) *server.Server
}

func defaultDeps() deps {
	return deps{
		newLogger:     logging.NewLogger,
		initTelemetry: telemetry.Init,
		newDBClient:   database.NewClient,
		migrateSchema: func(ctx context.Context, c *ent.Client, opts ...schema.MigrateOption) error {
			return c.Schema.Create(ctx, opts...)
		},
		ensureSeedData: database.EnsureSeedData,
		newServer:      server.NewServer,
	}
}

// apiServerRuntime holds everything main's serve loop needs once setup has
// completed successfully. It replaces the package-level srv/logger/client/
// sqlDB/tel vars that init() used to populate.
type apiServerRuntime struct {
	srv    *server.Server
	logger *zap.Logger
	client *ent.Client
	sqlDB  *sql.DB
	tel    *telemetry.Telemetry
}

// setup performs all of api-server's startup validation and wiring: flag/env
// already loaded by main, config validation gates, telemetry, database
// connection, optional auto-migration, and seed data, finishing with a
// constructed *server.Server. It returns an error instead of calling
// logger.Fatal directly so tests can drive every guard clause and branch
// without terminating the test process; main is the only place that turns a
// non-nil error into a process exit.
func setup(d deps) (*apiServerRuntime, error) {
	logger, err := d.newLogger()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}

	// Create server config and fail fast on required secrets before expensive setup.
	config := server.NewConfig()

	// Environment safety gates. ENV is canonical and ENVIRONMENT a legacy alias;
	// ambiguous configuration fails loudly instead of picking a side silently.
	if config.EnvironmentConflict {
		return nil, fmt.Errorf("ENV and ENVIRONMENT are both set and disagree; set only ENV (resolved_environment=%s)", config.Environment)
	}
	// An unrecognized LLM_BACKEND value is a fatal misconfiguration: silently
	// falling back to real providers while the operator believes they are
	// hermetic (or vice versa) would be fail-open with respect to their intent.
	switch config.LLMBackend {
	case "vendor", "mock", "local":
	default:
		return nil, fmt.Errorf("LLM_BACKEND has an invalid value; use vendor, mock, or local (value=%s)", config.LLMBackend)
	}
	// Scripted mode is test-only injection (no script can be supplied via env);
	// accepting it here would fail every chat turn with a cryptic error.
	if config.LLMBackend == "mock" && config.MockLLMMode != "echo" && config.MockLLMMode != "fixed" {
		return nil, fmt.Errorf("MOCK_LLM_MODE must be echo or fixed (value=%s)", config.MockLLMMode)
	}
	// A local backend with no model configured would fail every chat turn with
	// a cryptic provider error instead of a clear startup message.
	if config.LLMBackend == "local" && config.LocalLLMModel == "" {
		return nil, fmt.Errorf("LOCAL_LLM_MODEL is required when LLM_BACKEND=local")
	}
	// mock/local are fail-closed: honored only when ENV was explicitly set to a
	// local/test environment. The parsed Environment value defaults to
	// "development" when unset, so gating on it alone would be fail-open.
	if config.LLMBackend != "vendor" && !config.IsExplicitLocalEnv() {
		return nil, fmt.Errorf(
			"LLM_BACKEND=mock/local requires ENV explicitly set to development, test, or local (llm_backend=%s, environment=%s, environment_explicit=%t)",
			config.LLMBackend, config.Environment, config.EnvironmentExplicit,
		)
	}
	if config.LLMBackend == "mock" {
		logger.Warn("LLM_BACKEND=mock — in-process mock adapter serves all LLM calls and provider network egress is denied",
			zap.String("mock_mode", config.MockLLMMode),
			zap.String("environment", config.Environment),
		)
	}
	if config.LLMBackend == "local" {
		logger.Warn("LLM_BACKEND=local — assistant generation is served by a real local model server",
			zap.String("local_llm_base_url", config.LocalLLMBaseURL),
			zap.String("local_llm_model", config.LocalLLMModel),
			zap.String("environment", config.Environment),
		)
	}
	// Same fail-closed gate as LLM_BACKEND, checked before any expensive setup so a
	// misconfigured destructive migration fails loudly instead of silently not
	// doing what was intended. (The migration block below re-parses the flag for
	// its own warnings.)
	if destructive, ok := envBool("DESTRUCTIVE_MIGRATION"); ok && destructive && !config.IsExplicitLocalEnv() {
		return nil, fmt.Errorf(
			"DESTRUCTIVE_MIGRATION=true requires ENV explicitly set to development, test, or local (environment=%s, environment_explicit=%t)",
			config.Environment, config.EnvironmentExplicit,
		)
	}

	if _, err := datastore.ValidateTokenEncryptionSecret(config.TokenEncryptionSecret); err != nil {
		return nil, fmt.Errorf(
			"invalid TOKEN_ENCRYPTION_SECRET: %w (must be raw secret value (not ARN/reference) and at least %d characters)",
			err, datastore.MinTokenEncryptionSecretLen,
		)
	}

	// JWT_SECRET/JWT_REFRESH_SECRET are read directly from the environment in
	// internal/auth (not threaded through Config, unlike TokenEncryptionSecret
	// above), so they're validated the same way: read here with os.Getenv, not
	// config.*, to match what internal/auth actually consumes at request time.
	if err := auth.ValidateSecret("JWT_SECRET", os.Getenv("JWT_SECRET")); err != nil {
		return nil, fmt.Errorf("invalid JWT_SECRET: %w (minimum_length=%d)", err, auth.MinSecretLen)
	}
	if err := auth.ValidateSecret("JWT_REFRESH_SECRET", os.Getenv("JWT_REFRESH_SECRET")); err != nil {
		return nil, fmt.Errorf("invalid JWT_REFRESH_SECRET: %w (minimum_length=%d)", err, auth.MinSecretLen)
	}

	// Initialize telemetry (if configured)
	tel, err := d.initTelemetry(context.Background(), logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize telemetry: %w", err)
	}
	if err := tel.Metrics.InitStartupMetrics(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize startup metrics: %w", err)
	}

	// Initialize database connection
	client, sqlDB, err := d.newDBClient(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Best-effort patch so models.subscription_tier accepts "ultra" (Ent does not widen CHECKs on its own).
	database.MigrateModelSubscriptionTierUltra(context.Background(), sqlDB, logger)

	// Run migrations if AUTO_MIGRATE is set
	autoMigrate, autoMigrateSet := envBool("AUTO_MIGRATE")
	destructiveMigration, destructiveMigrationSet := envBool("DESTRUCTIVE_MIGRATION")

	if !autoMigrateSet && os.Getenv("AUTO_MIGRATE") != "" {
		logger.Warn("AUTO_MIGRATE has invalid value, defaulting to false", zap.String("value", os.Getenv("AUTO_MIGRATE")))
	}
	if !destructiveMigrationSet && os.Getenv("DESTRUCTIVE_MIGRATION") != "" {
		logger.Warn("DESTRUCTIVE_MIGRATION has invalid value, defaulting to false", zap.String("value", os.Getenv("DESTRUCTIVE_MIGRATION")))
	}
	if destructiveMigration && !autoMigrate {
		logger.Warn("DESTRUCTIVE_MIGRATION is enabled but AUTO_MIGRATE is disabled, destructive options will not run")
	}

	if autoMigrate {
		// Run Ent schema migration.
		// The historical role backfill migration is intentionally disabled because it was one-time only.
		migrationOptions := []schema.MigrateOption{}
		if destructiveMigration {
			migrationOptions = append(migrationOptions, schema.WithDropColumn(true), schema.WithDropIndex(true))
			logger.Warn("Running destructive database migration options (drop column/index) because DESTRUCTIVE_MIGRATION=true")
		}

		if err := d.migrateSchema(context.Background(), client, migrationOptions...); err != nil {
			return nil, fmt.Errorf("failed to run database migrations: %w", err)
		}
		logger.Debug("Database migrations applied", zap.Bool("destructive_migration", destructiveMigration))
	}

	// Ensure seed data exists before serving requests
	if err := d.ensureSeedData(context.Background(), client, logger); err != nil {
		return nil, fmt.Errorf("failed to seed database: %w", err)
	}

	// Create and configure server
	srv := d.newServer(config, logger, tel, client, sqlDB)

	return &apiServerRuntime{
		srv:    srv,
		logger: logger,
		client: client,
		sqlDB:  sqlDB,
		tel:    tel,
	}, nil
}

func main() {
	var envFile string
	flag.StringVar(&envFile, "env", ".env", "env file path")
	flag.Parse()

	if err := godotenv.Load(envFile); err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}

	rt, err := setup(defaultDeps())
	if err != nil {
		log.Fatal(err)
	}

	logger := rt.logger
	defer logger.Sync()

	// Local: start HTTP server with graceful shutdown
	defer rt.client.Close()

	// Run server in a goroutine so it doesn't block
	go func() {
		if err := rt.srv.Start(); err != nil {
			logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	// Wait for interrupt signal to gracefully shut down the server
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	sig := <-c
	logger.Info("Received signal, shutting down server", zap.String("signal", sig.String()))

	// Create a deadline to wait for
	ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancel()

	// Gracefully shutdown the server
	if err := rt.srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	// Shutdown telemetry
	if rt.tel != nil {
		if err := rt.tel.Shutdown(ctx); err != nil {
			logger.Error("Failed to shutdown telemetry", zap.Error(err))
		}
	}

	logger.Info("Server gracefully stopped")
}
