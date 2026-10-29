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

// createSlackTestTables creates the four Slack tables, matching the generated ent schema
// (ent/schema/slack.go), with the cascades the relay relies on. createDiscordTestTables calls
// it too, since the relay provenance lookups read every platform's bindings.
func createSlackTestTables(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS slack_apps (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			personality_id uuid NOT NULL,
			team_id text NOT NULL,
			team_name text NOT NULL DEFAULT '',
			team_url text NOT NULL DEFAULT '',
			app_id text NOT NULL UNIQUE,
			bot_user_id text NOT NULL,
			bot_id text NOT NULL DEFAULT '',
			bot_name text NOT NULL DEFAULT '',
			bot_token text NOT NULL,
			app_token text NOT NULL,
			status text NOT NULL DEFAULT 'active',
			last_error text,
			last_connected_at datetime,
			user_slack_apps uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			UNIQUE (team_id, personality_id)
		)`,
		`CREATE TABLE IF NOT EXISTS slack_bindings (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			chat_id uuid NOT NULL,
			channel_id text NOT NULL,
			channel_name text NOT NULL DEFAULT '',
			is_private bool NOT NULL DEFAULT false,
			inbound_enabled bool NOT NULL DEFAULT true,
			allow_user_ids json NOT NULL,
			deny_user_ids json NOT NULL,
			answer_agents bool NOT NULL DEFAULT false,
			allow_unrestricted bool NOT NULL DEFAULT false,
			status text NOT NULL DEFAULT 'active',
			last_error text,
			last_activity_at datetime,
			slack_app_bindings uuid NOT NULL REFERENCES slack_apps(id) ON DELETE CASCADE,
			UNIQUE (channel_id, slack_app_bindings)
		)`,
		`CREATE TABLE IF NOT EXISTS slack_message_links (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			direction text NOT NULL,
			chat_message_id uuid,
			message_ts text,
			thread_ts text NOT NULL DEFAULT '',
			posted_ts json NOT NULL,
			author_user_id text NOT NULL DEFAULT '',
			author_name text NOT NULL DEFAULT '',
			reply_to_link_id uuid,
			status text NOT NULL DEFAULT 'received',
			error text,
			attempts integer NOT NULL DEFAULT 0,
			agent_depth integer NOT NULL DEFAULT 0,
			slack_binding_links uuid NOT NULL REFERENCES slack_bindings(id) ON DELETE CASCADE,
			UNIQUE (message_ts, slack_binding_links)
		)`,
		`CREATE TABLE IF NOT EXISTS slack_pending_posts (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			chat_id uuid NOT NULL,
			source text NOT NULL,
			expires_at datetime NOT NULL,
			slack_binding_pending_posts uuid NOT NULL REFERENCES slack_bindings(id) ON DELETE CASCADE
		)`,
	} {
		_, err := db.Exec(ddl)
		require.NoError(t, err)
	}
}

type slackFixture struct {
	ds      *Datastore
	owner   uuid.UUID
	other   uuid.UUID
	persona uuid.UUID
	chat    uuid.UUID
}

func newSlackFixture(t *testing.T) *slackFixture {
	t.Helper()
	ds, cleanup := newTestDatastore(t, createMemoryImportTestSchema, alterChatsTableForAgentJobTests)
	t.Cleanup(cleanup)
	f := &slackFixture{ds: ds, owner: uuid.New(), other: uuid.New(), persona: uuid.New(), chat: uuid.New()}
	createTestUser(t, ds, f.owner)
	createTestUser(t, ds, f.other)
	createTestPersonality(t, ds, f.persona, f.owner)
	createTestChat(t, ds, f.chat, f.owner)
	return f
}

func (f *slackFixture) app(t *testing.T, appID, teamID string) *models.SlackApp {
	t.Helper()
	a, err := f.ds.CreateSlackApp(context.Background(), f.owner, models.SlackAppCreate{
		PersonalityID: f.persona, TeamID: teamID, TeamName: "Acme", AppID: appID,
		BotUserID: "U-" + appID, BotID: "B-" + appID, BotName: "vix",
		BotToken: "xoxb-" + appID, AppToken: "xapp-" + appID,
	})
	require.NoError(t, err)
	return a
}

func (f *slackFixture) binding(t *testing.T, appID uuid.UUID, channel string) *models.SlackBinding {
	t.Helper()
	b, err := f.ds.CreateSlackBinding(context.Background(), f.owner, appID, models.SlackBindingCreate{
		ChatID: f.chat, ChannelID: channel, ChannelName: "general", InboundEnabled: true,
	})
	require.NoError(t, err)
	return b
}

func TestSlackTokensAreEncryptedAndOwnerScoped(t *testing.T) {
	f := newSlackFixture(t)
	ctx := context.Background()
	a := f.app(t, "A1", "T1")

	raw, err := f.ds.dbClient.SlackApp.Get(ctx, a.ID)
	require.NoError(t, err)
	assert.NotContains(t, raw.BotToken, "xoxb", "bot token stored encrypted")
	assert.NotContains(t, raw.AppToken, "xapp", "app token stored encrypted")
	creds, err := f.ds.GetSlackAppCredentials(ctx, f.owner, a.ID)
	require.NoError(t, err)
	assert.Equal(t, "xoxb-A1", creds.BotToken)
	assert.Equal(t, "xapp-A1", creds.AppToken)
	_, err = f.ds.GetSlackAppCredentials(ctx, f.other, a.ID)
	assert.ErrorIs(t, err, ErrSlackAppNotFound)

	for name, in := range map[string]models.SlackAppCreate{
		"the same app twice":                            {PersonalityID: f.persona, TeamID: "T2", AppID: "A1", BotUserID: "U", BotToken: "x", AppToken: "y"},
		"a second app for the persona in one workspace": {PersonalityID: f.persona, TeamID: "T1", AppID: "A9", BotUserID: "U", BotToken: "x", AppToken: "y"},
	} {
		_, err := f.ds.CreateSlackApp(ctx, f.owner, in)
		assert.ErrorIs(t, err, ErrSlackAppExists, name)
	}
	other := f.app(t, "A2", "T2")
	assert.Equal(t, f.persona, other.PersonalityID, "a persona may be in several workspaces")

	tok := "xoxb-new"
	_, err = f.ds.UpdateSlackApp(ctx, f.owner, a.ID, models.SlackAppPatch{BotToken: &tok})
	require.NoError(t, err)
	creds, err = f.ds.GetSlackAppCredentials(ctx, f.owner, a.ID)
	require.NoError(t, err)
	assert.Equal(t, "xoxb-new", creds.BotToken)
	assert.Equal(t, "xapp-A1", creds.AppToken, "the other token is left alone")
}

func TestSlackActiveAppsAndGatewayState(t *testing.T) {
	f := newSlackFixture(t)
	ctx := context.Background()
	a := f.app(t, "A1", "T1")

	apps, err := f.ds.ListActiveSlackAppCredentials(ctx)
	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.Equal(t, f.owner, apps[0].OwnerID)
	assert.Equal(t, "xapp-A1", apps[0].AppToken)

	require.NoError(t, f.ds.SetSlackAppState(ctx, a.ID, models.SlackAppActive, ""))
	got, err := f.ds.GetSlackApp(ctx, f.owner, a.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.LastConnectedAt)

	require.NoError(t, f.ds.SetSlackAppState(ctx, a.ID, models.SlackAppInvalidToken, "invalid_auth"))
	apps, err = f.ds.ListActiveSlackAppCredentials(ctx)
	require.NoError(t, err)
	assert.Empty(t, apps, "an app with a rejected token is not connected")
	require.NoError(t, f.ds.SetSlackAppState(ctx, a.ID, models.SlackAppActive, ""))
	got, err = f.ds.GetSlackApp(ctx, f.owner, a.ID)
	require.NoError(t, err)
	assert.Equal(t, models.SlackAppInvalidToken, got.Status, "a socket that still connects cannot clear invalid_token")
	active := models.SlackAppActive
	_, err = f.ds.UpdateSlackApp(ctx, f.owner, a.ID, models.SlackAppPatch{Status: &active})
	require.NoError(t, err)

	disabled := models.SlackAppDisabled
	_, err = f.ds.UpdateSlackApp(ctx, f.owner, a.ID, models.SlackAppPatch{Status: &disabled})
	require.NoError(t, err)
	require.NoError(t, f.ds.SetSlackAppState(ctx, a.ID, models.SlackAppActive, ""))
	got, err = f.ds.GetSlackApp(ctx, f.owner, a.ID)
	require.NoError(t, err)
	assert.Equal(t, models.SlackAppDisabled, got.Status, "the gateway cannot resume a paused app")
}

func TestSlackLinksDedupeAndThreadsContinue(t *testing.T) {
	f := newSlackFixture(t)
	ctx := context.Background()
	a := f.app(t, "A1", "T1")
	b := f.binding(t, a.ID, "C1")

	target, err := f.ds.FindSlackBindingTarget(ctx, a.ID, "C1")
	require.NoError(t, err)
	assert.Equal(t, b.ID, target.Binding.ID)
	assert.Equal(t, "xoxb-A1", target.App.BotToken)
	assert.Equal(t, "Acme", target.Binding.TeamName)
	_, err = f.ds.FindSlackBindingTarget(ctx, a.ID, "C2")
	assert.ErrorIs(t, err, ErrSlackBindingNotFound)

	in := models.SlackInbound{BindingID: b.ID, MessageTS: "100.1", ThreadTS: "100.1", AuthorUserID: "UALICE", AuthorName: "Alice"}
	link, created, err := f.ds.RecordInboundSlackMessage(ctx, in)
	require.NoError(t, err)
	require.True(t, created)
	_, created, err = f.ds.RecordInboundSlackMessage(ctx, in)
	require.NoError(t, err)
	assert.False(t, created, "a redelivered message is recorded once")

	name, err := f.ds.ExternalSpeakerForTurn(ctx, f.owner, f.chat, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, "Alice", name, "while the turn starts, the speaker comes from the link without a message")

	ok, err := f.ds.SlackThreadContinues(ctx, b.ID, "100.1", "UALICE")
	require.NoError(t, err)
	assert.False(t, ok, "not until the turn has started")

	userMsg := uuid.New()
	require.NoError(t, f.ds.AttachSlackLinkMessage(ctx, link.ID, userMsg))
	found, err := f.ds.FindInboundSlackLink(ctx, userMsg)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "100.1", *found.MessageTS)

	reply := uuid.New()
	out, created, err := f.ds.StartOutboundSlackLink(ctx, b.ID, reply, "100.1", &link.ID)
	require.NoError(t, err)
	require.True(t, created)
	_, created, err = f.ds.StartOutboundSlackLink(ctx, b.ID, reply, "100.1", &link.ID)
	require.NoError(t, err)
	assert.False(t, created, "a retried hook finds the link")
	require.NoError(t, f.ds.FinishOutboundSlackLink(ctx, out.ID, "", []string{"100.2"}, nil))

	for _, tc := range []struct {
		name, thread, author string
		want                 bool
	}{
		{"the asker writing on in the thread", "100.1", "UALICE", true},
		{"someone else in the thread", "100.1", "UBOB", false},
		{"the asker in another thread", "200.1", "UALICE", false},
		{"no thread", "", "UALICE", false},
	} {
		ok, err := f.ds.SlackThreadContinues(ctx, b.ID, tc.thread, tc.author)
		require.NoError(t, err)
		assert.Equal(t, tc.want, ok, tc.name)
	}

	links, err := f.ds.ListSlackLinksForChat(ctx, f.owner, f.chat, 10)
	require.NoError(t, err)
	assert.Len(t, links, 2)
	links, err = f.ds.ListSlackLinksForChat(ctx, f.other, f.chat, 10)
	require.NoError(t, err)
	assert.Empty(t, links)

	// Bob's top-level mention was batched into the same turn, but the reply went to Alice's
	// thread only: Bob's own thread has no reply from the persona, so he has not started a
	// conversation there.
	bobLink, _, err := f.ds.RecordInboundSlackMessage(ctx, models.SlackInbound{BindingID: b.ID, MessageTS: "300.1", ThreadTS: "300.1", AuthorUserID: "UBOB", AuthorName: "Bob"})
	require.NoError(t, err)
	require.NoError(t, f.ds.AttachSlackLinkMessage(ctx, bobLink.ID, userMsg))
	ok, err = f.ds.SlackThreadContinues(ctx, b.ID, "300.1", "UBOB")
	require.NoError(t, err)
	assert.False(t, ok, "a batched asker in a thread the persona never posted in")
}

func TestSlackRelayThreadsAreExternal(t *testing.T) {
	f := newSlackFixture(t)
	ctx := context.Background()
	platform, err := f.ds.RelayPlatformForChat(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Empty(t, platform)

	a := f.app(t, "A1", "T1")
	f.binding(t, a.ID, "C1")
	ext, err := f.ds.IsExternalRelayChat(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.True(t, ext)
	platform, err = f.ds.RelayPlatformForChat(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Equal(t, "Slack", platform)
	ext, err = f.ds.IsExternalRelayChat(ctx, f.other, f.chat)
	require.NoError(t, err)
	assert.False(t, ext, "owner-scoped")
}

func TestSlackBindingsPendingPostsRenamesAndCascade(t *testing.T) {
	f := newSlackFixture(t)
	ctx := context.Background()
	a := f.app(t, "A1", "T1")
	b := f.binding(t, a.ID, "C1")
	_, err := f.ds.CreateSlackBinding(ctx, f.owner, a.ID, models.SlackBindingCreate{ChatID: f.chat, ChannelID: "C1"})
	assert.ErrorIs(t, err, ErrSlackBindingExists)

	forPersona, err := f.ds.ListSlackBindingsForPersonality(ctx, f.owner, f.persona)
	require.NoError(t, err)
	require.Len(t, forPersona, 1)
	assert.Equal(t, "Acme", forPersona[0].TeamName)

	require.NoError(t, f.ds.RenameSlackChannel(ctx, a.ID, "C1", "#lounge"))
	got, err := f.ds.GetSlackBinding(ctx, f.owner, b.ID)
	require.NoError(t, err)
	assert.Equal(t, "lounge", got.ChannelName)

	// Nothing can be armed through a paused app or a broken binding.
	paused, active := models.SlackAppDisabled, models.SlackAppActive
	_, err = f.ds.UpdateSlackApp(ctx, f.owner, a.ID, models.SlackAppPatch{Status: &paused})
	require.NoError(t, err)
	assert.ErrorIs(t, f.ds.SetSlackPendingPost(ctx, f.owner, b.ID, f.chat, models.SlackPostFromComposer), ErrSlackBindingPaused)
	_, err = f.ds.UpdateSlackApp(ctx, f.owner, a.ID, models.SlackAppPatch{Status: &active})
	require.NoError(t, err)
	require.NoError(t, f.ds.SetSlackBindingStatus(ctx, b.ID, models.SlackBindingBroken, "gone"))
	assert.ErrorIs(t, f.ds.SetSlackPendingPost(ctx, f.owner, b.ID, f.chat, models.SlackPostFromComposer), ErrSlackBindingPaused)
	require.NoError(t, f.ds.SetSlackBindingStatus(ctx, b.ID, models.SlackBindingActive, ""))

	require.NoError(t, f.ds.SetSlackPendingPost(ctx, f.owner, b.ID, f.chat, models.SlackPostFromComposer))
	pending, err := f.ds.ListSlackPendingPosts(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{b.ID}, pending)
	assert.ErrorIs(t, f.ds.SetSlackPendingPost(ctx, f.other, b.ID, f.chat, models.SlackPostFromTool), ErrSlackBindingNotFound)
	consumed, err := f.ds.ConsumeSlackPendingPosts(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{b.ID}, consumed)
	consumed, err = f.ds.ConsumeSlackPendingPosts(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Empty(t, consumed, "consumed once")

	require.NoError(t, f.ds.BreakSlackChannel(ctx, a.ID, "C1", "removed"))
	got, err = f.ds.GetSlackBinding(ctx, f.owner, b.ID)
	require.NoError(t, err)
	assert.Equal(t, models.SlackBindingBroken, got.Status)
	forPersona, err = f.ds.ListSlackBindingsForPersonality(ctx, f.owner, f.persona)
	require.NoError(t, err)
	assert.Empty(t, forPersona, "a broken binding is not a place to post")

	require.NoError(t, f.ds.DeleteSlackApp(ctx, f.owner, a.ID))
	bindings, err := f.ds.ListSlackBindings(ctx, f.owner)
	require.NoError(t, err)
	assert.Empty(t, bindings, "removing the app removes its bindings")
}

func TestSlackBatchLinksAndLoopDepth(t *testing.T) {
	f := newSlackFixture(t)
	ctx := context.Background()
	a := f.app(t, "A1", "T1")
	b := f.binding(t, a.ID, "C1")
	start := time.Now().Add(-time.Minute)

	msgID := uuid.New()
	for _, in := range []models.SlackInbound{
		{BindingID: b.ID, MessageTS: "1.1", ThreadTS: "1.1", AuthorUserID: "UALICE", AuthorName: "Alice"},
		{BindingID: b.ID, MessageTS: "1.2", ThreadTS: "1.2", AuthorUserID: "UNOVA", AuthorName: "Nova", AgentDepth: 1},
	} {
		link, created, err := f.ds.RecordInboundSlackMessage(ctx, in)
		require.NoError(t, err)
		require.True(t, created)
		require.NoError(t, f.ds.AttachSlackLinkMessage(ctx, link.ID, msgID))
		time.Sleep(2 * time.Millisecond)
	}
	found, err := f.ds.FindInboundSlackLink(ctx, msgID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "1.2", *found.MessageTS, "the reply answers the newest message")
	speaker, err := f.ds.ExternalSpeakerForMessage(ctx, f.owner, msgID)
	require.NoError(t, err)
	assert.Equal(t, "Alice, Nova", speaker)

	last, err := f.ds.LastInboundSlackLink(ctx, b.ID, start)
	require.NoError(t, err)
	require.NotNil(t, last)
	assert.Equal(t, 1, last.AgentDepth)
}
