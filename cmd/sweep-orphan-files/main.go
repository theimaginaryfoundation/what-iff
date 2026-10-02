// Command sweep-orphan-files deletes stored attachment objects that no file attachment row
// references any more (issue #252): objects under users/{id}/ left behind by deletes that predate
// object cleanup, or by a cleanup that failed. See storage.SweepUserOrphans for what counts as
// referenced. Only known attachment layouts (images/, images/thumbs/, chats/, personalities/ and
// top-level {id}[_name] keys) are candidates; anything else under users/{id}/ is left alone.
//
// Safe by default: it prints what it would delete unless -yes is passed, keeps anything modified
// within -grace (so in-flight uploads are never swept), and refuses to run against a
// production-looking ENV unless -force is also given. Running it twice is harmless.
//
// Usage:
//
//	go run ./cmd/sweep-orphan-files -email you@example.com          # dry run for one account
//	go run ./cmd/sweep-orphan-files -user-id <uuid> -yes            # any id, including a deleted account's
//	go run ./cmd/sweep-orphan-files -all -grace 72h -yes            # every existing account
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entuser "github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/database"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/logging"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

func main() {
	var (
		email               string
		userIDFlag          string
		all                 bool
		grace               time.Duration
		envFile             string
		confirm             bool
		force               bool
		insecureLocalSecret bool
	)
	flag.StringVar(&email, "email", "", "sweep the account with this email")
	flag.StringVar(&userIDFlag, "user-id", "", "sweep this user id (need not exist any more)")
	flag.BoolVar(&all, "all", false, "sweep every existing account")
	flag.DurationVar(&grace, "grace", 24*time.Hour, "keep objects modified more recently than this")
	flag.StringVar(&envFile, "env", ".env", "env file path")
	flag.BoolVar(&confirm, "yes", false, "actually delete (omit for a dry run)")
	flag.BoolVar(&force, "force", false, "allow running against a production-looking ENV")
	flag.BoolVar(&insecureLocalSecret, "allow-insecure-local-secret", false, "allow a dummy token secret only for local development")
	flag.Parse()

	selectors := 0
	for _, set := range []bool{strings.TrimSpace(email) != "", strings.TrimSpace(userIDFlag) != "", all} {
		if set {
			selectors++
		}
	}
	if selectors != 1 {
		log.Fatal("pass exactly one of -email, -user-id or -all")
	}
	if grace < time.Hour {
		log.Fatal("-grace must be at least 1h so in-flight uploads are never swept")
	}
	if err := godotenv.Load(envFile); err != nil {
		log.Printf("warning: could not load %s: %v", envFile, err)
	}

	env := strings.ToLower(strings.TrimSpace(os.Getenv("ENV")))
	if env == "" {
		env = strings.ToLower(strings.TrimSpace(os.Getenv("ENVIRONMENT")))
	}
	if (env == "prod" || env == "production") && !force {
		log.Fatalf("refusing to run against ENV=%q without -force", env)
	}

	logger, err := logging.NewLogger()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer logger.Sync()

	client, sqlDB, err := database.NewClient(logger)
	if err != nil {
		logger.Fatal("connect db", zap.Error(err))
	}
	defer client.Close()
	defer sqlDB.Close()

	// The token secret is required to construct the datastore but this tool never touches
	// encrypted tokens. A dummy is allowed only with an explicit local-development escape hatch.
	secret := os.Getenv("TOKEN_ENCRYPTION_SECRET")
	if len(secret) < datastore.MinTokenEncryptionSecretLen {
		if !insecureLocalSecret || (env != "development" && env != "local" && env != "test") {
			logger.Fatal("TOKEN_ENCRYPTION_SECRET is required; use -allow-insecure-local-secret only in local development")
		}
		secret = strings.Repeat("0", datastore.MinTokenEncryptionSecretLen)
	}
	ds, err := datastore.NewDatastore(client, sqlDB, logger, secret, nil)
	if err != nil {
		logger.Fatal("init datastore", zap.Error(err))
	}

	ctx := context.Background()
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-2" // same default as the API server
	}
	fileStore, err := storage.NewFileStore(ctx, os.Getenv("S3_FILE_BUCKET"), region, logger)
	if err != nil {
		logger.Fatal("init file store", zap.Error(err))
	}

	var userIDs []uuid.UUID
	switch {
	case all:
		userIDs, err = client.User.Query().IDs(ctx)
		if err != nil {
			logger.Fatal("list users", zap.Error(err))
		}
	case strings.TrimSpace(userIDFlag) != "":
		id, err := uuid.Parse(strings.TrimSpace(userIDFlag))
		if err != nil {
			log.Fatalf("invalid -user-id: %v", err)
		}
		userIDs = []uuid.UUID{id}
	default:
		u, err := client.User.Query().Where(entuser.EmailEQ(email)).Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				log.Fatalf("no user with email %q", email)
			}
			logger.Fatal("lookup user", zap.Error(err))
		}
		userIDs = []uuid.UUID{u.ID}
	}

	opts := storage.SweepOptions{DryRun: !confirm, GracePeriod: grace}
	var orphans, deleted, failed int
	for _, id := range userIDs {
		res, err := storage.SweepUserOrphans(ctx, logger, fileStore, ds, id, opts)
		if err != nil {
			logger.Error("sweep failed", zap.String("user_id", id.String()), zap.Error(err))
			failed++
			continue
		}
		fmt.Printf("%s: scanned %d, referenced %d, skipped (unknown prefix) %d, within grace %d, orphans %d, deleted %d, failed %d\n",
			id, res.Scanned, res.Referenced, res.SkippedUnknown, res.TooRecent, len(res.Orphans), res.Deleted, len(res.Failed))
		for _, o := range res.Orphans {
			fmt.Printf("  %s  %d bytes  %s\n", o.LastModified.UTC().Format(time.RFC3339), o.Size, o.Key)
		}
		orphans += len(res.Orphans)
		deleted += res.Deleted
		failed += len(res.Failed)
	}

	if !confirm {
		fmt.Printf("\nDry run: %d orphaned object(s) across %d account(s). Re-run with -yes to delete.\n", orphans, len(userIDs))
		return
	}
	fmt.Printf("\nDeleted %d of %d orphaned object(s) across %d account(s); %d failure(s).\n", deleted, orphans, len(userIDs), failed)
	if failed > 0 {
		os.Exit(1)
	}
}
