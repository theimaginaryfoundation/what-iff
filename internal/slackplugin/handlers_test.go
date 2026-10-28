package slackplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/featuregate"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/slackrelay"
	"go.uber.org/zap"
)

// fakeStore implements the routes' Store for one user. It embeds the interface, so a route
// calling a method the test did not expect panics instead of passing quietly.
type fakeStore struct {
	Store
	owner     uuid.UUID
	apps      map[uuid.UUID]*models.SlackAppCredentials
	status    map[uuid.UUID]models.SlackAppStatus
	bindings  map[uuid.UUID]*models.SlackBinding
	chats     map[uuid.UUID]*models.Chat
	persona   *models.Personality
	createdCh []models.Chat
}

func newFakeStore(owner uuid.UUID) *fakeStore {
	return &fakeStore{owner: owner, apps: map[uuid.UUID]*models.SlackAppCredentials{}, status: map[uuid.UUID]models.SlackAppStatus{},
		bindings: map[uuid.UUID]*models.SlackBinding{}, chats: map[uuid.UUID]*models.Chat{},
		persona: &models.Personality{ID: uuid.New(), Name: "Vix"}}
}

func (f *fakeStore) mine(user uuid.UUID) bool { return user == f.owner }

func (f *fakeStore) model(id uuid.UUID) *models.SlackApp {
	c := f.apps[id]
	return &models.SlackApp{ID: id, PersonalityID: c.PersonalityID, TeamID: c.TeamID, AppID: c.AppID, BotUserID: c.BotUserID, Status: f.status[id]}
}

func (f *fakeStore) CreateSlackApp(_ context.Context, user uuid.UUID, in models.SlackAppCreate) (*models.SlackApp, error) {
	for _, a := range f.apps {
		if a.AppID == in.AppID {
			return nil, datastore.ErrSlackAppExists
		}
	}
	id := uuid.New()
	f.apps[id] = &models.SlackAppCredentials{ID: id, OwnerID: user, PersonalityID: in.PersonalityID, TeamID: in.TeamID, AppID: in.AppID,
		BotUserID: in.BotUserID, BotID: in.BotID, BotToken: in.BotToken, AppToken: in.AppToken}
	f.status[id] = models.SlackAppActive
	return f.model(id), nil
}

func (f *fakeStore) GetSlackApp(_ context.Context, user, id uuid.UUID) (*models.SlackApp, error) {
	if _, ok := f.apps[id]; !ok || !f.mine(user) {
		return nil, datastore.ErrSlackAppNotFound
	}
	return f.model(id), nil
}

func (f *fakeStore) GetSlackAppCredentials(_ context.Context, user, id uuid.UUID) (*models.SlackAppCredentials, error) {
	a, ok := f.apps[id]
	if !ok || !f.mine(user) {
		return nil, datastore.ErrSlackAppNotFound
	}
	return a, nil
}

func (f *fakeStore) UpdateSlackApp(_ context.Context, user, id uuid.UUID, p models.SlackAppPatch) (*models.SlackApp, error) {
	a, ok := f.apps[id]
	if !ok || !f.mine(user) {
		return nil, datastore.ErrSlackAppNotFound
	}
	if p.BotToken != nil {
		a.BotToken = *p.BotToken
	}
	if p.AppToken != nil {
		a.AppToken = *p.AppToken
	}
	if p.Status != nil {
		f.status[id] = *p.Status
	}
	return f.model(id), nil
}

func (f *fakeStore) GetPersonality(_ context.Context, user, _ uuid.UUID) (*models.Personality, error) {
	if !f.mine(user) {
		return nil, datastore.ErrPersonalityNotFound
	}
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

func (f *fakeStore) CreateSlackBinding(_ context.Context, _ uuid.UUID, appID uuid.UUID, in models.SlackBindingCreate) (*models.SlackBinding, error) {
	b := &models.SlackBinding{ID: uuid.New(), AppID: appID, ChatID: in.ChatID, ChannelID: in.ChannelID, ChannelName: in.ChannelName,
		IsPrivate: in.IsPrivate, InboundEnabled: in.InboundEnabled, AllowUserIDs: in.AllowUserIDs, DenyUserIDs: in.DenyUserIDs,
		AllowUnrestricted: in.AllowUnrestricted, Status: models.SlackBindingActive}
	f.bindings[b.ID] = b
	return b, nil
}

// fakeSlack answers like Slack for one app, "AVIX" in workspace "T1".
type fakeSlack struct {
	Slack
	botUser string
	appID   string
	joined  []string
}

func (s *fakeSlack) AuthTest(_ context.Context, botToken string) (*slackrelay.Identity, error) {
	if botToken == "xoxb-revoked" {
		return nil, &slackrelay.Error{Method: "auth.test", Code: "invalid_auth"}
	}
	return &slackrelay.Identity{TeamID: "T1", Team: "Acme", URL: "https://acme.slack.com/", UserID: s.botUser, User: "vix", BotID: "BVIX"}, nil
}

func (s *fakeSlack) Bot(context.Context, string, string) (*slackrelay.BotInfo, error) {
	return &slackrelay.BotInfo{ID: "BVIX", AppID: s.appID, Name: "Vix"}, nil
}

func (s *fakeSlack) JoinChannel(_ context.Context, _, channel string) (*slackrelay.Channel, error) {
	if channel == "GSECRET" {
		return nil, &slackrelay.Error{Method: "conversations.join", Code: "method_not_supported_for_channel_type"}
	}
	s.joined = append(s.joined, channel)
	return &slackrelay.Channel{ID: channel, Name: "general", IsMember: true}, nil
}

type rig struct {
	t      *testing.T
	store  *fakeStore
	slack  *fakeSlack
	router *mux.Router
	user   uuid.UUID
	kicks  int
	// tokenApp is the app the app-level token belongs to.
	tokenApp string
}

func newRig(t *testing.T) *rig {
	user := uuid.New()
	r := &rig{t: t, store: newFakeStore(user), slack: &fakeSlack{botUser: "UVIX", appID: "AVIX"}, user: user, tokenApp: "AVIX"}
	h := &Handler{Store: r.store, Slack: r.slack, Logger: zap.NewNop(), Changed: func() { r.kicks++ },
		CheckAppToken: func(_ context.Context, tok string) (string, error) {
			if tok == "xapp-revoked" {
				return "", &slackrelay.Error{Method: "apps.connections.open", Code: "invalid_auth"}
			}
			return r.tokenApp, nil
		}}
	r.router = mux.NewRouter()
	h.Register(r.router.PathPrefix("/slack").Subrouter())
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

func (r *rig) createApp() string {
	r.t.Helper()
	code, out := r.do(http.MethodPost, "/slack/apps", map[string]any{
		"personality_id": r.store.persona.ID, "bot_token": " xoxb-1 ", "app_token": "xapp-1",
	})
	require.Equal(r.t, http.StatusCreated, code, out)
	return out["id"].(string)
}

func TestManifestIsForThePersona(t *testing.T) {
	r := newRig(t)
	code, out := r.do(http.MethodGet, "/slack/manifest?personality_id="+r.store.persona.ID.String(), nil)
	require.Equal(t, http.StatusOK, code, out)
	link, err := url.Parse(out["create_url"].(string))
	require.NoError(t, err)
	assert.Contains(t, link.Query().Get("manifest_json"), `"name":"Vix"`)
	assert.Equal(t, "Vix", out["manifest"].(map[string]any)["display_information"].(map[string]any)["name"])

	code, _ = r.do(http.MethodGet, "/slack/manifest", nil)
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestCreateAppChecksBothTokensAreOneApps(t *testing.T) {
	tests := []struct {
		name               string
		botToken, appToken string
		tokenApp           string
		wantCode           int
		wantMessage        string
	}{
		{name: "a user token is refused before asking Slack", botToken: "xoxp-1", appToken: "xapp-1", wantCode: http.StatusBadRequest, wantMessage: "xoxb-"},
		{name: "an app token must be an app-level token", botToken: "xoxb-1", appToken: "xoxb-2", wantCode: http.StatusBadRequest, wantMessage: "xapp-"},
		{name: "a revoked bot token", botToken: "xoxb-revoked", appToken: "xapp-1", wantCode: http.StatusBadRequest, wantMessage: "rejected the bot token"},
		{name: "a revoked app token", botToken: "xoxb-1", appToken: "xapp-revoked", wantCode: http.StatusBadRequest, wantMessage: "app-level token"},
		{name: "tokens of two apps", botToken: "xoxb-1", appToken: "xapp-1", tokenApp: "AOTHER", wantCode: http.StatusBadRequest, wantMessage: "different Slack app"},
		{name: "one app's tokens", botToken: "xoxb-1", appToken: "xapp-1", wantCode: http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			if tt.tokenApp != "" {
				r.tokenApp = tt.tokenApp
			}
			code, out := r.do(http.MethodPost, "/slack/apps", map[string]any{
				"personality_id": r.store.persona.ID, "bot_token": tt.botToken, "app_token": tt.appToken,
			})
			require.Equal(t, tt.wantCode, code, out)
			if tt.wantMessage != "" {
				assert.Contains(t, out["message"], tt.wantMessage)
				assert.Empty(t, r.store.apps)
				return
			}
			assert.Equal(t, "AVIX", out["app_id"])
			assert.Equal(t, "https://api.slack.com/apps/AVIX/general", out["settings_url"])
			assert.NotContains(t, out, "bot_token")
			assert.NotContains(t, out, "app_token")
			assert.Equal(t, 1, r.kicks, "the gateway is told")
		})
	}
}

func TestUpdateAppTokens(t *testing.T) {
	r := newRig(t)
	id := r.createApp()
	appID := uuid.MustParse(id)
	r.store.status[appID] = models.SlackAppInvalidToken

	code, out := r.do(http.MethodPatch, "/slack/apps/"+id, map[string]any{"bot_token": "xoxb-new"})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "active", out["status"], "working tokens clear invalid_token")
	assert.Equal(t, "xoxb-new", r.store.apps[appID].BotToken)

	r.slack.appID = "AOTHER"
	code, _ = r.do(http.MethodPatch, "/slack/apps/"+id, map[string]any{"bot_token": "xoxb-other"})
	assert.Equal(t, http.StatusBadRequest, code, "another app's token cannot replace this one's")
	assert.Equal(t, "xoxb-new", r.store.apps[appID].BotToken)

	code, out = r.do(http.MethodPatch, "/slack/apps/"+id, map[string]any{"enabled": false})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "disabled", out["status"])
}

func TestBindingCreatesASandboxedThreadAndRefusesAnOpenOne(t *testing.T) {
	r := newRig(t)
	appID := r.createApp()

	code, out := r.do(http.MethodPost, "/slack/bindings", map[string]any{"app_id": appID, "channel_id": "C1", "channel_name": "#general",
		"allow_user_ids": []string{" ualice ", "UALICE", "not an id", "C123"}})
	require.Equal(t, http.StatusCreated, code, out)
	require.Len(t, r.store.createdCh, 1)
	assert.Equal(t, models.ContextScopeSandbox, r.store.createdCh[0].ContextScope)
	assert.Equal(t, "Slack · #general", r.store.createdCh[0].Name)
	assert.Equal(t, r.store.persona.ID, r.store.createdCh[0].PersonalityID)
	assert.Equal(t, []any{"UALICE"}, out["allow_user_ids"], "ids are cleaned and de-duplicated")
	assert.Equal(t, true, out["chat_sandboxed"])

	open := uuid.New()
	r.store.chats[open] = &models.Chat{ID: open, ContextScope: models.ContextScopeAccount}
	code, _ = r.do(http.MethodPost, "/slack/bindings", map[string]any{"app_id": appID, "channel_id": "C2", "chat_id": open})
	assert.Equal(t, http.StatusBadRequest, code, "an unsandboxed thread needs the acknowledgement")
	code, out = r.do(http.MethodPost, "/slack/bindings", map[string]any{"app_id": appID, "channel_id": "C2", "chat_id": open, "allow_unrestricted": true})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, true, out["allow_unrestricted"])

	code, _ = r.do(http.MethodPost, "/slack/bindings", map[string]any{"app_id": appID, "channel_id": "general"})
	assert.Equal(t, http.StatusBadRequest, code, "a channel name is not a channel id")
}

func TestJoinChannel(t *testing.T) {
	r := newRig(t)
	appID := r.createApp()
	code, out := r.do(http.MethodPost, "/slack/apps/"+appID+"/channels/C1/join", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, []string{"C1"}, r.slack.joined)
	code, out = r.do(http.MethodPost, "/slack/apps/"+appID+"/channels/GSECRET/join", nil)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, out["message"], "/invite")
}

func TestAnotherAccountsRowsAreNotFound(t *testing.T) {
	r := newRig(t)
	appID := r.createApp()
	r.user = uuid.New()
	for _, call := range []struct{ method, path string }{
		{http.MethodPatch, "/slack/apps/" + appID},
		{http.MethodGet, "/slack/apps/" + appID + "/channels"},
		{http.MethodGet, "/slack/manifest?personality_id=" + r.store.persona.ID.String()},
	} {
		code, _ := r.do(call.method, call.path, map[string]any{"refresh": true})
		assert.Equal(t, http.StatusNotFound, code, call.path)
	}
}

// onlyFor is a per-user experimental gate that has the relay on for one user.
type onlyFor uuid.UUID

func (g onlyFor) ExperimentalEnabled(_ context.Context, userID uuid.UUID, feature string) bool {
	return feature == models.ExperimentalFeatureSlackRelay && userID == uuid.UUID(g)
}

func withExperimentalGate(t *testing.T, g featuregate.ExperimentalGate) {
	t.Helper()
	orig := featuregate.ActiveExperimental
	featuregate.ActiveExperimental = g
	t.Cleanup(func() { featuregate.ActiveExperimental = orig })
}

func TestRoutesAreNotFoundForUsersWithoutTheFeature(t *testing.T) {
	t.Setenv("SLACK_RELAY_ENABLED", "true")
	tester, other := uuid.New(), uuid.New()
	withExperimentalGate(t, onlyFor(tester))

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := experimentalOnly(zap.NewNop())(ok)
	call := func(user *uuid.UUID) int {
		req := httptest.NewRequest(http.MethodGet, "/api/slack/apps", nil)
		if user != nil {
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, *user))
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	assert.Equal(t, http.StatusNoContent, call(&tester))
	assert.Equal(t, http.StatusNotFound, call(&other), "the web app hides Slack on a 404")
	assert.Equal(t, http.StatusNotFound, call(nil))
}

func TestGatewayKeepsOnlyAppsOfOwnersWithTheFeature(t *testing.T) {
	t.Setenv("SLACK_RELAY_ENABLED", "true")
	tester, other := uuid.New(), uuid.New()
	withExperimentalGate(t, onlyFor(tester))

	all := []models.SlackAppCredentials{{ID: uuid.New(), OwnerID: tester}, {ID: uuid.New(), OwnerID: other}, {ID: uuid.New(), OwnerID: tester}}
	list := mayUse(func(context.Context) ([]models.SlackAppCredentials, error) {
		return append([]models.SlackAppCredentials(nil), all...), nil
	})
	got, err := list(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, c := range got {
		assert.Equal(t, tester, c.OwnerID)
	}

	t.Setenv("SLACK_RELAY_ENABLED", "false")
	got, err = list(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got, "switching the relay off closes every connection")
}

func TestFingerprintChangesWithTokensOnly(t *testing.T) {
	a := models.SlackAppCredentials{ID: uuid.New(), AppToken: "xapp-1", BotToken: "xoxb-1", BotUserID: "U1"}
	b := a
	b.TeamID = "T2"
	assert.Equal(t, fingerprint(a), fingerprint(b))
	b.AppToken = "xapp-2"
	assert.NotEqual(t, fingerprint(a), fingerprint(b))
	assert.NotContains(t, fingerprint(a), "xapp", "tokens are not kept in the clear")
}
