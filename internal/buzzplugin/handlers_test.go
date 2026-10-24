package buzzplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeStore implements the routes' Store for one user. It embeds the interface, so a route
// calling a method the test did not expect panics instead of passing quietly.
type fakeStore struct {
	Store
	owner     uuid.UUID
	agents    map[uuid.UUID]*models.BuzzAgentCredentials
	comms     map[uuid.UUID]*models.BuzzConnection
	bindings  map[uuid.UUID]*models.BuzzBinding
	chats     map[uuid.UUID]*models.Chat
	persona   *models.Personality
	createdCh []models.Chat
}

func newFakeStore(owner uuid.UUID) *fakeStore {
	return &fakeStore{owner: owner, agents: map[uuid.UUID]*models.BuzzAgentCredentials{}, comms: map[uuid.UUID]*models.BuzzConnection{},
		bindings: map[uuid.UUID]*models.BuzzBinding{}, chats: map[uuid.UUID]*models.Chat{}, persona: &models.Personality{ID: uuid.New(), Name: "Vix"}}
}

func (f *fakeStore) mine(user uuid.UUID) bool { return user == f.owner }

func (f *fakeStore) CreateBuzzAgent(_ context.Context, user uuid.UUID, in models.BuzzAgentCreate) (*models.BuzzAgent, error) {
	for _, a := range f.agents {
		if a.PersonalityID == in.PersonalityID {
			return nil, datastore.ErrBuzzAgentExists
		}
	}
	id := uuid.New()
	f.agents[id] = &models.BuzzAgentCredentials{ID: id, OwnerID: user, PersonalityID: in.PersonalityID, PubKey: in.PubKey, SecretKey: in.SecretKey}
	return &models.BuzzAgent{ID: id, PersonalityID: in.PersonalityID, PubKey: in.PubKey, Status: models.BuzzAgentActive}, nil
}

func (f *fakeStore) GetBuzzAgentCredentials(_ context.Context, user, id uuid.UUID) (*models.BuzzAgentCredentials, error) {
	a, ok := f.agents[id]
	if !ok || !f.mine(user) {
		return nil, datastore.ErrBuzzAgentNotFound
	}
	return a, nil
}

func (f *fakeStore) CreateBuzzCommunity(_ context.Context, user, agentID uuid.UUID, in models.BuzzCommunityCreate) (*models.BuzzCommunity, error) {
	a := f.agents[agentID]
	id := uuid.New()
	c := models.BuzzCommunity{ID: id, AgentID: agentID, RelayURL: in.RelayURL, Name: in.Name, Attested: in.AuthTag != nil, OwnerPubKey: in.OwnerPubKey, PolicyVersion: in.PolicyVersion, Status: in.Status}
	conn := &models.BuzzConnection{Agent: *a, Community: c}
	if in.AuthTag != nil {
		conn.AuthTag = *in.AuthTag
	}
	f.comms[id] = conn
	return &c, nil
}

func (f *fakeStore) GetBuzzConnection(_ context.Context, user, id uuid.UUID) (*models.BuzzConnection, error) {
	c, ok := f.comms[id]
	if !ok || !f.mine(user) {
		return nil, datastore.ErrBuzzCommunityNotFound
	}
	return c, nil
}

func (f *fakeStore) GetBuzzCommunity(_ context.Context, user, id uuid.UUID) (*models.BuzzCommunity, error) {
	c, err := f.GetBuzzConnection(context.Background(), user, id)
	if err != nil {
		return nil, err
	}
	return &c.Community, nil
}

func (f *fakeStore) UpdateBuzzCommunity(_ context.Context, user, id uuid.UUID, p models.BuzzCommunityPatch) (*models.BuzzCommunity, error) {
	c, ok := f.comms[id]
	if !ok || !f.mine(user) {
		return nil, datastore.ErrBuzzCommunityNotFound
	}
	if p.Status != nil {
		c.Community.Status = *p.Status
	}
	if p.LastError != nil {
		c.Community.LastError = p.LastError
	}
	if p.PolicyVersion != nil {
		c.Community.PolicyVersion = p.PolicyVersion
	}
	return &c.Community, nil
}

func (f *fakeStore) GetPersonality(context.Context, uuid.UUID, uuid.UUID) (*models.Personality, error) {
	return f.persona, nil
}

func (f *fakeStore) GetChat(_ context.Context, user, id uuid.UUID) (*models.Chat, error) {
	c, ok := f.chats[id]
	if !ok || !f.mine(user) {
		return nil, datastore.ErrChatNotFound
	}
	return c, nil
}

func (f *fakeStore) CreateChat(_ context.Context, _ uuid.UUID, c models.Chat) (*models.Chat, error) {
	c.ID = uuid.New()
	f.chats[c.ID] = &c
	f.createdCh = append(f.createdCh, c)
	return &c, nil
}

func (f *fakeStore) CreateBuzzBinding(_ context.Context, _ uuid.UUID, communityID uuid.UUID, in models.BuzzBindingCreate) (*models.BuzzBinding, error) {
	b := &models.BuzzBinding{ID: uuid.New(), CommunityID: communityID, ChatID: in.ChatID, ChannelID: in.ChannelID, ChannelName: in.ChannelName,
		InboundEnabled: in.InboundEnabled, AllowPubKeys: in.AllowPubKeys, DenyPubKeys: in.DenyPubKeys, AllowUnrestricted: in.AllowUnrestricted, Status: models.BuzzBindingActive}
	f.bindings[b.ID] = b
	return b, nil
}

// fakeRelay answers like a Buzz community.
type fakeRelay struct {
	buzzrelay.Relay
	policy    *buzzrelay.JoinPolicy
	claimed   []string
	confirmed []bool
	member    bool
	published []nostr.Event
}

func (r *fakeRelay) Info(context.Context, buzzrelay.Endpoint) (*buzzrelay.RelayInfo, error) {
	return &buzzrelay.RelayInfo{Name: "Acme", Software: "https://github.com/block/buzz", Self: "relaypub"}, nil
}

func (r *fakeRelay) JoinPolicy(context.Context, buzzrelay.Endpoint) (*buzzrelay.JoinPolicy, error) {
	return r.policy, nil
}

func (r *fakeRelay) ClaimInvite(_ context.Context, _ buzzrelay.Endpoint, _ buzzrelay.Signer, code, version string, age bool) (bool, error) {
	r.claimed = append(r.claimed, code+"@"+version)
	r.confirmed = append(r.confirmed, age)
	r.member = true
	return false, nil
}

func (r *fakeRelay) Query(_ context.Context, _ buzzrelay.Endpoint, s buzzrelay.Signer, _ ...buzzrelay.Filter) ([]nostr.Event, error) {
	if !r.member && s.Attestation == nil {
		return nil, buzzrelay.ErrNoAccess
	}
	return nil, nil
}

func (r *fakeRelay) Publish(_ context.Context, _ buzzrelay.Endpoint, _ buzzrelay.Signer, ev *nostr.Event) error {
	r.published = append(r.published, *ev)
	return nil
}

type rig struct {
	t       *testing.T
	store   *fakeStore
	relay   *fakeRelay
	router  *mux.Router
	user    uuid.UUID
	kicks   int
	agentID uuid.UUID
	agent   *nostr.Keys
}

func newRig(t *testing.T) *rig {
	user := uuid.New()
	r := &rig{t: t, store: newFakeStore(user), relay: &fakeRelay{}, user: user}
	h := &Handler{Store: r.store, Relay: r.relay, Logger: zap.NewNop(), Changed: func() { r.kicks++ }}
	r.router = mux.NewRouter()
	h.Register(r.router.PathPrefix("/buzz").Subrouter())
	return r
}

func (r *rig) do(method, path string, body any) (int, map[string]any) {
	r.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, r.user))
	rec := httptest.NewRecorder()
	r.router.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (r *rig) createAgent() {
	r.t.Helper()
	code, out := r.do(http.MethodPost, "/buzz/agents", map[string]any{"personality_id": r.store.persona.ID})
	require.Equal(r.t, http.StatusCreated, code, out)
	r.agentID = uuid.MustParse(out["id"].(string))
	creds := r.store.agents[r.agentID]
	r.agent, _ = nostr.KeysFromHex(creds.SecretKey)
	assert.Equal(r.t, r.agent.PublicKey(), out["pubkey"])
	assert.NotContains(r.t, out, "secret_key")
	assert.Equal(r.t, "Vix", out["persona_name"])
}

func TestCreateAgentGeneratesAKeyOncePerPersona(t *testing.T) {
	r := newRig(t)
	r.createAgent()
	code, _ := r.do(http.MethodPost, "/buzz/agents", map[string]any{"personality_id": r.store.persona.ID})
	assert.Equal(t, http.StatusConflict, code)
}

func TestAddCommunityNeedsTermsBeforeClaimingAnInvite(t *testing.T) {
	r := newRig(t)
	r.createAgent()
	r.relay.policy = &buzzrelay.JoinPolicy{TermsMarkdown: "# Be nice", Version: "v2", AgeAttestationRequired: true}
	path := "/buzz/agents/" + r.agentID.String() + "/communities"

	code, out := r.do(http.MethodPost, path, map[string]any{"relay_url": "https://acme.example/invite/v2.code"})
	require.Equal(t, http.StatusConflict, code)
	assert.Equal(t, models.ErrCodeBuzzTermsRequired, out["code"])
	assert.Equal(t, "v2", out["details"].(map[string]any)["policy"].(map[string]any)["version"])
	assert.Empty(t, r.relay.claimed, "nothing is claimed before the terms are accepted")

	code, _ = r.do(http.MethodPost, path, map[string]any{"relay_url": "https://acme.example/invite/v2.code", "accept_terms_version": "v2"})
	assert.Equal(t, http.StatusConflict, code, "the age requirement must be confirmed too")

	code, out = r.do(http.MethodPost, path, map[string]any{"relay_url": "https://acme.example/invite/v2.code", "accept_terms_version": "v2", "confirm_age": true})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, []string{"v2.code@v2"}, r.relay.claimed)
	assert.Equal(t, []bool{true}, r.relay.confirmed)
	assert.Equal(t, "active", out["status"], "a claimed invite lets the persona in")
	assert.Equal(t, "wss://acme.example", out["relay_url"])
	assert.Equal(t, 1, r.kicks, "the gateway is told")
	require.NotEmpty(t, r.relay.published)
	profile := buzzrelay.ParseProfile(r.relay.published[0])
	assert.Equal(t, "Vix", profile.Name)
	assert.True(t, profile.Bot)
}

func TestAddCommunityWithoutAccessNeedsAccess(t *testing.T) {
	r := newRig(t)
	r.createAgent()
	code, out := r.do(http.MethodPost, "/buzz/agents/"+r.agentID.String()+"/communities", map[string]any{"relay_url": "acme.example"})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, "needs_access", out["status"])
	assert.Contains(t, out["last_error"], "npub1", "the owner is told which key to have added")

	code, _ = r.do(http.MethodPost, "/buzz/agents/"+r.agentID.String()+"/communities", map[string]any{"relay_url": "http://localhost:3000"})
	assert.Equal(t, http.StatusBadRequest, code, "plain http is refused outside development")
}

func TestAttestationsMustBeSignedForThePersona(t *testing.T) {
	r := newRig(t)
	r.createAgent()
	owner, _ := nostr.GenerateKeys()
	other, _ := nostr.GenerateKeys()
	path := "/buzz/agents/" + r.agentID.String() + "/communities"

	wrong, _ := nostr.Attest(owner, other.PublicKey(), "")
	code, out := r.do(http.MethodPost, path, map[string]any{"relay_url": "acme.example", "attestation": wrong.JSON()})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, out["message"], "not signed for this persona")

	right, _ := nostr.Attest(owner, r.agent.PublicKey(), "")
	code, out = r.do(http.MethodPost, path, map[string]any{"relay_url": "acme.example", "attestation": right.JSON()})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, true, out["attested"])
	assert.Equal(t, owner.PublicKey(), out["owner_pubkey"])
	assert.Equal(t, "active", out["status"], "the owner's attestation admits the persona")
}

func TestBindingCreatesASandboxedThreadAndRefusesAnOpenOne(t *testing.T) {
	r := newRig(t)
	r.createAgent()
	r.relay.member = true
	_, comm := r.do(http.MethodPost, "/buzz/agents/"+r.agentID.String()+"/communities", map[string]any{"relay_url": "acme.example"})
	communityID := comm["id"]

	code, out := r.do(http.MethodPost, "/buzz/bindings", map[string]any{"community_id": communityID, "channel_id": "c1", "channel_name": "#general",
		"allow_pubkeys": []string{r.agent.NPub()}})
	require.Equal(t, http.StatusCreated, code, out)
	require.Len(t, r.store.createdCh, 1)
	assert.Equal(t, models.ContextScopeSandbox, r.store.createdCh[0].ContextScope)
	assert.Equal(t, "Buzz · #general", r.store.createdCh[0].Name)
	assert.Equal(t, r.store.persona.ID, r.store.createdCh[0].PersonalityID)
	assert.Equal(t, []any{r.agent.PublicKey()}, out["allow_pubkeys"], "npubs are stored as hex")
	assert.Equal(t, true, out["chat_sandboxed"])

	open := uuid.New()
	r.store.chats[open] = &models.Chat{ID: open, ContextScope: models.ContextScopeAccount}
	code, _ = r.do(http.MethodPost, "/buzz/bindings", map[string]any{"community_id": communityID, "channel_id": "c2", "chat_id": open})
	assert.Equal(t, http.StatusBadRequest, code, "an unsandboxed thread needs the acknowledgement")
	code, out = r.do(http.MethodPost, "/buzz/bindings", map[string]any{"community_id": communityID, "channel_id": "c2", "chat_id": open, "allow_unrestricted": true})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, true, out["allow_unrestricted"])

	code, _ = r.do(http.MethodPost, "/buzz/bindings", map[string]any{"community_id": communityID, "channel_id": "c3", "deny_pubkeys": []string{"not-a-key"}})
	assert.Equal(t, http.StatusBadRequest, code, "a bad key is refused, not dropped")
}

func TestAnotherAccountsRowsAreNotFound(t *testing.T) {
	r := newRig(t)
	r.createAgent()
	r.user = uuid.New()
	code, _ := r.do(http.MethodPost, "/buzz/agents/"+r.agentID.String()+"/communities", map[string]any{"relay_url": "acme.example"})
	assert.Equal(t, http.StatusNotFound, code)
}
