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
	"flag"
	"fmt"
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

func main() {
	var envFile string
	flag.StringVar(&envFile, "env", ".env", "env file path")
	flag.Parse()

	if err := godotenv.Load(envFile); err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		log.Fatal("create-superuser is interactive-only and requires a TTY; there is intentionally no -password flag or credential env var")
	}

	// Local-DB safeguards: refuse non-local targets even when the shell is
	// misconfigured to point elsewhere.
	if arn := strings.TrimSpace(os.Getenv("DB_SECRET_ARN")); arn != "" {
		log.Fatal("DB_SECRET_ARN is set (Secrets-Manager-managed database); refusing to run against a non-local database")
	}
	dbHost := strings.ToLower(strings.TrimSpace(os.Getenv("DB_HOST")))
	if !localDBHosts[dbHost] {
		log.Fatalf("DB_HOST=%q is not a local database host (allowed: localhost, 127.0.0.1, ::1, db, host.docker.internal); refusing", dbHost)
	}
	// The host allowlist checks a label, not a destination: a tunnel or proxy
	// on localhost can still terminate remotely. Surface the exact target and
	// require explicit confirmation so an operator with a forwarded port sees
	// what they are about to modify.
	dbPort := strings.TrimSpace(os.Getenv("DB_PORT"))
	dbName := strings.TrimSpace(os.Getenv("DB_NAME"))
	fmt.Printf("Target database: %s:%s/%s\n", dbHost, dbPort, dbName)
	fmt.Println("⚠️  Ensure this is a genuinely local database — port forwards/tunnels to remote databases also appear as localhost.")
	startupReader := bufio.NewReader(os.Stdin)
	proceed, err := confirm(startupReader, "Proceed against this database? [y/N]: ")
	if err != nil {
		log.Fatalf("failed to read confirmation: %v", err)
	}
	if !proceed {
		fmt.Println("Aborted.")
		return
	}

	logger, err := logging.NewLogger()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	client, sqlDB, err := database.NewClient(logger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}
	defer client.Close()
	defer sqlDB.Close()

	ctx := context.Background()

	// A fresh `make db-up` has no tables yet: run schema migration and seed
	// reference data (roles, models) before provisioning the admin.
	if err := client.Schema.Create(ctx); err != nil {
		logger.Fatal("Failed to run database migrations", zap.Error(err))
	}
	if err := database.EnsureSeedData(ctx, client, logger); err != nil {
		logger.Fatal("Failed to seed database", zap.Error(err))
	}

	reader := bufio.NewReader(os.Stdin)
	email, err := promptLine(reader, "Admin email: ")
	if err != nil {
		log.Fatalf("failed to read email: %v", err)
	}
	if email == "" || !strings.Contains(email, "@") {
		log.Fatalf("invalid email %q", email)
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
			fmt.Printf("User %s already has the admin role; nothing to do.\n", email)
			return
		}
		fmt.Printf("User %s exists without the admin role.\n", email)
		promote, err := confirm(reader, "Promote to admin and reset their password? [y/N]: ")
		if err != nil {
			log.Fatalf("failed to read confirmation: %v", err)
		}
		if !promote {
			fmt.Println("Aborted.")
			return
		}
	case ent.IsNotFound(err):
		fmt.Printf("User %s does not exist; a new admin user will be created.\n", email)
	default:
		logger.Fatal("Failed to look up user", zap.Error(err))
	}

	password, err := promptPassword()
	if err != nil {
		log.Fatalf("failed to read password: %v", err)
	}

	result, err := database.CreateOrPromoteAdmin(ctx, client, logger, email, password)
	if err != nil {
		logger.Fatal("Failed to provision admin", zap.Error(err))
	}
	fmt.Printf("Done: %s (%s)\n", email, result)
}

func promptLine(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func promptPassword() (string, error) {
	for {
		fmt.Print("Password: ")
		first, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", err
		}
		if len(first) < minPasswordLength {
			fmt.Printf("Password must be at least %d characters.\n", minPasswordLength)
			continue
		}
		fmt.Print("Confirm password: ")
		second, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", err
		}
		if string(first) != string(second) {
			fmt.Println("Passwords do not match; try again.")
			continue
		}
		return string(first), nil
	}
}

func confirm(reader *bufio.Reader, prompt string) (bool, error) {
	line, err := promptLine(reader, prompt)
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
