package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"

	_ "github.com/mattn/go-sqlite3"
)

// newTestDBClient opens a fresh in-memory sqlite ent.Client, unmigrated — run's own
// migrateSchema call is what creates the tables, same as it does in production.
func newTestDBClient(t *testing.T) (*ent.Client, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB(dialect.SQLite, db)
	return ent.NewClient(ent.Driver(drv)), db
}

// newSeededTestDBClient hand-creates the subset of tables the create-superuser flow
// actually touches (users, roles, user_roles, models, user_preferences) and returns a
// no-op migrateSchema stub to use in place of the real one.
//
// The real ent schema declares the `embeddings.embedding` column as a Postgres-only
// pgvector type with no SQLite mapping (ent/schema/embedding.go), so the real
// client.Schema.Create always fails against SQLite for the whole-schema migration —
// confirmed empirically: even pre-creating the embeddings table with a placeholder
// column type doesn't help, since ent's SQLite migrator rebuilds the table via a
// rename-copy-drop strategy that still targets the unsupported type. None of the
// tables create-superuser's flow needs are affected by this — only embeddings is —
// so hand-rolling just what's needed and skipping the real migration call sidesteps it
// without weakening what these tests actually verify.
func newSeededTestDBClient(t *testing.T) (*ent.Client, *sql.DB, func(context.Context, *ent.Client) error) {
	t.Helper()
	client, db := newTestDBClient(t)

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

	noopMigrate := func(context.Context, *ent.Client) error { return nil }
	return client, db, noopMigrate
}

// seedExistingUser inserts a user, optionally with the admin role, into an
// already-hand-migrated client (see newSeededTestDBClient) so a test can exercise
// run's existing-user branches.
func seedExistingUser(t *testing.T, client *ent.Client, email string, admin bool) {
	t.Helper()
	ctx := context.Background()

	now := time.Now().UTC()
	roleName := "user"
	if admin {
		roleName = "admin"
	}
	role, err := client.Role.Create().
		SetName(roleName).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.User.Create().
		SetUsername(strings.SplitN(email, "@", 2)[0]).
		SetEmail(email).
		SetPasswordHash("placeholder-hash").
		SetCreatedAt(now).
		SetUpdatedAt(now).
		AddRoleIDs(role.ID).
		Save(ctx)
	require.NoError(t, err)
}

func baseTestDeps(t *testing.T, stdin string) deps {
	t.Helper()
	return deps{
		getenv: func(k string) string {
			if k == "DB_HOST" {
				return "localhost"
			}
			return ""
		},
		isTerminal: func() bool { return true },
		stdin:      strings.NewReader(stdin),
		stdout:     &bytes.Buffer{},
		readPassword: func() ([]byte, error) {
			return nil, errors.New("readPassword not expected to be called in this test")
		},
		newLogger: func() (*zap.Logger, error) { return zap.NewNop(), nil },
		newDBClient: func(*zap.Logger) (*ent.Client, *sql.DB, error) {
			client, db, _ := newSeededTestDBClient(t)
			return client, db, nil
		},
		migrateSchema: func(context.Context, *ent.Client) error { return nil },
	}
}

func TestRun_RefusesWithoutTTY(t *testing.T) {
	d := baseTestDeps(t, "")
	d.isTerminal = func() bool { return false }

	err := run(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "interactive-only")
}

func TestRun_RefusesWhenSecretsManagerARNSet(t *testing.T) {
	d := baseTestDeps(t, "")
	d.getenv = func(k string) string {
		if k == "DB_SECRET_ARN" {
			return "arn:aws:secretsmanager:us-east-1:123:secret:foo"
		}
		return ""
	}

	err := run(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "DB_SECRET_ARN")
}

func TestRun_RefusesNonLocalDBHost(t *testing.T) {
	d := baseTestDeps(t, "")
	d.getenv = func(k string) string {
		if k == "DB_HOST" {
			return "prod-db.example.com"
		}
		return ""
	}

	err := run(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a local database host")
}

func TestRun_AcceptsEachAllowedLocalHost(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1", "db", "host.docker.internal", "LOCALHOST"} {
		t.Run(host, func(t *testing.T) {
			d := baseTestDeps(t, "n\n")
			d.getenv = func(k string) string {
				if k == "DB_HOST" {
					return host
				}
				return ""
			}

			err := run(d)
			require.NoError(t, err)
		})
	}
}

func TestRun_AbortsWhenStartupConfirmationDeclined(t *testing.T) {
	d := baseTestDeps(t, "n\n")

	err := run(d)
	require.NoError(t, err)
}

func TestRun_ConfirmationReadFailure(t *testing.T) {
	d := baseTestDeps(t, "") // EOF immediately, no newline

	err := run(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read confirmation")
}

func TestRun_LoggerInitFailure(t *testing.T) {
	d := baseTestDeps(t, "y\n")
	d.newLogger = func() (*zap.Logger, error) { return nil, errors.New("boom") }

	err := run(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to initialize logger")
}

func TestRun_DBClientFailure(t *testing.T) {
	d := baseTestDeps(t, "y\n")
	d.newDBClient = func(*zap.Logger) (*ent.Client, *sql.DB, error) {
		return nil, nil, errors.New("connection refused")
	}

	err := run(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to connect to database")
}

func TestRun_InvalidEmail(t *testing.T) {
	d := baseTestDeps(t, "y\nnot-an-email\n")

	err := run(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid email")
}

func TestRun_EmailReadFailure(t *testing.T) {
	d := baseTestDeps(t, "y\n") // confirms, then EOF before an email line

	err := run(d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read email")
}

func TestRun_NewUserHappyPath(t *testing.T) {
	stdin := "y\n" + // proceed against DB
		"new-admin@example.com\n" + // email
		""
	d := baseTestDeps(t, stdin)
	pwCalls := 0
	d.readPassword = func() ([]byte, error) {
		pwCalls++
		return []byte("supersecret1"), nil
	}

	err := run(d)
	require.NoError(t, err)
	require.Equal(t, 2, pwCalls, "password read once, then confirmation read once")

	out := d.stdout.(*bytes.Buffer).String()
	require.Contains(t, out, "does not exist; a new admin user will be created")
	require.Contains(t, out, "Done: new-admin@example.com")
}

func TestRun_ExistingNonAdminUser_PromotedOnConfirm(t *testing.T) {
	client, db, _ := newSeededTestDBClient(t)
	seedExistingUser(t, client, "existing@example.com", false)

	stdin := "y\n" + // proceed against DB
		"existing@example.com\n" + // email
		"y\n" + // promote confirmation
		""
	d := baseTestDeps(t, stdin)
	d.newDBClient = func(*zap.Logger) (*ent.Client, *sql.DB, error) { return client, db, nil }
	d.readPassword = func() ([]byte, error) { return []byte("supersecret1"), nil }

	err := run(d)
	require.NoError(t, err)

	out := d.stdout.(*bytes.Buffer).String()
	require.Contains(t, out, "exists without the admin role")
	require.Contains(t, out, "Done: existing@example.com")
}

func TestRun_ExistingNonAdminUser_DeclinesPromotion(t *testing.T) {
	client, db, _ := newSeededTestDBClient(t)
	seedExistingUser(t, client, "existing@example.com", false)

	stdin := "y\n" +
		"existing@example.com\n" +
		"n\n" +
		""
	d := baseTestDeps(t, stdin)
	d.newDBClient = func(*zap.Logger) (*ent.Client, *sql.DB, error) { return client, db, nil }

	err := run(d)
	require.NoError(t, err)

	out := d.stdout.(*bytes.Buffer).String()
	require.Contains(t, out, "Aborted.")
}

func TestRun_ExistingAdminUser_NoOp(t *testing.T) {
	client, db, _ := newSeededTestDBClient(t)
	seedExistingUser(t, client, "already-admin@example.com", true)

	stdin := "y\n" +
		"already-admin@example.com\n"
	d := baseTestDeps(t, stdin)
	d.newDBClient = func(*zap.Logger) (*ent.Client, *sql.DB, error) { return client, db, nil }

	err := run(d)
	require.NoError(t, err)

	out := d.stdout.(*bytes.Buffer).String()
	require.Contains(t, out, "already has the admin role; nothing to do")
}

func TestPromptLine(t *testing.T) {
	var stdout bytes.Buffer
	reader := bufio.NewReader(strings.NewReader("hello world  \n"))

	got, err := promptLine(&stdout, reader, "prompt: ")
	require.NoError(t, err)
	require.Equal(t, "hello world", got)
	require.Equal(t, "prompt: ", stdout.String())
}

func TestPromptLine_ReadError(t *testing.T) {
	var stdout bytes.Buffer
	reader := bufio.NewReader(strings.NewReader("no newline"))

	_, err := promptLine(&stdout, reader, "prompt: ")
	require.Error(t, err)
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"lowercase y", "y\n", true},
		{"lowercase yes", "yes\n", true},
		{"uppercase Y", "Y\n", true},
		{"uppercase YES", "YES\n", true},
		{"no", "n\n", false},
		{"empty", "\n", false},
		{"garbage", "sure\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got, err := confirm(&stdout, reader, "prompt: ")
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPromptPassword(t *testing.T) {
	var stdout bytes.Buffer
	calls := [][]byte{[]byte("goodpassword"), []byte("goodpassword")}
	i := 0
	readPassword := func() ([]byte, error) {
		v := calls[i]
		i++
		return v, nil
	}

	got, err := promptPassword(&stdout, readPassword)
	require.NoError(t, err)
	require.Equal(t, "goodpassword", got)
}

func TestPromptPassword_TooShortThenMismatchThenMatch(t *testing.T) {
	var stdout bytes.Buffer
	calls := [][]byte{
		[]byte("short"),        // too short, retry
		[]byte("goodpassword"), // long enough
		[]byte("mismatched1"),  // doesn't match -> retry
		[]byte("goodpassword"), // long enough
		[]byte("goodpassword"), // matches
	}
	i := 0
	readPassword := func() ([]byte, error) {
		v := calls[i]
		i++
		return v, nil
	}

	got, err := promptPassword(&stdout, readPassword)
	require.NoError(t, err)
	require.Equal(t, "goodpassword", got)
	require.Equal(t, 5, i)
	out := stdout.String()
	require.Contains(t, out, "at least")
	require.Contains(t, out, "do not match")
}

func TestPromptPassword_ReadError(t *testing.T) {
	var stdout bytes.Buffer
	readPassword := func() ([]byte, error) { return nil, errors.New("no tty") }

	_, err := promptPassword(&stdout, readPassword)
	require.Error(t, err)
}

func TestHasAdminRole(t *testing.T) {
	withAdmin := &ent.User{Edges: ent.UserEdges{Roles: []*ent.Role{{Name: "user"}, {Name: "admin"}}}}
	require.True(t, hasAdminRole(withAdmin))

	withoutAdmin := &ent.User{Edges: ent.UserEdges{Roles: []*ent.Role{{Name: "user"}}}}
	require.False(t, hasAdminRole(withoutAdmin))

	noRoles := &ent.User{}
	require.False(t, hasAdminRole(noRoles))
}
