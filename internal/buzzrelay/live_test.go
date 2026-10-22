package buzzrelay

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"go.uber.org/zap"
)

// Live tests against a real Buzz relay. They are skipped unless BUZZ_LIVE_RELAY names one
// (http://localhost:3000 for the local rig) and BUZZ_LIVE_OWNER_SECRET is the hex secret key
// of a relay owner or admin there. They check the wire format against Buzz itself:
//
//	BUZZ_LIVE_RELAY=http://localhost:3000 BUZZ_LIVE_OWNER_SECRET=<hex> go test -run Live ./internal/buzzrelay/
func liveRig(t *testing.T) (Endpoint, *Client, Signer) {
	t.Helper()
	raw, secret := os.Getenv("BUZZ_LIVE_RELAY"), os.Getenv("BUZZ_LIVE_OWNER_SECRET")
	if raw == "" || secret == "" {
		t.Skip("BUZZ_LIVE_RELAY and BUZZ_LIVE_OWNER_SECRET not set")
	}
	ep, _, err := ParseEndpoint(raw, true)
	require.NoError(t, err)
	owner, err := nostr.KeysFromHex(secret)
	require.NoError(t, err)
	return ep, NewClient(true), Signer{Keys: owner}
}

// liveChannel creates a channel as the owner and adds members to it.
func liveChannel(t *testing.T, ctx context.Context, c *Client, ep Endpoint, owner Signer, members ...string) string {
	t.Helper()
	channel := uuid.NewString()
	create := &nostr.Event{Kind: KindCreateGroup, Tags: nostr.Tags{{"h", channel}, {"name", "whatiff-live-" + channel[:8]}, {"visibility", "open"}}}
	require.NoError(t, sign(owner, create))
	require.NoError(t, c.Publish(ctx, ep, owner, create))
	for _, m := range members {
		add := &nostr.Event{Kind: KindPutUser, Tags: nostr.Tags{{"h", channel}, {"p", m}}}
		require.NoError(t, sign(owner, add))
		require.NoError(t, c.Publish(ctx, ep, owner, add))
	}
	return channel
}

func TestLiveInfoAndQuery(t *testing.T) {
	ep, c, owner := liveRig(t)
	ctx := context.Background()
	info, err := c.Info(ctx, ep)
	require.NoError(t, err)
	assert.True(t, info.IsBuzz(), "software = %q", info.Software)
	assert.Len(t, info.Self, 64)

	channel := liveChannel(t, ctx, c, ep, owner)
	meta, err := c.Query(ctx, ep, owner, Filter{"kinds": []int{KindGroupMetadata}, "#d": []string{channel}})
	require.NoError(t, err)
	require.NotEmpty(t, meta, "the relay publishes the channel's metadata")
	assert.NoError(t, meta[0].Verify())
	assert.Equal(t, info.Self, meta[0].PubKey, "channel state is relay-signed")
}

func TestLiveStrangerIsRefusedAndAttestationAdmits(t *testing.T) {
	ep, c, owner := liveRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	agent, _ := nostr.GenerateKeys()

	states := make(chan SessionState, 8)
	s := c.Open(ctx, SessionConfig{Endpoint: ep, Signer: Signer{Keys: agent}, OnState: func(st SessionState, detail string) {
		t.Logf("stranger: %s %s", st, detail)
		states <- st
	}})
	select {
	case st := <-states:
		assert.Equal(t, SessionNeedsAccess, st, "a key that is not a member is refused")
	case <-ctx.Done():
		t.Fatal("no state")
	}
	_ = s.Close()

	att, err := nostr.Attest(owner.Keys, agent.PublicKey(), "")
	require.NoError(t, err)
	attested := Signer{Keys: agent, Attestation: att}
	states2 := make(chan SessionState, 8)
	s2 := c.Open(ctx, SessionConfig{Endpoint: ep, Signer: attested, OnState: func(st SessionState, detail string) {
		t.Logf("attested: %s %s", st, detail)
		states2 <- st
	}})
	defer s2.Close()
	select {
	case st := <-states2:
		assert.Equal(t, SessionActive, st, "the owner's attestation admits the agent (NIP-AA)")
	case <-ctx.Done():
		t.Fatal("no state")
	}
	// It can publish its profile.
	prof, err := ProfileEvent(attested, Profile{Name: "live-agent", Bot: true})
	require.NoError(t, err)
	assert.NoError(t, c.Publish(ctx, ep, attested, prof))
}

func TestLiveInviteClaim(t *testing.T) {
	ep, c, owner := liveRig(t)
	ctx := context.Background()
	body, _ := json.Marshal(map[string]any{"ttl_secs": 3600, "max_uses": 1})
	req, err := c.signedPost(ctx, ep, owner, "/api/invites", body)
	require.NoError(t, err)
	var minted struct {
		Code string `json:"code"`
		URL  string `json:"url"`
	}
	require.NoError(t, c.do(req, &minted))
	require.NotEmpty(t, minted.Code)

	parsed, code, err := ParseEndpoint(minted.URL, true)
	require.NoError(t, err)
	assert.Equal(t, ep, parsed)
	assert.Equal(t, minted.Code, code)

	policy, err := c.JoinPolicy(ctx, ep)
	require.NoError(t, err)
	version := ""
	if policy != nil {
		version = policy.Version
	}
	agent, _ := nostr.GenerateKeys()
	already, err := c.ClaimInvite(ctx, ep, Signer{Keys: agent}, code, version)
	require.NoError(t, err)
	assert.False(t, already)
	already, err = c.ClaimInvite(ctx, ep, Signer{Keys: agent}, code, version)
	if err == nil {
		assert.True(t, already, "claiming again reports already a member")
	}
}

func TestLiveMentionArrivesAndReplyThreads(t *testing.T) {
	ep, c, owner := liveRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	agentKeys, _ := nostr.GenerateKeys()
	att, _ := nostr.Attest(owner.Keys, agentKeys.PublicKey(), "")
	agent := Signer{Keys: agentKeys, Attestation: att}
	channel := liveChannel(t, ctx, c, ep, owner, agentKeys.PublicKey())

	var mu sync.Mutex
	got := make(chan nostr.Event, 4)
	active := make(chan struct{}, 1)
	s := c.Open(ctx, SessionConfig{
		Endpoint: ep, Signer: agent, Logger: zap.NewNop(),
		Subs: map[string]Filter{"ch": {"kinds": []int{KindStreamMessage}, "#h": []string{channel}}},
		OnEvent: func(sub string, ev nostr.Event) {
			mu.Lock()
			defer mu.Unlock()
			if sub == "ch" {
				got <- ev
			}
		},
		OnState: func(st SessionState, detail string) {
			if st == SessionActive {
				active <- struct{}{}
			} else {
				t.Logf("agent: %s %s", st, detail)
			}
		},
		OnClosed: func(sub, reason string) { t.Logf("closed %s: %s", sub, reason) },
	})
	defer s.Close()
	select {
	case <-active:
	case <-ctx.Done():
		t.Fatal("agent never connected")
	}
	time.Sleep(300 * time.Millisecond) // let the REQ land

	ask, err := MessageEvent(owner, channel, "@live-agent what's up?", nil, []string{agentKeys.PublicKey()}, nil)
	require.NoError(t, err)
	require.NoError(t, c.Publish(ctx, ep, owner, ask))

	var seen nostr.Event
	select {
	case seen = <-got:
	case <-ctx.Done():
		t.Fatal("the mention never arrived")
	}
	assert.Equal(t, ask.ID, seen.ID)
	assert.True(t, seen.Tags.Has("p", agentKeys.PublicKey()))

	typing, err := TypingEvent(agent, channel, &ThreadRef{Root: ask.ID, Parent: ask.ID})
	require.NoError(t, err)
	assert.NoError(t, c.Publish(ctx, ep, agent, typing), "typing pulses are accepted over HTTP")

	thread := ReplyThread(seen)
	reply, err := MessageEvent(agent, channel, "**Not much!** Markdown works.", &thread, []string{owner.Keys.PublicKey()}, nil)
	require.NoError(t, err)
	require.NoError(t, c.Publish(ctx, ep, agent, reply))

	// The reply arrives on the agent's own subscription too, and is in the asker's thread.
	select {
	case echo := <-got:
		assert.Equal(t, reply.ID, echo.ID)
		assert.Equal(t, ThreadRef{Root: ask.ID, Parent: ask.ID}, ThreadOf(echo))
		assert.Equal(t, owner.Keys.PublicKey(), echo.Tags.Value("auth"), "the attestation rides on the event")
	case <-ctx.Done():
		t.Fatal("the reply was not delivered")
	}

	// A follow-up in the thread from the owner, without a p tag, still names the agent's
	// message as its parent.
	follow, err := MessageEvent(owner, channel, "and then?", &ThreadRef{Root: ask.ID, Parent: reply.ID}, nil, nil)
	require.NoError(t, err)
	require.NoError(t, c.Publish(ctx, ep, owner, follow))
	select {
	case ev := <-got:
		assert.Equal(t, ThreadRef{Root: ask.ID, Parent: reply.ID}, ThreadOf(ev))
	case <-ctx.Done():
		t.Fatal("the follow-up was not delivered")
	}
}
