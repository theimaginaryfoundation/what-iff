package main

import (
	"context"
	"database/sql"
	"flag"
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

var (
	srv    *server.Server
	logger *zap.Logger
	client *ent.Client
	sqlDB  *sql.DB
	tel    *telemetry.Telemetry
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

func init() {
	var err error

	var envFile string
	flag.StringVar(&envFile, "env", ".env", "env file path")
	flag.Parse()

	if err := godotenv.Load(envFile); err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}

	// Initialize logger
	logger, err = logging.NewLogger()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	// Create server config and fail fast on required secrets before expensive setup.
	config := server.NewConfig()

	// Environment safety gates. ENV is canonical and ENVIRONMENT a legacy alias;
	// ambiguous configuration fails loudly instead of picking a side silently.
	if config.EnvironmentConflict {
		logger.Fatal("ENV and ENVIRONMENT are both set and disagree; set only ENV",
			zap.String("resolved_environment", config.Environment),
		)
	}
	// An unrecognized LLM_BACKEND value is a fatal misconfiguration: silently
	// falling back to real providers while the operator believes they are
	// hermetic (or vice versa) would be fail-open with respect to their intent.
	switch config.LLMBackend {
	case "vendor", "mock", "local":
	default:
		logger.Fatal("LLM_BACKEND has an invalid value; use vendor, mock, or local", zap.String("value", config.LLMBackend))
	}
	// Scripted mode is test-only injection (no script can be supplied via env);
	// accepting it here would fail every chat turn with a cryptic error.
	if config.LLMBackend == "mock" && config.MockLLMMode != "echo" && config.MockLLMMode != "fixed" {
		logger.Fatal("MOCK_LLM_MODE must be echo or fixed", zap.String("value", config.MockLLMMode))
	}
	// A local backend with no model configured would fail every chat turn with
	// a cryptic provider error instead of a clear startup message.
	if config.LLMBackend == "local" && config.LocalLLMModel == "" {
		logger.Fatal("LOCAL_LLM_MODEL is required when LLM_BACKEND=local")
	}
	// mock/local are fail-closed: honored only when ENV was explicitly set to a
	// local/test environment. The parsed Environment value defaults to
	// "development" when unset, so gating on it alone would be fail-open.
	if config.LLMBackend != "vendor" && !config.IsExplicitLocalEnv() {
		logger.Fatal("LLM_BACKEND=mock/local requires ENV explicitly set to development, test, or local",
			zap.String("llm_backend", config.LLMBackend),
			zap.String("environment", config.Environment),
			zap.Bool("environment_explicit", config.EnvironmentExplicit),
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
		logger.Fatal("DESTRUCTIVE_MIGRATION=true requires ENV explicitly set to development, test, or local",
			zap.String("environment", config.Environment),
			zap.Bool("environment_explicit", config.EnvironmentExplicit),
		)
	}

	if _, err := datastore.ValidateTokenEncryptionSecret(config.TokenEncryptionSecret); err != nil {
		logger.Fatal(
			"Invalid TOKEN_ENCRYPTION_SECRET",
			zap.Error(err),
			zap.Int("minimum_length", datastore.MinTokenEncryptionSecretLen),
			zap.String("requirements", "must be raw secret value (not ARN/reference) and at least minimum_length characters"),
		)
	}

	// JWT_SECRET/JWT_REFRESH_SECRET are read directly from the environment in
	// internal/auth (not threaded through Config, unlike TokenEncryptionSecret
	// above), so they're validated the same way: read here with os.Getenv, not
	// config.*, to match what internal/auth actually consumes at request time.
	if err := auth.ValidateSecret("JWT_SECRET", os.Getenv("JWT_SECRET")); err != nil {
		logger.Fatal("Invalid JWT_SECRET", zap.Error(err), zap.Int("minimum_length", auth.MinSecretLen))
	}
	if err := auth.ValidateSecret("JWT_REFRESH_SECRET", os.Getenv("JWT_REFRESH_SECRET")); err != nil {
		logger.Fatal("Invalid JWT_REFRESH_SECRET", zap.Error(err), zap.Int("minimum_length", auth.MinSecretLen))
	}

	// Initialize telemetry (if configured)
	tel, err = telemetry.Init(context.Background(), logger)
	if err != nil {
		logger.Fatal("Failed to initialize telemetry", zap.Error(err))
	}
	if err := tel.Metrics.InitStartupMetrics(context.Background()); err != nil {
		logger.Fatal("Failed to initialize startup metrics", zap.Error(err))
	}

	// Initialize database connection
	client, sqlDB, err = database.NewClient(logger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
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

		if err := client.Schema.Create(context.Background(), migrationOptions...); err != nil {
			logger.Fatal("Failed to run database migrations", zap.Error(err))
		}
		logger.Debug("Database migrations applied", zap.Bool("destructive_migration", destructiveMigration))
	}

	// Best-effort historical repair for imported conversations affected by equal timestamp ordering.
	// The repair is deliberately narrow and idempotent: only archived imported 1-user/1-assistant
	// ties with no job association and no surrounding timestamp collision are changed.
	if repair, repairErr := datastore.RepairImportedMessageOrder(context.Background(), sqlDB); repairErr != nil {
		logger.Warn("Imported message ordering repair could not complete", zap.Error(repairErr))
	} else if repair.CandidatePairs > 0 {
		logger.Info("Imported message ordering repair complete",
			zap.Int("candidate_pairs", repair.CandidatePairs),
			zap.Int("repaired_pairs", repair.RepairedPairs),
			zap.Int("collision_abstentions", repair.CollisionAbstentions),
			zap.Int("concurrent_abstentions", repair.ConcurrentAbstentions),
		)
	}

	// Ensure seed data exists before serving requests
	if err := database.EnsureSeedData(context.Background(), client, logger); err != nil {
		logger.Fatal("Failed to seed database", zap.Error(err))
	}

	// Create and configure server
	srv = server.NewServer(config, logger, tel, client, sqlDB)
}

func main() {
	defer logger.Sync()

	// Local: start HTTP server with graceful shutdown
	defer client.Close()

	// Run server in a goroutine so it doesn't block
	go func() {
		if err := srv.Start(); err != nil {
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
	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	// Shutdown telemetry
	if tel != nil {
		if err := tel.Shutdown(ctx); err != nil {
			logger.Error("Failed to shutdown telemetry", zap.Error(err))
		}
	}

	logger.Info("Server gracefully stopped")
}
