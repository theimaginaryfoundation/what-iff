// Command create-superuser provisions a local admin user interactively.
//
// It is the ONLY admin-provisioning path (invoked via `make local-superuser`);
// the former SUPERADMIN_* boot provisioning was removed, so no environment
// variable can mint an admin in a running deployment. CI test users come from
// the public register endpoint. By design there is no -password flag and no
// credential env var: password entry is interactive only, and the tool
// refuses to run without a TTY or against a non-local database.
package main

import (
	"bufio"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"golang.org/x/term"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entuser "github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/database"
	"github.com/theimaginaryfoundation/what-iff/internal/logging"
)

const minPasswordLength = 8

// localDBHosts are the only database hosts this tool will talk to, even if the
// shell points at something else. "db" is the docker-compose service name;
// "host.docker.internal" is how a container reaches a host-side Postgres on
// Docker Desktop (macOS/Windows) — still local, just not loopback.
var localDBHosts = map[string]bool{
	"localhost":            true,
	"127.0.0.1":            true,
	"::1":                  true,
	"db":                   true,
	"host.docker.internal": true,
}

// deps bundles create-superuser's external dependencies behind seams so run
// can be driven end-to-end in tests without a TTY, environment, or a real
// database. defaultDeps wires them to the real world; main is a thin
// exit-code wrapper around run.
type deps struct {
	getenv        func(string) string
	isTerminal    func() bool
	stdin         io.Reader
	stdout        io.Writer
	readPassword  func() ([]byte, error)
	newLogger     func() (*zap.Logger, error)
	newDBClient   func(*zap.Logger) (*ent.Client, *sql.DB, error)
	migrateSchema func(context.Context, *ent.Client) error
}

func defaultDeps() deps {
	return deps{
		getenv:        os.Getenv,
		isTerminal:    func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		stdin:         os.Stdin,
		stdout:        os.Stdout,
		readPassword:  func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) },
		newLogger:     logging.NewLogger,
		newDBClient:   database.NewClient,
		migrateSchema: func(ctx context.Context, c *ent.Client) error { return c.Schema.Create(ctx) },
	}
}

func main() {
	var envFile string
	flag.StringVar(&envFile, "env", ".env", "env file path")
	flag.Parse()

	if err := godotenv.Load(envFile); err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}

	if err := run(defaultDeps()); err != nil {
		log.Fatal(err)
	}
}

// run holds all of create-superuser's actual logic. It returns an error
// instead of calling log.Fatal/os.Exit directly so tests can drive every
// guard clause and branch without terminating the test process; main is the
// only place that turns a non-nil error into a process exit.
func run(d deps) error {
	if !d.isTerminal() {
		return fmt.Errorf("create-superuser is interactive-only and requires a TTY; there is intentionally no -password flag or credential env var")
	}

	// Local-DB safeguards: refuse non-local targets even when the shell is
	// misconfigured to point elsewhere.
	if arn := strings.TrimSpace(d.getenv("DB_SECRET_ARN")); arn != "" {
		return fmt.Errorf("DB_SECRET_ARN is set (Secrets-Manager-managed database); refusing to run against a non-local database")
	}
	dbHost := strings.ToLower(strings.TrimSpace(d.getenv("DB_HOST")))
	if !localDBHosts[dbHost] {
		return fmt.Errorf("DB_HOST=%q is not a local database host (allowed: localhost, 127.0.0.1, ::1, db, host.docker.internal); refusing", dbHost)
	}

	// The host allowlist checks a label, not a destination: a tunnel or proxy
	// on localhost can still terminate remotely. Surface the exact target and
	// require explicit confirmation so an operator with a forwarded port sees
	// what they are about to modify.
	dbPort := strings.TrimSpace(d.getenv("DB_PORT"))
	dbName := strings.TrimSpace(d.getenv("DB_NAME"))
	fmt.Fprintf(d.stdout, "Target database: %s:%s/%s\n", dbHost, dbPort, dbName)
	fmt.Fprintln(d.stdout, "⚠️  Ensure this is a genuinely local database — port forwards/tunnels to remote databases also appear as localhost.")

	// One shared reader for the whole run: two independent bufio.Readers over
	// the same underlying stdin can each buffer ahead past what they consume,
	// silently dropping input the other reader was meant to see.
	reader := bufio.NewReader(d.stdin)

	proceed, err := confirm(d.stdout, reader, "Proceed against this database? [y/N]: ")
	if err != nil {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}
	if !proceed {
		fmt.Fprintln(d.stdout, "Aborted.")
		return nil
	}

	logger, err := d.newLogger()
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Sync()

	client, sqlDB, err := d.newDBClient(logger)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer client.Close()
	defer sqlDB.Close()

	ctx := context.Background()

	// A fresh `make db-up` has no tables yet: run schema migration and seed
	// reference data (roles, models) before provisioning the admin.
	if err := d.migrateSchema(ctx, client); err != nil {
		return fmt.Errorf("failed to run database migrations: %w", err)
	}
	if err := database.EnsureSeedData(ctx, client, logger); err != nil {
		return fmt.Errorf("failed to seed database: %w", err)
	}

	email, err := promptLine(d.stdout, reader, "Admin email: ")
	if err != nil {
		return fmt.Errorf("failed to read email: %w", err)
	}
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("invalid email %q", email)
	}

	// Explicit create / promote / password-reset semantics: inspect first and
	// require confirmation before touching an existing account.
	existing, err := client.User.Query().
		Where(entuser.EmailEqualFold(email)).
		WithRoles().
		Only(ctx)
	switch {
	case err == nil:
		if hasAdminRole(existing) {
			fmt.Fprintf(d.stdout, "User %s already has the admin role; nothing to do.\n", email)
			return nil
		}
		fmt.Fprintf(d.stdout, "User %s exists without the admin role.\n", email)
		promote, err := confirm(d.stdout, reader, "Promote to admin and reset their password? [y/N]: ")
		if err != nil {
			return fmt.Errorf("failed to read confirmation: %w", err)
		}
		if !promote {
			fmt.Fprintln(d.stdout, "Aborted.")
			return nil
		}
	case ent.IsNotFound(err):
		fmt.Fprintf(d.stdout, "User %s does not exist; a new admin user will be created.\n", email)
	default:
		return fmt.Errorf("failed to look up user: %w", err)
	}

	password, err := promptPassword(d.stdout, d.readPassword)
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}

	result, err := database.CreateOrPromoteAdmin(ctx, client, logger, email, password)
	if err != nil {
		return fmt.Errorf("failed to provision admin: %w", err)
	}
	fmt.Fprintf(d.stdout, "Done: %s (%s)\n", email, result)
	return nil
}

func promptLine(stdout io.Writer, reader *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(stdout, prompt)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func promptPassword(stdout io.Writer, readPassword func() ([]byte, error)) (string, error) {
	for {
		fmt.Fprint(stdout, "Password: ")
		first, err := readPassword()
		fmt.Fprintln(stdout)
		if err != nil {
			return "", err
		}
		if len(first) < minPasswordLength {
			fmt.Fprintf(stdout, "Password must be at least %d characters.\n", minPasswordLength)
			continue
		}
		fmt.Fprint(stdout, "Confirm password: ")
		second, err := readPassword()
		fmt.Fprintln(stdout)
		if err != nil {
			return "", err
		}
		if string(first) != string(second) {
			fmt.Fprintln(stdout, "Passwords do not match; try again.")
			continue
		}
		return string(first), nil
	}
}

func confirm(stdout io.Writer, reader *bufio.Reader, prompt string) (bool, error) {
	line, err := promptLine(stdout, reader, prompt)
	if err != nil {
		return false, err
	}
	answer := strings.ToLower(line)
	return answer == "y" || answer == "yes", nil
}

func hasAdminRole(u *ent.User) bool {
	for _, r := range u.Edges.Roles {
		if r.Name == "admin" {
			return true
		}
	}
	return false
}
