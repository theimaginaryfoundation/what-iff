package datastore

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/internal/exporter"
)

// newAccountExportTestDatastore is the shared SQLite fixture plus the chat_messages table and chat
// columns the conversation export queries select.
func newAccountExportTestDatastore(t *testing.T) *Datastore {
	t.Helper()
	ds, cleanup := newSQLiteDatastore(t)
	t.Cleanup(cleanup)

	// The shared SQLite fixture intentionally omits messages, so add the
	// production columns required by Ent's export query.
	_, err := ds.sqlDB.Exec(`
		ALTER TABLE chats ADD COLUMN source text;
		ALTER TABLE chats ADD COLUMN import_hash text;
		ALTER TABLE chats ADD COLUMN rehydration_state text;
		CREATE TABLE chat_messages (
			id uuid PRIMARY KEY,
			message text NOT NULL,
			origin text NOT NULL,
			read_status text NOT NULL,
			bookmarked boolean NOT NULL DEFAULT 0,
			response_id text,
			sent_at datetime NOT NULL,
			tokens integer,
			generation_model text,
			generation_personality text,
			generation_expression_reasoning text,
			model_reasoning text,
			last_error_message text,
			last_error_code text,
			checkpoint_completed_at datetime,
			context_breakdown json,
			chat_messages uuid NOT NULL,
			chat_message_generation_mood uuid,
			chat_message_generation_expression uuid
		)`)
	require.NoError(t, err)
	return ds
}

func TestExportConversationInputsUsesSentAtIDCursor(t *testing.T) {
	ds := newAccountExportTestDatastore(t)
	ctx := context.Background()

	owner, err := ds.dbClient.User.Create().
		SetUsername("export-owner").
		SetEmail("export-owner@example.com").
		SetPasswordHash("password-hash").
		Save(ctx)
	require.NoError(t, err)
	chat, err := ds.dbClient.Chat.Create().
		SetName("Export chat").
		SetOwnerID(owner.ID).
		Save(ctx)
	require.NoError(t, err)

	sentAt := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	for i := range accountExportBatchSize + 1 {
		_, err := ds.dbClient.ChatMessage.Create().
			SetChatID(chat.ID).
			SetMessage(fmt.Sprintf("message-%03d", i)).
			SetOrigin(chatmessage.OriginUser).
			SetSentAt(sentAt).
			Save(ctx)
		require.NoError(t, err)
	}

	conversations, err := ds.ExportConversationInputs(ctx, owner.ID)
	require.NoError(t, err)
	require.Len(t, conversations, 1)
	require.Len(t, conversations[0].Messages, accountExportBatchSize+1)

	seen := make(map[string]bool, len(conversations[0].Messages))
	for _, message := range conversations[0].Messages {
		seen[message.Text] = true
	}
	for i := range accountExportBatchSize + 1 {
		require.Truef(t, seen[fmt.Sprintf("message-%03d", i)], "message %d was skipped", i)
	}
}

// Chats are paged across more than one batch, every chat is visited once in (created_at, id)
// order (the order conversations.json is written in), and an error from the callback stops the walk.
func TestForEachExportConversationPagesChatsInExportOrder(t *testing.T) {
	ds := newAccountExportTestDatastore(t)
	ctx := context.Background()

	owner, err := ds.dbClient.User.Create().
		SetUsername("export-owner").
		SetEmail("export-owner@example.com").
		SetPasswordHash("password-hash").
		Save(ctx)
	require.NoError(t, err)

	// A few earlier chats, then a run sharing one created_at that the page boundary falls inside,
	// so only the ID tie-breaker carries the cursor to the next page.
	base := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	type key struct {
		createdAt time.Time
		id        string
	}
	var want []key
	total := accountExportBatchSize + 7
	for i := range total {
		createdAt := base
		if i%7 == 0 {
			createdAt = base.Add(-time.Duration(i+1) * time.Minute)
		}
		c, err := ds.dbClient.Chat.Create().
			SetName(fmt.Sprintf("chat-%03d", i)).
			SetOwnerID(owner.ID).
			SetCreatedAt(createdAt).
			Save(ctx)
		require.NoError(t, err)
		_, err = ds.dbClient.ChatMessage.Create().
			SetChatID(c.ID).
			SetMessage(fmt.Sprintf("message in %03d", i)).
			SetOrigin(chatmessage.OriginUser).
			SetSentAt(createdAt).
			Save(ctx)
		require.NoError(t, err)
		want = append(want, key{createdAt, c.ID.String()})
	}
	sort.Slice(want, func(i, j int) bool {
		if !want[i].createdAt.Equal(want[j].createdAt) {
			return want[i].createdAt.Before(want[j].createdAt)
		}
		return want[i].id < want[j].id
	})

	var got []key
	require.NoError(t, ds.ForEachExportConversation(ctx, owner.ID, func(c exporter.ConversationInput) error {
		require.Len(t, c.Messages, 1)
		got = append(got, key{c.CreatedAt.UTC(), c.ID.String()})
		return nil
	}))
	require.Equal(t, want, got)

	stop := errors.New("stop")
	visited := 0
	err = ds.ForEachExportConversation(ctx, owner.ID, func(exporter.ConversationInput) error {
		visited++
		return stop
	})
	require.ErrorIs(t, err, stop)
	require.Equal(t, 1, visited)
}

// The account export nests the memory ZIP by passing an outer archive entry straight to
// ExportMemories. The nested archive is byte-identical to a standalone export and imports back.
func TestExportMemoriesNestedInOuterZipRoundTrips(t *testing.T) {
	ctx := context.Background()
	src, cleanupSrc := newMemoryTestDatastore(t)
	defer cleanupSrc()
	dst, cleanupDst := newMemoryTestDatastore(t)
	defer cleanupDst()
	userID, dstUserID := uuid.New(), uuid.New()
	createTestUser(t, src, userID)
	createTestUser(t, dst, dstUserID)
	for i := range 3 {
		createGlobalMemory(t, src, userID, fmt.Sprintf("memory %d", i))
	}

	var standalone bytes.Buffer
	require.NoError(t, src.ExportMemories(ctx, userID, &standalone))

	var outer bytes.Buffer
	zw := zip.NewWriter(&outer)
	entry, err := zw.Create("memories.zip")
	require.NoError(t, err)
	require.NoError(t, src.ExportMemories(ctx, userID, entry))
	require.NoError(t, zw.Close())

	zr, err := zip.NewReader(bytes.NewReader(outer.Bytes()), int64(outer.Len()))
	require.NoError(t, err)
	require.Len(t, zr.File, 1)
	rc, err := zr.File[0].Open()
	require.NoError(t, err)
	nested, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	require.Equal(t, standalone.Bytes(), nested)

	nr, err := zip.NewReader(bytes.NewReader(nested), int64(len(nested)))
	require.NoError(t, err)
	res, err := dst.ImportMemories(ctx, dstUserID, nr, func(context.Context, string) ([]float32, error) { return []float32{1}, nil })
	require.NoError(t, err)
	require.Equal(t, 3, res.ImportedCount)
}
