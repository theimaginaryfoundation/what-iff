package datastore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createDiscordTestTables creates the four Discord tables, matching the
// generated ent schema (ent/schema/discord.go), with the cascades the
// relay relies on.
func createDiscordTestTables(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, ddl := range []string{
		`CREATE TABLE discord_bots (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			personality_id uuid NOT NULL UNIQUE,
			application_id text NOT NULL,
			bot_user_id text NOT NULL UNIQUE,
			bot_username text NOT NULL DEFAULT '',
			token text NOT NULL,
			message_content bool NOT NULL DEFAULT false,
			status text NOT NULL DEFAULT 'active',
			last_error text,
			user_discord_bots uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE discord_bindings (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			chat_id uuid NOT NULL,
			guild_id text NOT NULL,
			channel_id text NOT NULL,
			guild_name text NOT NULL DEFAULT '',
			channel_name text NOT NULL DEFAULT '',
			inbound_enabled bool NOT NULL DEFAULT true,
			allow_user_ids json NOT NULL,
			deny_user_ids json NOT NULL,
			allow_unrestricted bool NOT NULL DEFAULT false,
			status text NOT NULL DEFAULT 'active',
			last_error text,
			last_activity_at datetime,
			discord_bot_bindings uuid NOT NULL REFERENCES discord_bots(id) ON DELETE CASCADE,
			UNIQUE (channel_id, discord_bot_bindings)
		)`,
		`CREATE TABLE discord_message_links (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			direction text NOT NULL,
			chat_message_id uuid,
			discord_message_id text,
			discord_channel_id text NOT NULL DEFAULT '',
			posted_message_ids json NOT NULL,
			author_id text NOT NULL DEFAULT '',
			author_name text NOT NULL DEFAULT '',
			reply_to_link_id uuid,
			status text NOT NULL DEFAULT 'received',
			error text,
			attempts integer NOT NULL DEFAULT 0,
			discord_binding_links uuid NOT NULL REFERENCES discord_bindings(id) ON DELETE CASCADE,
			UNIQUE (discord_message_id, discord_binding_links)
		)`,
		`CREATE TABLE discord_pending_posts (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			chat_id uuid NOT NULL,
			source text NOT NULL,
			expires_at datetime NOT NULL,
			discord_binding_pending_posts uuid NOT NULL REFERENCES discord_bindings(id) ON DELETE CASCADE
		)`,
	} {
		_, err := db.Exec(ddl)
		require.NoError(t, err)
	}
}

type discordFixture struct {
	ds      *Datastore
	owner   uuid.UUID
	other   uuid.UUID
	persona uuid.UUID
	chat    uuid.UUID
}

func newDiscordFixture(t *testing.T) *discordFixture {
	t.Helper()
	ds, cleanup := newTestDatastore(t, createMemoryImportTestSchema, alterChatsTableForAgentJobTests)
	t.Cleanup(cleanup)
	f := &discordFixture{ds: ds, owner: uuid.New(), other: uuid.New(), persona: uuid.New(), chat: uuid.New()}
	createTestUser(t, ds, f.owner)
	createTestUser(t, ds, f.other)
	createTestPersonality(t, ds, f.persona, f.owner)
	createTestChat(t, ds, f.chat, f.owner)
	return f
}

func (f *discordFixture) bot(t *testing.T) *models.DiscordBot {
	t.Helper()
	bot, err := f.ds.CreateDiscordBot(context.Background(), f.owner, models.DiscordBotCreate{
		PersonalityID: f.persona, ApplicationID: "app", BotUserID: "111", BotUsername: "vix", Token: "secret-token",
	})
	require.NoError(t, err)
	return bot
}

func (f *discordFixture) binding(t *testing.T, botID uuid.UUID, channel string) *models.DiscordBinding {
	t.Helper()
	b, err := f.ds.CreateDiscordBinding(context.Background(), f.owner, botID, models.DiscordBindingCreate{
		ChatID: f.chat, GuildID: "g", ChannelID: channel, ChannelName: "general", InboundEnabled: true,
	})
	require.NoError(t, err)
	return b
}

func TestDiscordBotTokensAreEncryptedAndOwnerScoped(t *testing.T) {
	f := newDiscordFixture(t)
	ctx := context.Background()
	bot := f.bot(t)

	raw, err := f.ds.dbClient.DiscordBot.Get(ctx, bot.ID)
	require.NoError(t, err)
	assert.NotEqual(t, "secret-token", raw.Token, "stored encrypted")

	creds, err := f.ds.GetDiscordBotCredentials(ctx, f.owner, bot.ID)
	require.NoError(t, err)
	assert.Equal(t, "secret-token", creds.Token)
	assert.Equal(t, f.owner, creds.OwnerID)

	_, err = f.ds.GetDiscordBotCredentials(ctx, f.other, bot.ID)
	assert.ErrorIs(t, err, ErrDiscordBotNotFound)
	assert.ErrorIs(t, f.ds.DeleteDiscordBot(ctx, f.other, bot.ID), ErrDiscordBotNotFound)

	bots, err := f.ds.ListDiscordBots(ctx, f.other)
	require.NoError(t, err)
	assert.Empty(t, bots)
}

func TestDiscordBotIsUniquePerPersonaAndPerBotUser(t *testing.T) {
	f := newDiscordFixture(t)
	f.bot(t)
	_, err := f.ds.CreateDiscordBot(context.Background(), f.owner, models.DiscordBotCreate{
		PersonalityID: f.persona, ApplicationID: "app2", BotUserID: "222", Token: "t",
	})
	assert.ErrorIs(t, err, ErrDiscordBotExists)

	// Another user's persona cannot be used.
	_, err = f.ds.CreateDiscordBot(context.Background(), f.other, models.DiscordBotCreate{
		PersonalityID: f.persona, ApplicationID: "app3", BotUserID: "333", Token: "t",
	})
	assert.Error(t, err)
}

func TestActiveBotCredentialsCoverOnlyActiveBots(t *testing.T) {
	f := newDiscordFixture(t)
	ctx := context.Background()
	bot := f.bot(t)

	list, err := f.ds.ListActiveDiscordBotCredentials(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "secret-token", list[0].Token)

	require.NoError(t, f.ds.SetDiscordBotStatus(ctx, bot.ID, models.DiscordBotInvalidToken, "bad"))
	list, err = f.ds.ListActiveDiscordBotCredentials(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestDiscordBindingsResolveAndStayOwnerScoped(t *testing.T) {
	f := newDiscordFixture(t)
	ctx := context.Background()
	bot := f.bot(t)
	b := f.binding(t, bot.ID, "c1")

	_, err := f.ds.CreateDiscordBinding(ctx, f.owner, bot.ID, models.DiscordBindingCreate{ChatID: f.chat, GuildID: "g", ChannelID: "c1"})
	assert.ErrorIs(t, err, ErrDiscordBindingExists)

	otherChat := uuid.New()
	createTestChat(t, f.ds, otherChat, f.other)
	_, err = f.ds.CreateDiscordBinding(ctx, f.owner, bot.ID, models.DiscordBindingCreate{ChatID: otherChat, GuildID: "g", ChannelID: "c2"})
	assert.Error(t, err, "another account's chat cannot be bound")

	target, err := f.ds.FindDiscordBindingTarget(ctx, bot.ID, "c1")
	require.NoError(t, err)
	assert.Equal(t, b.ID, target.Binding.ID)
	assert.Equal(t, f.owner, target.Bot.OwnerID)
	assert.Equal(t, "secret-token", target.Bot.Token)
	_, err = f.ds.FindDiscordBindingTarget(ctx, bot.ID, "nope")
	assert.ErrorIs(t, err, ErrDiscordBindingNotFound)

	allow := []string{"1", "2"}
	updated, err := f.ds.UpdateDiscordBinding(ctx, f.owner, b.ID, models.DiscordBindingPatch{AllowUserIDs: &allow})
	require.NoError(t, err)
	assert.Equal(t, allow, updated.AllowUserIDs)
	assert.Equal(t, []string{}, updated.DenyUserIDs)
	_, err = f.ds.UpdateDiscordBinding(ctx, f.other, b.ID, models.DiscordBindingPatch{AllowUserIDs: &allow})
	assert.ErrorIs(t, err, ErrDiscordBindingNotFound)

	forChat, err := f.ds.ListDiscordBindingsForChat(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Len(t, forChat, 1)
	forPersona, err := f.ds.ListDiscordBindingsForPersonality(ctx, f.owner, f.persona)
	require.NoError(t, err)
	assert.Len(t, forPersona, 1)
}

func TestInboundMessagesAreRecordedOnceAndFoundByTheirSavedMessage(t *testing.T) {
	f := newDiscordFixture(t)
	ctx := context.Background()
	b := f.binding(t, f.bot(t).ID, "c1")

	in := models.DiscordInbound{BindingID: b.ID, DiscordMessageID: "m1", DiscordChannelID: "c1", AuthorID: "222", AuthorName: "alice"}
	link, created, err := f.ds.RecordInboundDiscordMessage(ctx, in)
	require.NoError(t, err)
	require.True(t, created)
	_, created, err = f.ds.RecordInboundDiscordMessage(ctx, in)
	require.NoError(t, err)
	assert.False(t, created, "a redelivered message is not recorded twice")

	msgID := uuid.New()
	require.NoError(t, f.ds.AttachDiscordLinkMessage(ctx, link.ID, msgID))
	found, err := f.ds.FindInboundDiscordLink(ctx, msgID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, b.ID, found.BindingID)
	assert.Equal(t, "alice", found.AuthorName)

	none, err := f.ds.FindInboundDiscordLink(ctx, uuid.New())
	require.NoError(t, err)
	assert.Nil(t, none)
}

func TestOutboundLinksAreIdempotentPerReply(t *testing.T) {
	f := newDiscordFixture(t)
	ctx := context.Background()
	b := f.binding(t, f.bot(t).ID, "c1")
	reply := uuid.New()

	link, created, err := f.ds.StartOutboundDiscordLink(ctx, b.ID, reply, "c1", nil)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, f.ds.FinishOutboundDiscordLink(ctx, link.ID, []string{"d1", "d2"}, nil))

	again, created, err := f.ds.StartOutboundDiscordLink(ctx, b.ID, reply, "c1", nil)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, models.DiscordLinkSent, again.Status)
	assert.Equal(t, []string{"d1", "d2"}, again.PostedMessageIDs)
	assert.Equal(t, 1, again.Attempts)

	links, err := f.ds.ListDiscordLinksForChat(ctx, f.owner, f.chat, 10)
	require.NoError(t, err)
	assert.Len(t, links, 1)
	links, err = f.ds.ListDiscordLinksForChat(ctx, f.other, f.chat, 10)
	require.NoError(t, err)
	assert.Empty(t, links)
}

func TestPendingPostsAreConsumedOnceAndExpire(t *testing.T) {
	f := newDiscordFixture(t)
	ctx := context.Background()
	b := f.binding(t, f.bot(t).ID, "c1")

	require.NoError(t, f.ds.SetDiscordPendingPost(ctx, f.owner, b.ID, f.chat, models.DiscordPostFromComposer))
	require.NoError(t, f.ds.SetDiscordPendingPost(ctx, f.owner, b.ID, f.chat, models.DiscordPostFromTool)) // replaces
	pending, err := f.ds.ListDiscordPendingPosts(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{b.ID}, pending)

	assert.Error(t, f.ds.SetDiscordPendingPost(ctx, f.other, b.ID, f.chat, models.DiscordPostFromComposer), "not the other user's binding")

	ids, err := f.ds.ConsumeDiscordPendingPosts(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{b.ID}, ids)
	ids, err = f.ds.ConsumeDiscordPendingPosts(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Empty(t, ids)

	// An expired request is dropped, not posted.
	require.NoError(t, f.ds.SetDiscordPendingPost(ctx, f.owner, b.ID, f.chat, models.DiscordPostFromComposer))
	_, err = f.ds.dbClient.DiscordPendingPost.Update().SetExpiresAt(time.Now().Add(-time.Minute)).Save(ctx)
	require.NoError(t, err)
	ids, err = f.ds.ConsumeDiscordPendingPosts(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Empty(t, ids)
	n, err := f.ds.dbClient.DiscordPendingPost.Query().Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)

	// Clearing withdraws the request.
	require.NoError(t, f.ds.SetDiscordPendingPost(ctx, f.owner, b.ID, f.chat, models.DiscordPostFromComposer))
	require.NoError(t, f.ds.ClearDiscordPendingPost(ctx, f.owner, b.ID, f.chat))
	pending, err = f.ds.ListDiscordPendingPosts(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestDeletingABotRemovesItsBindings(t *testing.T) {
	f := newDiscordFixture(t)
	ctx := context.Background()
	bot := f.bot(t)
	f.binding(t, bot.ID, "c1")

	require.NoError(t, f.ds.DeleteDiscordBot(ctx, f.owner, bot.ID))
	bindings, err := f.ds.ListDiscordBindings(ctx, f.owner)
	require.NoError(t, err)
	assert.Empty(t, bindings)
}

func TestDiscordBindingAllowUnrestrictedDefaultsOffAndRoundTrips(t *testing.T) {
	f := newDiscordFixture(t)
	ctx := context.Background()
	bot := f.bot(t)

	b := f.binding(t, bot.ID, "chan-ack")
	assert.False(t, b.AllowUnrestricted, "a binding is not acknowledged unless asked")
	target, err := f.ds.FindDiscordBindingTarget(ctx, bot.ID, "chan-ack")
	require.NoError(t, err)
	assert.False(t, target.Binding.AllowUnrestricted, "the relay sees the same")

	yes := true
	got, err := f.ds.UpdateDiscordBinding(ctx, f.owner, b.ID, models.DiscordBindingPatch{AllowUnrestricted: &yes})
	require.NoError(t, err)
	assert.True(t, got.AllowUnrestricted)
	target, err = f.ds.GetDiscordBindingTarget(ctx, b.ID)
	require.NoError(t, err)
	assert.True(t, target.Binding.AllowUnrestricted)

	// The relay can withdraw a stale acknowledgement, and withdrawing twice is harmless.
	require.NoError(t, f.ds.WithdrawDiscordBindingAcknowledgement(ctx, b.ID))
	require.NoError(t, f.ds.WithdrawDiscordBindingAcknowledgement(ctx, b.ID))
	target, err = f.ds.GetDiscordBindingTarget(ctx, b.ID)
	require.NoError(t, err)
	assert.False(t, target.Binding.AllowUnrestricted)
	_, err = f.ds.UpdateDiscordBinding(ctx, f.owner, b.ID, models.DiscordBindingPatch{AllowUnrestricted: &yes})
	require.NoError(t, err)

	// Repointing without carrying the acknowledgement leaves it for the caller to reset;
	// a patch that omits it does not change it.
	name := "renamed"
	got, err = f.ds.UpdateDiscordBinding(ctx, f.owner, b.ID, models.DiscordBindingPatch{ChannelName: &name})
	require.NoError(t, err)
	assert.True(t, got.AllowUnrestricted)

	ack, err := f.ds.CreateDiscordBinding(ctx, f.owner, bot.ID, models.DiscordBindingCreate{
		ChatID: f.chat, GuildID: "g", ChannelID: "chan-ack2", InboundEnabled: true, AllowUnrestricted: true,
	})
	require.NoError(t, err)
	assert.True(t, ack.AllowUnrestricted)
}

// A relay thread the server creates starts with its default-off tools in the same insert, so it
// never exists with them on.
func TestCreateChat_StoresDisabledTools(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createAgentJobTestUser(t, ds)
	modelID := createAgentJobTestModel(t, ds)
	_, err := ds.dbClient.UserPreference.Create().SetUserID(userID).SetDefaultModel(modelID).Save(ctx)
	require.NoError(t, err)
	persona := createAgentJobTestPersonality(t, ds, userID)

	chat, err := ds.CreateChat(ctx, userID, models.Chat{
		Name: "Discord · #general", PersonalityID: persona, Sandboxed: true,
		DisabledTools: []string{"web_search", "generate_image"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"web_search", "generate_image"}, chat.DisabledTools)
	assert.True(t, chat.Sandboxed)
	row, err := ds.dbClient.Chat.Get(ctx, chat.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"web_search", "generate_image"}, row.DisabledTools)

	plain, err := ds.CreateChat(ctx, userID, models.Chat{Name: "plain"})
	require.NoError(t, err)
	assert.Empty(t, plain.DisabledTools)
}
