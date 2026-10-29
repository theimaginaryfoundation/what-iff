package datastore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createBuzzTestTables creates the five Buzz tables, matching the generated ent schema
// (ent/schema/buzz.go), with the cascades the relay relies on. createDiscordTestTables calls
// it too, since the relay provenance lookups read both platforms' bindings.
func createBuzzTestTables(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS buzz_agents (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			personality_id uuid NOT NULL UNIQUE,
			pubkey text NOT NULL UNIQUE,
			secret_key text NOT NULL,
			status text NOT NULL DEFAULT 'active',
			last_error text,
			user_buzz_agents uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS buzz_communities (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			relay_url text NOT NULL,
			name text NOT NULL DEFAULT '',
			relay_pubkey text NOT NULL DEFAULT '',
			auth_tag text,
			owner_pubkey text,
			policy_version text,
			status text NOT NULL DEFAULT 'needs_access',
			last_error text,
			last_connected_at datetime,
			last_event_at datetime,
			buzz_agent_communities uuid NOT NULL REFERENCES buzz_agents(id) ON DELETE CASCADE,
			UNIQUE (relay_url, buzz_agent_communities)
		)`,
		`CREATE TABLE IF NOT EXISTS buzz_bindings (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			chat_id uuid NOT NULL,
			channel_id text NOT NULL,
			channel_name text NOT NULL DEFAULT '',
			inbound_enabled bool NOT NULL DEFAULT true,
			allow_pubkeys json NOT NULL,
			deny_pubkeys json NOT NULL,
			answer_agents bool NOT NULL DEFAULT false,
			allow_unrestricted bool NOT NULL DEFAULT false,
			status text NOT NULL DEFAULT 'active',
			last_error text,
			last_activity_at datetime,
			buzz_community_bindings uuid NOT NULL REFERENCES buzz_communities(id) ON DELETE CASCADE,
			UNIQUE (channel_id, buzz_community_bindings)
		)`,
		`CREATE TABLE IF NOT EXISTS buzz_message_links (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			direction text NOT NULL,
			chat_message_id uuid,
			event_id text,
			thread_root text NOT NULL DEFAULT '',
			posted_event_ids json NOT NULL,
			author_pubkey text NOT NULL DEFAULT '',
			author_name text NOT NULL DEFAULT '',
			reply_to_link_id uuid,
			status text NOT NULL DEFAULT 'received',
			error text,
			attempts integer NOT NULL DEFAULT 0,
			agent_depth integer NOT NULL DEFAULT 0,
			buzz_binding_links uuid NOT NULL REFERENCES buzz_bindings(id) ON DELETE CASCADE,
			UNIQUE (event_id, buzz_binding_links)
		)`,
		`CREATE TABLE IF NOT EXISTS buzz_pending_posts (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			chat_id uuid NOT NULL,
			source text NOT NULL,
			expires_at datetime NOT NULL,
			buzz_binding_pending_posts uuid NOT NULL REFERENCES buzz_bindings(id) ON DELETE CASCADE
		)`,
	} {
		_, err := db.Exec(ddl)
		require.NoError(t, err)
	}
}

type buzzFixture struct {
	ds      *Datastore
	owner   uuid.UUID
	other   uuid.UUID
	persona uuid.UUID
	chat    uuid.UUID
}

func newBuzzFixture(t *testing.T) *buzzFixture {
	t.Helper()
	ds, cleanup := newTestDatastore(t, createMemoryImportTestSchema, alterChatsTableForAgentJobTests)
	t.Cleanup(cleanup)
	f := &buzzFixture{ds: ds, owner: uuid.New(), other: uuid.New(), persona: uuid.New(), chat: uuid.New()}
	createTestUser(t, ds, f.owner)
	createTestUser(t, ds, f.other)
	createTestPersonality(t, ds, f.persona, f.owner)
	createTestChat(t, ds, f.chat, f.owner)
	return f
}

const testBuzzPubKey = "7e7e9c42a91bfef19fa929e5fda1b72e0ebc1a4c1141673e2794234d86addf4e"

func (f *buzzFixture) agent(t *testing.T) *models.BuzzAgent {
	t.Helper()
	a, err := f.ds.CreateBuzzAgent(context.Background(), f.owner, models.BuzzAgentCreate{
		PersonalityID: f.persona, PubKey: testBuzzPubKey, SecretKey: "secret-hex",
	})
	require.NoError(t, err)
	return a
}

func (f *buzzFixture) community(t *testing.T, agentID uuid.UUID) *models.BuzzCommunity {
	t.Helper()
	tag := `["auth","owner","","sig"]`
	owner := "ownerpub"
	c, err := f.ds.CreateBuzzCommunity(context.Background(), f.owner, agentID, models.BuzzCommunityCreate{
		RelayURL: "wss://acme.example", Name: "Acme", AuthTag: &tag, OwnerPubKey: &owner, Status: models.BuzzCommunityActive,
	})
	require.NoError(t, err)
	return c
}

func (f *buzzFixture) binding(t *testing.T, communityID uuid.UUID, channel string) *models.BuzzBinding {
	t.Helper()
	b, err := f.ds.CreateBuzzBinding(context.Background(), f.owner, communityID, models.BuzzBindingCreate{
		ChatID: f.chat, ChannelID: channel, ChannelName: "general", InboundEnabled: true,
	})
	require.NoError(t, err)
	return b
}

func TestBuzzKeysAndAttestationsAreEncryptedAndOwnerScoped(t *testing.T) {
	f := newBuzzFixture(t)
	ctx := context.Background()
	a := f.agent(t)
	back, err := nostr.DecodeNPub(a.NPub)
	require.NoError(t, err)
	assert.Equal(t, testBuzzPubKey, back, "the npub encodes the pubkey")

	raw, err := f.ds.dbClient.BuzzAgent.Get(ctx, a.ID)
	require.NoError(t, err)
	assert.NotEqual(t, "secret-hex", raw.SecretKey, "stored encrypted")
	creds, err := f.ds.GetBuzzAgentCredentials(ctx, f.owner, a.ID)
	require.NoError(t, err)
	assert.Equal(t, "secret-hex", creds.SecretKey)
	_, err = f.ds.GetBuzzAgentCredentials(ctx, f.other, a.ID)
	assert.ErrorIs(t, err, ErrBuzzAgentNotFound)

	_, err = f.ds.CreateBuzzAgent(ctx, f.owner, models.BuzzAgentCreate{PersonalityID: f.persona, PubKey: "x", SecretKey: "y"})
	assert.ErrorIs(t, err, ErrBuzzAgentExists, "one identity per persona")

	c := f.community(t, a.ID)
	assert.True(t, c.Attested)
	rawC, err := f.ds.dbClient.BuzzCommunity.Get(ctx, c.ID)
	require.NoError(t, err)
	assert.NotContains(t, *rawC.AuthTag, "auth", "attestation stored encrypted")
	conn, err := f.ds.GetBuzzConnection(ctx, f.owner, c.ID)
	require.NoError(t, err)
	assert.Equal(t, `["auth","owner","","sig"]`, conn.AuthTag)
	_, err = f.ds.GetBuzzConnection(ctx, f.other, c.ID)
	assert.ErrorIs(t, err, ErrBuzzCommunityNotFound)
	_, err = f.ds.CreateBuzzCommunity(ctx, f.owner, a.ID, models.BuzzCommunityCreate{RelayURL: "wss://acme.example", Status: models.BuzzCommunityActive})
	assert.ErrorIs(t, err, ErrBuzzCommunityExists)

	clear := ""
	updated, err := f.ds.UpdateBuzzCommunity(ctx, f.owner, c.ID, models.BuzzCommunityPatch{AuthTag: &clear, OwnerPubKey: &clear})
	require.NoError(t, err)
	assert.False(t, updated.Attested)
	assert.Nil(t, updated.OwnerPubKey)
}

func TestBuzzActiveConnectionsListBoundChannels(t *testing.T) {
	f := newBuzzFixture(t)
	ctx := context.Background()
	a := f.agent(t)
	c := f.community(t, a.ID)
	b := f.binding(t, c.ID, "chan-1")
	off := false
	quiet := f.binding(t, c.ID, "chan-2")
	_, err := f.ds.UpdateBuzzBinding(ctx, f.owner, quiet.ID, models.BuzzBindingPatch{InboundEnabled: &off})
	require.NoError(t, err)

	conns, err := f.ds.ListActiveBuzzConnections(ctx)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.Equal(t, "secret-hex", conns[0].Agent.SecretKey)
	assert.Equal(t, f.owner, conns[0].Agent.OwnerID)
	require.Len(t, conns[0].Channels, 1, "only bindings with inbound on are listened to")
	assert.Equal(t, b.ID, conns[0].Channels[0].BindingID)
	assert.Nil(t, conns[0].Community.LastEventAt, "no resume point yet")
	assert.Equal(t, models.BuzzAgentActive, conns[0].Agent.Status)

	at := time.Now().Add(-5 * time.Minute).Truncate(time.Second)
	require.NoError(t, f.ds.MarkBuzzCommunityEvent(ctx, c.ID, at))
	require.NoError(t, f.ds.MarkBuzzCommunityEvent(ctx, c.ID, at.Add(-time.Hour)))
	conns, err = f.ds.ListActiveBuzzConnections(ctx)
	require.NoError(t, err)
	require.NotNil(t, conns[0].Community.LastEventAt)
	assert.True(t, at.Equal(*conns[0].Community.LastEventAt), "the resume point only moves forward")

	disabled := models.BuzzCommunityDisabled
	_, err = f.ds.UpdateBuzzCommunity(ctx, f.owner, c.ID, models.BuzzCommunityPatch{Status: &disabled})
	require.NoError(t, err)
	conns, err = f.ds.ListActiveBuzzConnections(ctx)
	require.NoError(t, err)
	assert.Empty(t, conns, "a paused community is not connected")
	require.NoError(t, f.ds.SetBuzzCommunityState(ctx, c.ID, models.BuzzCommunityActive, ""))
	still, err := f.ds.GetBuzzCommunity(ctx, f.owner, c.ID)
	require.NoError(t, err)
	assert.Equal(t, models.BuzzCommunityDisabled, still.Status, "the gateway cannot resume a paused community")

	active := models.BuzzCommunityActive
	_, err = f.ds.UpdateBuzzCommunity(ctx, f.owner, c.ID, models.BuzzCommunityPatch{Status: &active})
	require.NoError(t, err)
	_, err = f.ds.SetBuzzAgentStatus(ctx, f.owner, a.ID, models.BuzzAgentDisabled)
	require.NoError(t, err)
	conns, err = f.ds.ListActiveBuzzConnections(ctx)
	require.NoError(t, err)
	assert.Empty(t, conns, "a paused identity is not connected")
}

func TestBuzzLinksDedupeAndThreadsContinue(t *testing.T) {
	f := newBuzzFixture(t)
	ctx := context.Background()
	a := f.agent(t)
	c := f.community(t, a.ID)
	b := f.binding(t, c.ID, "chan-1")

	target, err := f.ds.FindBuzzBindingTarget(ctx, c.ID, "chan-1")
	require.NoError(t, err)
	assert.Equal(t, b.ID, target.Binding.ID)
	assert.Equal(t, `["auth","owner","","sig"]`, target.AuthTag)
	_, err = f.ds.FindBuzzBindingTarget(ctx, c.ID, "other")
	assert.ErrorIs(t, err, ErrBuzzBindingNotFound)

	in := models.BuzzInbound{BindingID: b.ID, EventID: "ev1", ThreadRoot: "ev1", AuthorPubKey: "alice", AuthorName: "Alice"}
	link, created, err := f.ds.RecordInboundBuzzMessage(ctx, in)
	require.NoError(t, err)
	require.True(t, created)
	_, created, err = f.ds.RecordInboundBuzzMessage(ctx, in)
	require.NoError(t, err)
	assert.False(t, created, "a redelivered event is recorded once")

	// While the turn starts, the speaker comes from the link without a message.
	name, err := f.ds.ExternalSpeakerForTurn(ctx, f.owner, f.chat, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, "Alice", name)

	userMsg := uuid.New()
	require.NoError(t, f.ds.AttachBuzzLinkMessage(ctx, link.ID, userMsg))
	found, err := f.ds.FindInboundBuzzLink(ctx, userMsg)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "ev1", *found.EventID)
	name, err = f.ds.ExternalSpeakerForMessage(ctx, f.owner, userMsg)
	require.NoError(t, err)
	assert.Equal(t, "Alice", name)

	reply := uuid.New()
	out, created, err := f.ds.StartOutboundBuzzLink(ctx, b.ID, reply, "ev1", &link.ID)
	require.NoError(t, err)
	require.True(t, created)
	_, created, err = f.ds.StartOutboundBuzzLink(ctx, b.ID, reply, "ev1", &link.ID)
	require.NoError(t, err)
	assert.False(t, created, "a retried hook finds the link")
	require.NoError(t, f.ds.FinishOutboundBuzzLink(ctx, out.ID, []string{"reply-ev"}, nil))

	ok, err := f.ds.BuzzThreadContinues(ctx, b.ID, "ev1", "reply-ev", "bob")
	require.NoError(t, err)
	assert.True(t, ok, "anyone replying directly to the persona's message")
	ok, err = f.ds.BuzzThreadContinues(ctx, b.ID, "ev1", "ev1", "alice")
	require.NoError(t, err)
	assert.True(t, ok, "the asker writing on in their own thread")
	ok, err = f.ds.BuzzThreadContinues(ctx, b.ID, "ev1", "ev1", "bob")
	require.NoError(t, err)
	assert.False(t, ok, "someone else in the asker's thread, without a mention")
	ok, err = f.ds.BuzzThreadContinues(ctx, b.ID, "elsewhere", "elsewhere", "alice")
	require.NoError(t, err)
	assert.False(t, ok)

	links, err := f.ds.ListBuzzLinksForChat(ctx, f.owner, f.chat, 10)
	require.NoError(t, err)
	assert.Len(t, links, 2)
	links, err = f.ds.ListBuzzLinksForChat(ctx, f.other, f.chat, 10)
	require.NoError(t, err)
	assert.Empty(t, links)
}

func TestBuzzRelayThreadsAreExternal(t *testing.T) {
	f := newBuzzFixture(t)
	ctx := context.Background()
	ext, err := f.ds.IsExternalRelayChat(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.False(t, ext)
	platform, err := f.ds.RelayPlatformForChat(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Empty(t, platform)

	a := f.agent(t)
	c := f.community(t, a.ID)
	f.binding(t, c.ID, "chan-1")
	ext, err = f.ds.IsExternalRelayChat(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.True(t, ext)
	platform, err = f.ds.RelayPlatformForChat(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Equal(t, "Buzz", platform)
	ext, err = f.ds.IsExternalRelayChat(ctx, f.other, f.chat)
	require.NoError(t, err)
	assert.False(t, ext, "owner-scoped")
}

func TestBuzzPendingPostsAndCascade(t *testing.T) {
	f := newBuzzFixture(t)
	ctx := context.Background()
	a := f.agent(t)
	c := f.community(t, a.ID)
	b := f.binding(t, c.ID, "chan-1")

	require.NoError(t, f.ds.SetBuzzPendingPost(ctx, f.owner, b.ID, f.chat, models.BuzzPostFromComposer))
	assert.ErrorIs(t, f.ds.SetBuzzPendingPost(ctx, f.other, b.ID, f.chat, models.BuzzPostFromComposer), ErrBuzzBindingNotFound)

	// Nothing can be armed through a paused persona or community, or a broken binding.
	disabled, active := models.BuzzCommunityDisabled, models.BuzzCommunityActive
	_, err := f.ds.UpdateBuzzCommunity(ctx, f.owner, c.ID, models.BuzzCommunityPatch{Status: &disabled})
	require.NoError(t, err)
	assert.ErrorIs(t, f.ds.SetBuzzPendingPost(ctx, f.owner, b.ID, f.chat, models.BuzzPostFromComposer), ErrBuzzBindingPaused)
	_, err = f.ds.UpdateBuzzCommunity(ctx, f.owner, c.ID, models.BuzzCommunityPatch{Status: &active})
	require.NoError(t, err)
	_, err = f.ds.SetBuzzAgentStatus(ctx, f.owner, a.ID, models.BuzzAgentDisabled)
	require.NoError(t, err)
	assert.ErrorIs(t, f.ds.SetBuzzPendingPost(ctx, f.owner, b.ID, f.chat, models.BuzzPostFromComposer), ErrBuzzBindingPaused)
	_, err = f.ds.SetBuzzAgentStatus(ctx, f.owner, a.ID, models.BuzzAgentActive)
	require.NoError(t, err)
	require.NoError(t, f.ds.SetBuzzBindingStatus(ctx, b.ID, models.BuzzBindingBroken, "gone"))
	assert.ErrorIs(t, f.ds.SetBuzzPendingPost(ctx, f.owner, b.ID, f.chat, models.BuzzPostFromComposer), ErrBuzzBindingPaused)
	require.NoError(t, f.ds.SetBuzzBindingStatus(ctx, b.ID, models.BuzzBindingActive, ""))

	ours, err := f.ds.IsBuzzAgentPubKey(ctx, a.PubKey)
	require.NoError(t, err)
	assert.True(t, ours)
	ours, err = f.ds.IsBuzzAgentPubKey(ctx, "not-a-persona")
	require.NoError(t, err)
	assert.False(t, ours)
	pending, err := f.ds.ListBuzzPendingPosts(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{b.ID}, pending)
	got, err := f.ds.ConsumeBuzzPendingPosts(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{b.ID}, got)
	got, err = f.ds.ConsumeBuzzPendingPosts(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Empty(t, got, "consumed once")

	// An expired request is dropped.
	require.NoError(t, f.ds.dbClient.BuzzPendingPost.Create().SetBindingID(b.ID).SetChatID(f.chat).
		SetSource("tool").SetExpiresAt(time.Now().Add(-time.Minute)).Exec(ctx))
	got, err = f.ds.ConsumeBuzzPendingPosts(ctx, f.owner, f.chat)
	require.NoError(t, err)
	assert.Empty(t, got)

	forPersona, err := f.ds.ListBuzzBindingsForPersonality(ctx, f.owner, f.persona)
	require.NoError(t, err)
	assert.Len(t, forPersona, 1)

	require.NoError(t, f.ds.DeleteBuzzAgent(ctx, f.owner, a.ID))
	bindings, err := f.ds.ListBuzzBindings(ctx, f.owner)
	require.NoError(t, err)
	assert.Empty(t, bindings, "removing the identity removes its bindings")
}

func TestBuzzBatchLinksLoopDepthAndRenames(t *testing.T) {
	f := newBuzzFixture(t)
	ctx := context.Background()
	a := f.agent(t)
	c := f.community(t, a.ID)
	b := f.binding(t, c.ID, "chan-1")
	start := time.Now().Add(-time.Minute)

	msgID := uuid.New()
	for _, in := range []models.BuzzInbound{
		{BindingID: b.ID, EventID: "ev1", ThreadRoot: "ev1", AuthorPubKey: "alice", AuthorName: "Alice"},
		{BindingID: b.ID, EventID: "ev2", ThreadRoot: "ev2", AuthorPubKey: "nova", AuthorName: "Nova", AgentDepth: 1},
	} {
		link, created, err := f.ds.RecordInboundBuzzMessage(ctx, in)
		require.NoError(t, err)
		require.True(t, created)
		require.NoError(t, f.ds.AttachBuzzLinkMessage(ctx, link.ID, msgID))
		time.Sleep(2 * time.Millisecond)
	}
	found, err := f.ds.FindInboundBuzzLink(ctx, msgID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "ev2", *found.EventID, "the reply answers the newest event")
	speaker, err := f.ds.ExternalSpeakerForMessage(ctx, f.owner, msgID)
	require.NoError(t, err)
	assert.Equal(t, "Alice, Nova", speaker)

	last, err := f.ds.LastInboundBuzzLink(ctx, b.ID, start)
	require.NoError(t, err)
	require.NotNil(t, last)
	assert.Equal(t, 1, last.AgentDepth)

	require.NoError(t, f.ds.RenameBuzzChannel(ctx, b.ID, "#lounge"))
	got, err := f.ds.GetBuzzBinding(ctx, f.owner, b.ID)
	require.NoError(t, err)
	assert.Equal(t, "lounge", got.ChannelName)
}
