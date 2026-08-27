// Command clear-account wipes a single user's CONTENT (chats, messages, memories, personalities,
// moods, agent jobs, rituals, jobs, and the memory/compaction audit tables) while keeping the user
// row, preferences, billing, and roles intact. It exists for local export/import test loops.
//
// Safe by default: it prints a dry-run summary unless -yes is passed, and refuses to run against a
// production-looking ENV unless -force is also given.
//
// Usage:
//
//	go run ./cmd/clear-account -email you@example.com          # dry run (counts only)
//	go run ./cmd/clear-account -email you@example.com -yes      # actually delete
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entagentjob "github.com/theimaginaryfoundation/what-iff/ent/agentjob"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	entsnap "github.com/theimaginaryfoundation/what-iff/ent/checkpointsnapshot"
	entcompaction "github.com/theimaginaryfoundation/what-iff/ent/compactionevent"
	entjob "github.com/theimaginaryfoundation/what-iff/ent/job"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	entmerge "github.com/theimaginaryfoundation/what-iff/ent/memorymergeevent"
	entmood "github.com/theimaginaryfoundation/what-iff/ent/mood"
	entpersonality "github.com/theimaginaryfoundation/what-iff/ent/personality"
	entritual "github.com/theimaginaryfoundation/what-iff/ent/ritual"
	entuser "github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/database"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/logging"
)

func main() {
	var (
		email               string
		envFile             string
		confirm             bool
		force               bool
		insecureLocalSecret bool
	)
	flag.StringVar(&email, "email", "", "email of the account to clear (required)")
	flag.StringVar(&envFile, "env", ".env", "env file path")
	flag.BoolVar(&confirm, "yes", false, "actually delete (omit for a dry-run summary)")
	flag.BoolVar(&force, "force", false, "allow running against a production-looking ENV")
	flag.BoolVar(&insecureLocalSecret, "allow-insecure-local-secret", false, "allow a dummy token secret only for local development")
	flag.Parse()

	if strings.TrimSpace(email) == "" {
		log.Fatal("-email is required")
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

	// token secret is required to construct the datastore but this tool never touches encrypted
	// tokens. A dummy is allowed only with an explicit local-development escape hatch.
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

	u, err := client.User.Query().Where(entuser.EmailEQ(email)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			log.Fatalf("no user with email %q", email)
		}
		logger.Fatal("lookup user", zap.Error(err))
	}
	uid := u.ID

	// Enumerate ids per section (child → parent order for the datastore deletes).
	agentJobIDs := mustIDs(ctx, client.AgentJob.Query().Where(entagentjob.HasOwnerWith(entuser.ID(uid))).IDs)
	ritualIDs := mustIDs(ctx, client.Ritual.Query().Where(entritual.HasOwnerWith(entuser.ID(uid))).IDs)
	memoryIDs := mustIDs(ctx, client.Memory.Query().Where(entmemory.HasOwnerWith(entuser.ID(uid))).IDs)
	chatIDs := mustIDs(ctx, client.Chat.Query().Where(entchat.HasOwnerWith(entuser.ID(uid))).IDs)
	personalityIDs := mustIDs(ctx, client.Personality.Query().Where(entpersonality.HasUserWith(entuser.ID(uid))).IDs)
	moodIDs := mustIDs(ctx, client.Mood.Query().Where(entmood.HasOwnerWith(entuser.ID(uid))).IDs)

	fmt.Printf("Account: %s (%s)\n", u.Email, uid)
	fmt.Printf("  agent jobs:    %d\n", len(agentJobIDs))
	fmt.Printf("  rituals:       %d\n", len(ritualIDs))
	fmt.Printf("  memories:      %d\n", len(memoryIDs))
	fmt.Printf("  chats:         %d\n", len(chatIDs))
	fmt.Printf("  personalities: %d\n", len(personalityIDs))
	fmt.Printf("  moods:         %d\n", len(moodIDs))
	fmt.Println("  + jobs and memory/compaction audit rows (bulk)")

	if !confirm {
		fmt.Println("\nDry run — re-run with -yes to delete.")
		return
	}

	// Delete via datastore methods so M2M links (message↔ritual, personality↔mood) are handled the
	// same way the app's own delete endpoints handle them.
	deleteEach("agent jobs", agentJobIDs, func(id uuid.UUID) error { return ds.DeleteAgentJob(ctx, uid, id) }, logger)
	deleteEach("rituals", ritualIDs, func(id uuid.UUID) error { return ds.DeleteRitual(ctx, uid, id) }, logger)
	deleteEach("memories", memoryIDs, func(id uuid.UUID) error { return ds.DeleteMemory(ctx, uid, id) }, logger)
	deleteEach("chats", chatIDs, func(id uuid.UUID) error { return ds.DeleteChat(ctx, uid, id) }, logger)
	deleteEach("personalities", personalityIDs, func(id uuid.UUID) error { return ds.DeletePersonality(ctx, uid, id) }, logger)
	deleteEach("moods", moodIDs, func(id uuid.UUID) error { return ds.DeleteMood(ctx, uid, id) }, logger)

	// Bulk-delete rows with no child references. Audit tables: merge events reference compaction
	// events (SetNull), compaction events reference checkpoint snapshots (FK) — delete in that order.
	bulkDelete(ctx, "memory merge events", client.MemoryMergeEvent.Delete().Where(entmerge.UserID(uid)).Exec, logger)
	bulkDelete(ctx, "compaction events", client.CompactionEvent.Delete().Where(entcompaction.UserID(uid)).Exec, logger)
	bulkDelete(ctx, "checkpoint snapshots", client.CheckpointSnapshot.Delete().Where(entsnap.UserID(uid)).Exec, logger)
	bulkDelete(ctx, "jobs", client.Job.Delete().Where(entjob.HasOwnerWith(entuser.ID(uid))).Exec, logger)

	fmt.Printf("\nCleared content for %s (user, preferences, billing, and roles kept).\n", u.Email)
}

func mustIDs(ctx context.Context, fn func(context.Context) ([]uuid.UUID, error)) []uuid.UUID {
	ids, err := fn(ctx)
	if err != nil {
		log.Fatalf("enumerate ids: %v", err)
	}
	return ids
}

func deleteEach(label string, ids []uuid.UUID, del func(uuid.UUID) error, logger *zap.Logger) {
	deleted := 0
	for _, id := range ids {
		if err := del(id); err != nil {
			logger.Warn("delete failed", zap.String("section", label), zap.String("id", id.String()), zap.Error(err))
			continue
		}
		deleted++
	}
	fmt.Printf("  deleted %d/%d %s\n", deleted, len(ids), label)
}

func bulkDelete(ctx context.Context, label string, exec func(context.Context) (int, error), logger *zap.Logger) {
	n, err := exec(ctx)
	if err != nil {
		logger.Warn("bulk delete failed", zap.String("section", label), zap.Error(err))
		return
	}
	fmt.Printf("  deleted %d %s\n", n, label)
}
