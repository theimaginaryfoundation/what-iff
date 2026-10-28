package datastore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	entschema "entgo.io/ent/dialect/sql/schema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/migrate"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createGeneratedTestSchema creates every generated table except the vector-typed ones SQLite
// cannot express, so these tests run on the real column set instead of hand-written DDL.
func createGeneratedTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	var tables []*entschema.Table
	for _, tb := range migrate.Tables {
		if tb.Name == "embeddings" || tb.Name == "file_chunks" {
			continue
		}
		tables = append(tables, tb)
	}
	m, err := entschema.NewMigrate(entsql.OpenDB(dialect.SQLite, db))
	require.NoError(t, err)
	require.NoError(t, m.Create(context.Background(), tables...))
}

type remoteSyncFixture struct {
	ds    *Datastore
	owner uuid.UUID
	other uuid.UUID
	t0    time.Time
}

func newRemoteSyncFixture(t *testing.T) *remoteSyncFixture {
	t.Helper()
	ds, cleanup := newTestDatastore(t, createGeneratedTestSchema)
	t.Cleanup(cleanup)
	f := &remoteSyncFixture{ds: ds, owner: uuid.New(), other: uuid.New(), t0: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	createTestUser(t, ds, f.owner)
	createTestUser(t, ds, f.other)
	token := "wht_secret-token-abcd"
	require.NoError(t, ds.UpsertRemoteSync(context.Background(), f.owner, models.RemoteSyncUpsert{BaseURL: "https://whatiff.chat", Token: &token}))
	return f
}

// msgs builds remote messages alternating user/assistant, a minute apart from f.t0+offset minutes.
func (f *remoteSyncFixture) msgs(offset int, texts ...string) []models.RemoteSyncMessage {
	out := make([]models.RemoteSyncMessage, len(texts))
	for i, text := range texts {
		origin := models.MessageOriginUser
		if (offset+i)%2 == 1 {
			origin = models.MessageOriginAssistant
		}
		out[i] = models.RemoteSyncMessage{
			RemoteID: uuid.New(), Message: text, Origin: origin,
			SentAt: f.t0.Add(time.Duration(offset+i) * time.Minute), GenerationModel: "remote-model",
		}
	}
	return out
}

func (f *remoteSyncFixture) thread(id uuid.UUID, name string) models.RemoteSyncThread {
	last := f.t0.Add(time.Hour)
	return models.RemoteSyncThread{RemoteChatID: id, Name: name, CreatedAt: f.t0, LastMessageTime: &last, Host: "whatiff.chat"}
}

func (f *remoteSyncFixture) apply(t *testing.T, th models.RemoteSyncThread, batch []models.RemoteSyncMessage) *models.RemoteSyncApplyResult {
	t.Helper()
	res, err := f.ds.ApplyRemoteSyncThread(context.Background(), f.owner, th, batch)
	require.NoError(t, err)
	return res
}

func (f *remoteSyncFixture) texts(t *testing.T, chatID uuid.UUID) []string {
	t.Helper()
	rows, err := f.ds.dbClient.ChatMessage.Query().
		Where(chatmessage.HasChatWith(entchat.ID(chatID))).
		Order(ent.Asc(chatmessage.FieldSentAt), ent.Asc(chatmessage.FieldID)).
		All(context.Background())
	require.NoError(t, err)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Message
	}
	return out
}

func (f *remoteSyncFixture) writeLocal(t *testing.T, chatID uuid.UUID, text string, at time.Time) {
	t.Helper()
	_, err := f.ds.dbClient.ChatMessage.Create().
		SetChatID(chatID).SetMessage(text).SetOrigin(chatmessage.OriginUser).SetSentAt(at).
		Save(context.Background())
	require.NoError(t, err)
}

func TestRemoteSyncSettingsKeepTheTokenSecretAndOwnerScoped(t *testing.T) {
	f := newRemoteSyncFixture(t)
	ctx := context.Background()

	raw, err := f.ds.dbClient.RemoteSync.Query().Only(ctx)
	require.NoError(t, err)
	assert.NotContains(t, raw.Token, "secret", "stored encrypted")

	s, err := f.ds.GetRemoteSyncSettings(ctx, f.owner)
	require.NoError(t, err)
	assert.True(t, s.Configured)
	assert.True(t, s.Enabled)
	assert.Equal(t, "abcd", s.TokenHint)
	assert.Equal(t, models.RemoteSyncStatusPending, s.Status)

	acct, err := f.ds.GetRemoteSyncAccount(ctx, f.owner)
	require.NoError(t, err)
	assert.Equal(t, "wht_secret-token-abcd", acct.Token)

	other, err := f.ds.GetRemoteSyncSettings(ctx, f.other)
	require.NoError(t, err)
	assert.False(t, other.Configured)
	_, err = f.ds.GetRemoteSyncAccount(ctx, f.other)
	assert.ErrorIs(t, err, ErrRemoteSyncNotFound)
	assert.ErrorIs(t, f.ds.DeleteRemoteSync(ctx, f.other), ErrRemoteSyncNotFound)
}

func TestRemoteSyncUpsertNeedsATokenFirstAndKeepsItAfter(t *testing.T) {
	f := newRemoteSyncFixture(t)
	ctx := context.Background()

	err := f.ds.UpsertRemoteSync(ctx, f.other, models.RemoteSyncUpsert{BaseURL: "https://whatiff.chat"})
	assert.ErrorIs(t, err, ErrRemoteSyncTokenRequired)

	// A later save without a token keeps the stored one and clears needs_attention.
	msg := "token was revoked"
	require.NoError(t, f.ds.FinishRemoteSyncRun(ctx, f.owner, models.RemoteSyncRunResult{Status: models.RemoteSyncStatusNeedsAttention, Error: &msg}))
	off := false
	require.NoError(t, f.ds.UpsertRemoteSync(ctx, f.owner, models.RemoteSyncUpsert{BaseURL: "https://example.test", Enabled: &off}))
	s, err := f.ds.GetRemoteSyncSettings(ctx, f.owner)
	require.NoError(t, err)
	assert.Equal(t, "https://example.test", s.BaseURL)
	assert.False(t, s.Enabled)
	assert.Equal(t, models.RemoteSyncStatusPending, s.Status)
	assert.Nil(t, s.LastError)
	acct, err := f.ds.GetRemoteSyncAccount(ctx, f.owner)
	require.NoError(t, err)
	assert.Equal(t, "wht_secret-token-abcd", acct.Token)
}

func TestRemoteSyncLeaseAndDueList(t *testing.T) {
	f := newRemoteSyncFixture(t)
	ctx := context.Background()

	due, err := f.ds.ListDueRemoteSyncs(ctx, time.Now())
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{f.owner}, due, "never run is due")

	got, err := f.ds.ClaimRemoteSync(ctx, f.owner, time.Now().Add(time.Minute))
	require.NoError(t, err)
	assert.True(t, got)
	got, err = f.ds.ClaimRemoteSync(ctx, f.owner, time.Now().Add(time.Minute))
	require.NoError(t, err)
	assert.False(t, got, "a held lease is not claimed twice")
	s, err := f.ds.GetRemoteSyncSettings(ctx, f.owner)
	require.NoError(t, err)
	assert.True(t, s.Running)
	due, err = f.ds.ListDueRemoteSyncs(ctx, time.Now())
	require.NoError(t, err)
	assert.Empty(t, due, "a running account is not due")

	require.NoError(t, f.ds.FinishRemoteSyncRun(ctx, f.owner, models.RemoteSyncRunResult{
		Status: models.RemoteSyncStatusOK, Stats: models.RemoteSyncRunStats{ThreadsCreated: 2, MessagesAdded: 7},
	}))
	s, err = f.ds.GetRemoteSyncSettings(ctx, f.owner)
	require.NoError(t, err)
	assert.False(t, s.Running)
	assert.Equal(t, models.RemoteSyncStatusOK, s.Status)
	require.NotNil(t, s.LastRun)
	assert.Equal(t, 7, s.LastRun.MessagesAdded)
	assert.NotNil(t, s.LastSuccessAt)
	acct, err := f.ds.GetRemoteSyncAccount(ctx, f.owner)
	require.NoError(t, err)
	assert.NotNil(t, acct.LastRunAt, "a wake reads it to skip fresh accounts")
	assert.False(t, acct.FullWalk, "a clean run needs no full walk next time")

	due, err = f.ds.ListDueRemoteSyncs(ctx, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	assert.Empty(t, due, "ran recently")
	due, err = f.ds.ListDueRemoteSyncs(ctx, time.Now().Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{f.owner}, due)

	msg := "token refused"
	require.NoError(t, f.ds.FinishRemoteSyncRun(ctx, f.owner, models.RemoteSyncRunResult{Status: models.RemoteSyncStatusNeedsAttention, Error: &msg}))
	due, err = f.ds.ListDueRemoteSyncs(ctx, time.Now().Add(time.Minute))
	require.NoError(t, err)
	assert.Empty(t, due, "needs_attention waits for its owner")
}

func TestApplyCreatesAMirrorThenAppendsWithoutDuplicates(t *testing.T) {
	f := newRemoteSyncFixture(t)
	ctx := context.Background()
	personaID := uuid.New()
	createTestPersonality(t, f.ds, personaID, f.owner)
	persona, err := f.ds.dbClient.Personality.Get(ctx, personaID)
	require.NoError(t, err)

	remote := uuid.New()
	th := f.thread(remote, "Trip planning")
	th.PersonalityName = "  " + persona.Name + " "
	first := f.msgs(0, "hi", "hello", "lisbon?")
	res := f.apply(t, th, first)
	assert.Equal(t, models.RemoteSyncCreated, res.Outcome)
	assert.Equal(t, 3, res.MessagesAdded)

	mirror, err := f.ds.dbClient.Chat.Get(ctx, res.ChatID)
	require.NoError(t, err)
	assert.Equal(t, "Trip planning", mirror.Name)
	assert.Equal(t, models.ChatSourceRemoteSync, mirror.Source)
	assert.Equal(t, models.ConversationImportHash(remote.String()), mirror.ImportHash)
	assert.Empty(t, mirror.CheckpointSummary, "summarized lazily on first use")
	require.NotNil(t, mirror.LastMessageTime)
	assert.True(t, mirror.LastMessageTime.Equal(first[2].SentAt))
	pid, err := mirror.QueryPersonality().OnlyID(ctx)
	require.NoError(t, err)
	assert.Equal(t, personaID, pid, "persona matched by name")

	// The overlap re-reads the last message; it is not copied twice.
	second := append([]models.RemoteSyncMessage{first[2]}, f.msgs(3, "yes, lisbon")...)
	res = f.apply(t, th, second)
	assert.Equal(t, models.RemoteSyncAppended, res.Outcome)
	assert.Equal(t, 1, res.MessagesAdded)
	assert.Equal(t, mirror.ID, res.ChatID)
	assert.Equal(t, []string{"hi", "hello", "lisbon?", "yes, lisbon"}, f.texts(t, mirror.ID))

	res = f.apply(t, th, second)
	assert.Equal(t, models.RemoteSyncUnchanged, res.Outcome)

	s, err := f.ds.GetRemoteSyncSettings(ctx, f.owner)
	require.NoError(t, err)
	assert.Equal(t, &models.RemoteSyncTotals{Threads: 1, Messages: 4, Forks: 0}, s.Totals)

	links, err := f.ds.ListRemoteSyncChatLinks(ctx, f.owner)
	require.NoError(t, err)
	require.Contains(t, links, remote)
	require.NotNil(t, links[remote].RemoteHeadSentAt)
	assert.True(t, links[remote].RemoteHeadSentAt.Equal(f.t0.Add(3*time.Minute)))
	require.NotNil(t, links[remote].RemoteLastMessageTime)
}

func TestApplyForksWhenBothSidesWrote(t *testing.T) {
	f := newRemoteSyncFixture(t)
	ctx := context.Background()
	remote := uuid.New()
	th := f.thread(remote, "Trip planning")

	res := f.apply(t, th, f.msgs(0, "A1", "B1"))
	original := res.ChatID

	// At home: a local turn in the mirror.
	f.writeLocal(t, original, "a2 (home)", f.t0.Add(10*time.Minute))
	// Meanwhile on the phone the remote thread moved on.
	res = f.apply(t, th, f.msgs(2, "A2 (phone)", "B2 (phone)"))
	assert.Equal(t, models.RemoteSyncForked, res.Outcome)
	fork := res.ChatID
	assert.NotEqual(t, original, fork)

	assert.Equal(t, []string{"A1", "B1", "a2 (home)"}, f.texts(t, original), "the home continuation is untouched")
	assert.Equal(t, []string{"A1", "B1", "A2 (phone)", "B2 (phone)"}, f.texts(t, fork))
	forkRow, err := f.ds.dbClient.Chat.Get(ctx, fork)
	require.NoError(t, err)
	assert.Equal(t, "Trip planning (from whatiff.chat)", forkRow.Name)
	assert.Empty(t, forkRow.ImportHash, "only the first mirror carries the import hash")
	assert.Equal(t, models.ChatSourceRemoteSync, forkRow.Source)

	links, err := f.ds.ListRemoteSyncChatLinks(ctx, f.owner)
	require.NoError(t, err)
	assert.Equal(t, fork, links[remote].ChatID, "the fork is now tracking")
	assert.Equal(t, 1, links[remote].Forks)

	// More remote messages go to the fork, which has no local tail.
	res = f.apply(t, th, f.msgs(4, "A3"))
	assert.Equal(t, models.RemoteSyncAppended, res.Outcome)
	assert.Equal(t, fork, res.ChatID)

	// Writing in the fork and diverging again forks the fork.
	f.writeLocal(t, fork, "a4 (home again)", f.t0.Add(20*time.Minute))
	res = f.apply(t, th, f.msgs(5, "B3"))
	assert.Equal(t, models.RemoteSyncForked, res.Outcome)
	assert.Equal(t, []string{"A1", "B1", "A2 (phone)", "B2 (phone)", "A3", "B3"}, f.texts(t, res.ChatID))

	s, err := f.ds.GetRemoteSyncSettings(ctx, f.owner)
	require.NoError(t, err)
	assert.Equal(t, 2, s.Totals.Forks)
	assert.Equal(t, 6, s.Totals.Messages, "distinct remote messages, not copies")
}

func TestApplyAdoptsAnAccountImportedThread(t *testing.T) {
	f := newRemoteSyncFixture(t)
	ctx := context.Background()
	remote := uuid.New()
	history := f.msgs(0, "A1", "B1", "A2", "B2")

	// An earlier account-export import holds the first three messages.
	imported, err := f.ds.dbClient.Chat.Create().
		SetName("Trip planning").SetOwnerID(f.owner).SetSource(models.ChatSourceAnthropic).
		SetImportHash(models.ConversationImportHash(remote.String())).
		Save(ctx)
	require.NoError(t, err)
	for _, m := range history[:3] {
		_, err := f.ds.dbClient.ChatMessage.Create().SetChatID(imported.ID).SetMessage(m.Message).
			SetOrigin(chatmessage.Origin(m.Origin)).SetSentAt(m.SentAt.Add(time.Microsecond)).Save(ctx)
		require.NoError(t, err)
	}

	res := f.apply(t, f.thread(remote, "Trip planning"), history)
	assert.Equal(t, models.RemoteSyncAdopted, res.Outcome)
	assert.Equal(t, imported.ID, res.ChatID)
	assert.Equal(t, 1, res.MessagesAdded)
	assert.Equal(t, []string{"A1", "B1", "A2", "B2"}, f.texts(t, imported.ID))

	n, err := f.ds.dbClient.Chat.Query().Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "nothing copied twice")
}

func TestApplyAdoptionWithALocalTailForks(t *testing.T) {
	f := newRemoteSyncFixture(t)
	ctx := context.Background()
	remote := uuid.New()
	history := f.msgs(0, "A1", "B1", "A2 (phone)")

	imported, err := f.ds.dbClient.Chat.Create().
		SetName("Trip").SetOwnerID(f.owner).SetSource(models.ChatSourceAnthropic).
		SetImportHash(models.ConversationImportHash(remote.String())).Save(ctx)
	require.NoError(t, err)
	f.writeLocal(t, imported.ID, "A1", history[0].SentAt)
	_, err = f.ds.dbClient.ChatMessage.Create().SetChatID(imported.ID).SetMessage("B1").
		SetOrigin(chatmessage.OriginAssistant).SetSentAt(history[1].SentAt).Save(ctx)
	require.NoError(t, err)
	f.writeLocal(t, imported.ID, "a2 (home)", f.t0.Add(30*time.Minute))

	res := f.apply(t, f.thread(remote, "Trip"), history)
	assert.Equal(t, models.RemoteSyncForked, res.Outcome)
	assert.Equal(t, []string{"A1", "B1", "a2 (home)"}, f.texts(t, imported.ID))
	assert.Equal(t, []string{"A1", "B1", "A2 (phone)"}, f.texts(t, res.ChatID))
}

func TestApplyDetachesWhenTheMirrorWasDeleted(t *testing.T) {
	f := newRemoteSyncFixture(t)
	ctx := context.Background()
	remote := uuid.New()
	th := f.thread(remote, "Gone")
	res := f.apply(t, th, f.msgs(0, "A1"))
	_, err := f.ds.dbClient.ChatMessage.Delete().Exec(ctx)
	require.NoError(t, err)
	require.NoError(t, f.ds.dbClient.Chat.DeleteOneID(res.ChatID).Exec(ctx))

	res = f.apply(t, th, f.msgs(1, "B1"))
	assert.Equal(t, models.RemoteSyncDetached, res.Outcome)
	res = f.apply(t, th, f.msgs(2, "A2"))
	assert.Equal(t, models.RemoteSyncDetached, res.Outcome, "stays detached")

	n, err := f.ds.dbClient.Chat.Query().Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, n, "not recreated")
	links, err := f.ds.ListRemoteSyncChatLinks(ctx, f.owner)
	require.NoError(t, err)
	assert.Equal(t, models.RemoteSyncLinkDetached, links[remote].State)
}

func TestApplyCopiesRemoteRenamesUnlessRenamedLocally(t *testing.T) {
	f := newRemoteSyncFixture(t)
	ctx := context.Background()
	remote := uuid.New()
	res := f.apply(t, f.thread(remote, "Draft"), f.msgs(0, "A1"))

	f.apply(t, f.thread(remote, "Trip planning"), nil)
	c, err := f.ds.dbClient.Chat.Get(ctx, res.ChatID)
	require.NoError(t, err)
	assert.Equal(t, "Trip planning", c.Name)

	require.NoError(t, f.ds.dbClient.Chat.UpdateOneID(res.ChatID).SetName("My name").Exec(ctx))
	f.apply(t, f.thread(remote, "Trip planning v2"), nil)
	c, err = f.ds.dbClient.Chat.Get(ctx, res.ChatID)
	require.NoError(t, err)
	assert.Equal(t, "My name", c.Name, "a local rename wins")
}

func TestApplyIsOwnerScoped(t *testing.T) {
	f := newRemoteSyncFixture(t)
	ctx := context.Background()
	remote := uuid.New()

	_, err := f.ds.ApplyRemoteSyncThread(ctx, f.other, f.thread(remote, "x"), f.msgs(0, "A1"))
	assert.ErrorIs(t, err, ErrRemoteSyncNotFound, "no sync configured for the other account")

	// The other account's import-hash thread is never adopted into the owner's sync.
	_, err = f.ds.dbClient.Chat.Create().SetName("theirs").SetOwnerID(f.other).
		SetImportHash(models.ConversationImportHash(remote.String())).Save(ctx)
	require.NoError(t, err)
	res := f.apply(t, f.thread(remote, "mine"), f.msgs(0, "A1"))
	assert.Equal(t, models.RemoteSyncCreated, res.Outcome)
}

func TestDeleteRemoteSyncKeepsThreadsAndReadoptsThem(t *testing.T) {
	f := newRemoteSyncFixture(t)
	ctx := context.Background()
	remote := uuid.New()
	th := f.thread(remote, "Trip")
	history := f.msgs(0, "A1", "B1")
	res := f.apply(t, th, history)

	require.NoError(t, f.ds.DeleteRemoteSync(ctx, f.owner))
	n, err := f.ds.dbClient.RemoteSyncMessage.Query().Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, n, "links cascade")
	assert.Equal(t, []string{"A1", "B1"}, f.texts(t, res.ChatID), "threads stay")

	token := "wht_new-token-wxyz"
	require.NoError(t, f.ds.UpsertRemoteSync(ctx, f.owner, models.RemoteSyncUpsert{BaseURL: "https://whatiff.chat", Token: &token}))
	again := f.apply(t, th, append(history, f.msgs(2, "A2")...))
	assert.Equal(t, models.RemoteSyncAdopted, again.Outcome)
	assert.Equal(t, res.ChatID, again.ChatID)
	assert.Equal(t, []string{"A1", "B1", "A2"}, f.texts(t, res.ChatID))
}
