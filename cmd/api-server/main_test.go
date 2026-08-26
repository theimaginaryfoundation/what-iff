package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/database"
	"github.com/theimaginaryfoundation/what-iff/internal/server"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"

	_ "github.com/mattn/go-sqlite3"
)

// newSeededTestDBClient hand-creates the subset of tables api-server's setup
// flow actually touches (roles, models, users, user_preferences) against an
// in-memory sqlite database, matching create-superuser's main_test.go helper
// of the same name. See that file for why the real client.Schema.Create can't
// be used against sqlite (the embeddings table's pgvector column type has no
// SQLite mapping): setup's own migrateSchema seam is stubbed to a no-op in
// tests and these tables are pre-created directly instead.
func newSeededTestDBClient(t *testing.T) (*ent.Client, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))

	statements := []string{
		`CREATE TABLE roles (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL UNIQUE,
			description text
		)`,
		`CREATE TABLE models (
			id uuid PRIMARY KEY,
			name text NOT NULL,
			display_name text NOT NULL,
			description text NOT NULL,
			provider text NOT NULL DEFAULT 'openai',
			tool_support bool NOT NULL DEFAULT false,
			base_credits_per_slab integer NOT NULL DEFAULT 1,
			subscription_tier text NOT NULL DEFAULT 'high',
			deleted bool NOT NULL DEFAULT false,
			is_default bool NOT NULL DEFAULT false
		)`,
		`CREATE TABLE users (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			username text NOT NULL UNIQUE,
			email text NOT NULL UNIQUE,
			password_hash text NOT NULL,
			first_name text,
			last_name text,
			timezone text NOT NULL DEFAULT 'America/New_York',
			status text NOT NULL DEFAULT 'active',
			enable_experimental_models bool NOT NULL DEFAULT false,
			last_login datetime,
			last_seen datetime,
			terms_accepted_at datetime,
			refresh_token_id text
		)`,
		`CREATE TABLE user_roles (
			user_id uuid NOT NULL,
			role_id uuid NOT NULL,
			PRIMARY KEY (user_id, role_id)
		)`,
		`CREATE TABLE user_preferences (
			id uuid PRIMARY KEY,
			theme text NOT NULL DEFAULT 'dark',
			last_seen_announcement text DEFAULT '',
			experimental_memory_dedupe_chain bool NOT NULL DEFAULT false,
			favorite_model_ids json NOT NULL DEFAULT '[]',
			user_preferences uuid NOT NULL UNIQUE,
			default_model uuid NOT NULL,
			default_personality uuid
		)`,
	}
	for _, stmt := range statements {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}

	return client, db
}

// validTestEnv sets every environment variable setup() requires for a clean
// run: valid JWT/token secrets, vendor LLM backend (no local/mock gates to
// satisfy), and no auto-migration (tests drive schema via
// newSeededTestDBClient + a no-op migrateSchema instead).
func validTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENV", "")
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("LLM_BACKEND", "vendor")
	t.Setenv("MOCK_LLM_MODE", "")
	t.Setenv("LOCAL_LLM_MODEL", "")
	t.Setenv("DESTRUCTIVE_MIGRATION", "")
	t.Setenv("AUTO_MIGRATE", "")
	t.Setenv("TOKEN_ENCRYPTION_SECRET", "this-is-a-valid-token-encryption-secret-value")
	t.Setenv("JWT_SECRET", "this-is-a-valid-jwt-secret-at-least-32-chars")
	t.Setenv("JWT_REFRESH_SECRET", "this-is-a-valid-jwt-refresh-secret-32-chars")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("DB_TYPE", "")
}

// baseTestDeps wires every seam to a real, working implementation against an
// in-memory sqlite database, so setup() can complete an actual happy path.
// Individual tests override just the field they want to fail.
func baseTestDeps(t *testing.T) deps {
	t.Helper()
	client, db := newSeededTestDBClient(t)
	return deps{
		newLogger: func() (*zap.Logger, error) { return zap.NewNop(), nil },
		initTelemetry: func(ctx context.Context, l *zap.Logger) (*telemetry.Telemetry, error) {
			return telemetry.Init(ctx, l)
		},
		newDBClient:    func(*zap.Logger) (*ent.Client, *sql.DB, error) { return client, db, nil },
		migrateSchema:  func(context.Context, *ent.Client, ...schema.MigrateOption) error { return nil },
		ensureSeedData: database.EnsureSeedData,
		newServer:      server.NewServer,
	}
}

func TestEnvBool(t *testing.T) {
	t.Setenv("TEST_ENV_BOOL_TRUE", "true")
	t.Setenv("TEST_ENV_BOOL_FALSE", "false")
	t.Setenv("TEST_ENV_BOOL_INVALID", "not-a-bool")

	v, ok := envBool("TEST_ENV_BOOL_TRUE")
	require.True(t, ok)
	require.True(t, v)

	v, ok = envBool("TEST_ENV_BOOL_FALSE")
	require.True(t, ok)
	require.False(t, v)

	_, ok = envBool("TEST_ENV_BOOL_INVALID")
	require.False(t, ok)

	_, ok = envBool("TEST_ENV_BOOL_UNSET_XYZ")
	require.False(t, ok)
}

func TestSetup_LoggerInitFailure(t *testing.T) {
	d := deps{
		newLogger: func() (*zap.Logger, error) { return nil, errors.New("boom") },
	}
	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to initialize logger")
}

func TestSetup_HappyPath(t *testing.T) {
	validTestEnv(t)
	d := baseTestDeps(t)

	rt, err := setup(d)
	require.NoError(t, err)
	require.NotNil(t, rt)
	require.NotNil(t, rt.srv)
	require.NotNil(t, rt.logger)
	require.NotNil(t, rt.client)
	require.NotNil(t, rt.sqlDB)
	require.NotNil(t, rt.tel)
}

func TestSetup_EnvironmentConflict(t *testing.T) {
	validTestEnv(t)
	t.Setenv("ENV", "production")
	t.Setenv("ENVIRONMENT", "development")
	d := baseTestDeps(t)

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ENV and ENVIRONMENT are both set and disagree")
}

func TestSetup_InvalidLLMBackend(t *testing.T) {
	validTestEnv(t)
	t.Setenv("LLM_BACKEND", "not-a-real-backend")
	d := baseTestDeps(t)

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "LLM_BACKEND has an invalid value")
}

func TestSetup_InvalidMockLLMMode(t *testing.T) {
	validTestEnv(t)
	t.Setenv("ENV", "test")
	t.Setenv("LLM_BACKEND", "mock")
	t.Setenv("MOCK_LLM_MODE", "not-a-real-mode")
	d := baseTestDeps(t)

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "MOCK_LLM_MODE must be echo or fixed")
}

func TestSetup_LocalBackendMissingModel(t *testing.T) {
	validTestEnv(t)
	t.Setenv("ENV", "test")
	t.Setenv("LLM_BACKEND", "local")
	t.Setenv("LOCAL_LLM_MODEL", "")
	d := baseTestDeps(t)

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "LOCAL_LLM_MODEL is required when LLM_BACKEND=local")
}

func TestSetup_MockBackendRequiresExplicitLocalEnv(t *testing.T) {
	validTestEnv(t)
	t.Setenv("LLM_BACKEND", "mock")
	t.Setenv("MOCK_LLM_MODE", "echo")
	// ENV/ENVIRONMENT left unset by validTestEnv: not explicit.
	d := baseTestDeps(t)

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "LLM_BACKEND=mock/local requires ENV explicitly set")
}

func TestSetup_LocalBackendRequiresExplicitLocalEnv(t *testing.T) {
	validTestEnv(t)
	t.Setenv("LLM_BACKEND", "local")
	t.Setenv("LOCAL_LLM_MODEL", "some-model")
	// ENV/ENVIRONMENT left unset by validTestEnv: not explicit.
	d := baseTestDeps(t)

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "LLM_BACKEND=mock/local requires ENV explicitly set")
}

func TestSetup_MockBackendAllowedWithExplicitLocalEnv(t *testing.T) {
	validTestEnv(t)
	t.Setenv("ENV", "test")
	t.Setenv("LLM_BACKEND", "mock")
	t.Setenv("MOCK_LLM_MODE", "echo")
	d := baseTestDeps(t)

	rt, err := setup(d)
	require.NoError(t, err)
	require.NotNil(t, rt)
}

func TestSetup_DestructiveMigrationRequiresExplicitLocalEnv(t *testing.T) {
	validTestEnv(t)
	t.Setenv("DESTRUCTIVE_MIGRATION", "true")
	// ENV/ENVIRONMENT left unset by validTestEnv: not explicit.
	d := baseTestDeps(t)

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "DESTRUCTIVE_MIGRATION=true requires ENV explicitly set")
}

func TestSetup_DestructiveMigrationAllowedWithExplicitLocalEnv(t *testing.T) {
	validTestEnv(t)
	t.Setenv("ENV", "test")
	t.Setenv("DESTRUCTIVE_MIGRATION", "true")
	t.Setenv("AUTO_MIGRATE", "true")
	d := baseTestDeps(t)
	migrateCalled := false
	d.migrateSchema = func(context.Context, *ent.Client, ...schema.MigrateOption) error {
		migrateCalled = true
		return nil
	}

	rt, err := setup(d)
	require.NoError(t, err)
	require.NotNil(t, rt)
	require.True(t, migrateCalled)
}

func TestSetup_InvalidTokenEncryptionSecret(t *testing.T) {
	validTestEnv(t)
	t.Setenv("TOKEN_ENCRYPTION_SECRET", "too-short")
	d := baseTestDeps(t)

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid TOKEN_ENCRYPTION_SECRET")
}

func TestSetup_InvalidJWTSecret(t *testing.T) {
	validTestEnv(t)
	t.Setenv("JWT_SECRET", "too-short")
	d := baseTestDeps(t)

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid JWT_SECRET")
}

func TestSetup_InvalidJWTRefreshSecret(t *testing.T) {
	validTestEnv(t)
	t.Setenv("JWT_REFRESH_SECRET", "too-short")
	d := baseTestDeps(t)

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid JWT_REFRESH_SECRET")
}

func TestSetup_TelemetryInitFailure(t *testing.T) {
	validTestEnv(t)
	d := baseTestDeps(t)
	d.initTelemetry = func(context.Context, *zap.Logger) (*telemetry.Telemetry, error) {
		return nil, errors.New("otel unreachable")
	}

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to initialize telemetry")
}

func TestSetup_DBClientFailure(t *testing.T) {
	validTestEnv(t)
	d := baseTestDeps(t)
	d.newDBClient = func(*zap.Logger) (*ent.Client, *sql.DB, error) {
		return nil, nil, errors.New("connection refused")
	}

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to connect to database")
}

func TestSetup_MigrationFailure(t *testing.T) {
	validTestEnv(t)
	t.Setenv("AUTO_MIGRATE", "true")
	d := baseTestDeps(t)
	d.migrateSchema = func(context.Context, *ent.Client, ...schema.MigrateOption) error {
		return errors.New("migration exploded")
	}

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to run database migrations")
}

func TestSetup_LocalBackendAllowedWithExplicitLocalEnv(t *testing.T) {
	validTestEnv(t)
	t.Setenv("ENV", "test")
	t.Setenv("LLM_BACKEND", "local")
	t.Setenv("LOCAL_LLM_MODEL", "some-model")
	d := baseTestDeps(t)

	rt, err := setup(d)
	require.NoError(t, err)
	require.NotNil(t, rt)
}

func TestSetup_AutoMigrateInvalidValueDefaultsFalse(t *testing.T) {
	validTestEnv(t)
	t.Setenv("AUTO_MIGRATE", "not-a-bool")
	d := baseTestDeps(t)
	migrateCalled := false
	d.migrateSchema = func(context.Context, *ent.Client, ...schema.MigrateOption) error {
		migrateCalled = true
		return nil
	}

	rt, err := setup(d)
	require.NoError(t, err)
	require.NotNil(t, rt)
	require.False(t, migrateCalled, "invalid AUTO_MIGRATE value should default to false, not run migrations")
}

func TestSetup_DestructiveMigrationInvalidValueDefaultsFalse(t *testing.T) {
	validTestEnv(t)
	t.Setenv("ENV", "test")
	t.Setenv("DESTRUCTIVE_MIGRATION", "not-a-bool")
	t.Setenv("AUTO_MIGRATE", "true")
	d := baseTestDeps(t)
	var gotOpts []schema.MigrateOption
	d.migrateSchema = func(_ context.Context, _ *ent.Client, opts ...schema.MigrateOption) error {
		gotOpts = opts
		return nil
	}

	rt, err := setup(d)
	require.NoError(t, err)
	require.NotNil(t, rt)
	require.Empty(t, gotOpts, "invalid DESTRUCTIVE_MIGRATION value should default to false, not add drop options")
}

func TestSetup_DestructiveMigrationEnabledButAutoMigrateDisabled(t *testing.T) {
	validTestEnv(t)
	t.Setenv("ENV", "test")
	t.Setenv("DESTRUCTIVE_MIGRATION", "true")
	t.Setenv("AUTO_MIGRATE", "false")
	d := baseTestDeps(t)
	migrateCalled := false
	d.migrateSchema = func(context.Context, *ent.Client, ...schema.MigrateOption) error {
		migrateCalled = true
		return nil
	}

	rt, err := setup(d)
	require.NoError(t, err)
	require.NotNil(t, rt)
	require.False(t, migrateCalled, "AUTO_MIGRATE=false should skip migration even with DESTRUCTIVE_MIGRATION=true")
}

func TestSetup_SeedFailure(t *testing.T) {
	validTestEnv(t)
	d := baseTestDeps(t)
	d.ensureSeedData = func(context.Context, *ent.Client, *zap.Logger) error {
		return errors.New("seed exploded")
	}

	_, err := setup(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to seed database")
}
