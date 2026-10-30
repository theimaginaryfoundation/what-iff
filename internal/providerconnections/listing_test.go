package providerconnections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// seen records the auth headers of each request a test server received.
type seen struct {
	mu       sync.Mutex
	requests []http.Header
	paths    []string
}

func (s *seen) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Header.Clone())
	s.paths = append(s.paths, r.URL.RequestURI())
}

// openAIServer answers GET /v1/models like an OpenAI-compatible server.
func openAIServer(t *testing.T, s *seen) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"zeta","object":"model","created":1,"owned_by":"x"},{"id":"alpha","object":"model","created":1,"owned_by":"x"}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// anthropicServer answers GET /v1/models like Anthropic, two pages.
func anthropicServer(t *testing.T, s *seen) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after_id") == "" {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"claude-b","display_name":"Claude B","created_at":"2026-01-01T00:00:00Z","type":"model"}],"has_more":true,"first_id":"claude-b","last_id":"claude-b"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"claude-a","display_name":"Claude A","created_at":"2026-01-01T00:00:00Z","type":"model"}],"has_more":false,"first_id":"claude-a","last_id":"claude-a"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The deployment's own keys live in the process environment, where the SDKs'
// client defaults would pick them up. A connection must send its own key, and
// none at all when it has none.
func setDeploymentKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-deployment-openai")
	t.Setenv("OPENAI_ORG_ID", "org-deployment")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-deployment")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "deployment-token")
}

func TestListOpenAI_SendsTheConnectionsKey(t *testing.T) {
	setDeploymentKeys(t)
	var s seen
	srv := openAIServer(t, &s)

	got, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorOpenAI, BaseURL: srv.URL + "/v1", Key: "sk-connection-key"})
	require.NoError(t, err)
	require.Equal(t, []ListedModel{{ID: "alpha"}, {ID: "zeta"}}, got, "sorted by id")
	require.Len(t, s.requests, 1)
	require.Equal(t, "Bearer sk-connection-key", s.requests[0].Get("Authorization"))
	require.Empty(t, s.requests[0].Get("OpenAI-Organization"))
}

func TestListOpenAI_NoKeySendsNone(t *testing.T) {
	setDeploymentKeys(t)
	var s seen
	srv := openAIServer(t, &s)

	_, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorOpenAI, BaseURL: srv.URL + "/v1", Key: ""})
	require.NoError(t, err)
	require.Empty(t, s.requests[0].Get("Authorization"), "never the deployment's key")
	require.Empty(t, s.requests[0].Get("OpenAI-Organization"))
}

func TestListAnthropic_PagesAndSendsTheConnectionsKey(t *testing.T) {
	setDeploymentKeys(t)
	var s seen
	srv := anthropicServer(t, &s)

	got, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorAnthropic, BaseURL: srv.URL, Key: "sk-ant-connection"})
	require.NoError(t, err)
	require.Equal(t, []ListedModel{
		{ID: "claude-a", DisplayName: "Claude A"},
		{ID: "claude-b", DisplayName: "Claude B"},
	}, got)
	require.Len(t, s.requests, 2, "every page")
	for _, h := range s.requests {
		require.Equal(t, "sk-ant-connection", h.Get("X-Api-Key"))
		require.Empty(t, h.Get("Authorization"))
		require.NotEmpty(t, h.Get("Anthropic-Version"))
	}
	require.Contains(t, s.paths[0], "/v1/models")
}

func TestListAnthropic_NoKeySendsNone(t *testing.T) {
	setDeploymentKeys(t)
	var s seen
	srv := anthropicServer(t, &s)

	_, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorAnthropic, BaseURL: srv.URL, Key: ""})
	require.NoError(t, err)
	for _, h := range s.requests {
		require.Empty(t, h.Get("X-Api-Key"))
		require.Empty(t, h.Get("Authorization"))
	}
}

func TestList_FailuresHaveStableCodes(t *testing.T) {
	status := func(code int, body string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = fmt.Fprint(w, body)
		}))
		t.Cleanup(srv.Close)
		return srv.URL + "/v1"
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL + "/v1"
	closed.Close()
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, "<html>router admin page</html>")
	}))
	t.Cleanup(html.Close)

	for name, tc := range map[string]struct {
		url  string
		want string
	}{
		"nothing listening": {closedURL, models.ErrCodeProviderUnreachable},
		"key rejected":      {status(401, `{"error":{"message":"bad key"}}`), models.ErrCodeProviderKeyRejected},
		"forbidden":         {status(403, `{"error":{"message":"no"}}`), models.ErrCodeProviderKeyRejected},
		"no models route":   {status(404, `{"error":{"message":"not found"}}`), models.ErrCodeProviderIncompatible},
		"not that API":      {html.URL + "/v1", models.ErrCodeProviderIncompatible},
		"server error":      {status(500, `{"error":{"message":"boom"}}`), models.ErrCodeProviderUnavailable},
		"rate limited":      {status(429, `{"error":{"message":"slow down"}}`), models.ErrCodeProviderUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorOpenAI, BaseURL: tc.url, Key: "sk-connection-key"})
			var ce *ConnectError
			require.True(t, errors.As(err, &ce), "got %v", err)
			require.Equal(t, tc.want, ce.Code)
		})
	}
}

func TestList_MockSendsNothing(t *testing.T) {
	got, err := NewLister(nil, true).List(context.Background(), Target{Flavor: FlavorOpenAI, BaseURL: "http://192.0.2.1/v1", Key: ""})
	require.NoError(t, err)
	require.Equal(t, mockModels, got)
}

func TestNormalizeAddress(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:11434/v1/":  "http://localhost:11434/v1",
		"  https://example.test/v1  ": "https://example.test/v1",
	} {
		got, err := normalizeAddress(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got)
	}
	for _, bad := range []string{"", "localhost:11434", "ftp://example.test", "http://", "http://user:pass@example.test/v1", "::"} {
		_, err := normalizeAddress(bad)
		require.ErrorIs(t, err, ErrInvalidAddress, bad)
	}
}

func TestPresets(t *testing.T) {
	presets := Presets(map[models.ModelProvider]string{models.ModelProviderOpenAI: "https://api.example.test/v1"}, "host.docker.internal")

	openai, ok := findPreset(presets, "openai")
	require.True(t, ok)
	require.Equal(t, "OpenAI", openai.Name)
	require.Equal(t, "https://api.example.test/v1", openai.BaseURL, "the deployment's address for it")
	require.True(t, openai.Builtin)
	require.True(t, openai.KeyRequired)
	require.False(t, openai.AddressEditable)

	ollama, ok := findPreset(presets, PresetOllama)
	require.True(t, ok)
	require.Equal(t, "http://host.docker.internal:11434/v1", ollama.BaseURL, "reached by the local host name")
	require.True(t, ollama.AddressEditable)
	require.False(t, ollama.KeyRequired)

	custom, ok := findPreset(presets, PresetCustom)
	require.True(t, ok)
	require.Empty(t, custom.Flavor)
	require.Empty(t, custom.BaseURL)

	for _, p := range models.ModelProviders {
		_, ok := findPreset(presets, string(p))
		require.True(t, ok, "every built-in provider is a preset: %s", p)
		require.True(t, builtinFlavors[p].Valid(), "and has a flavor to list with: %s", p)
	}
}

// OpenRouter's model list is public, so the key is checked separately: a bad
// key fails the connection even though listing worked.
func TestList_KeyCheckForAPublicModelList(t *testing.T) {
	var s seen
	srv := openAIServer(t, &s)
	check := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-or-good-key-1234" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":{}}`)
	}))
	t.Cleanup(check.Close)
	target := Target{Flavor: FlavorOpenAI, BaseURL: srv.URL + "/v1", KeyCheckURL: check.URL}

	target.Key = "sk-or-good-key-1234"
	got, err := NewLister(nil, false).List(context.Background(), target)
	require.NoError(t, err)
	require.Len(t, got, 2)

	target.Key = "sk-or-wrong-key-0000"
	_, err = NewLister(nil, false).List(context.Background(), target)
	var ce *ConnectError
	require.True(t, errors.As(err, &ce), "got %v", err)
	require.Equal(t, models.ErrCodeProviderKeyRejected, ce.Code)
}

// Google answers a bad key with 400; for it, that is a rejected key, and for
// everyone else 400 still means the server is not what was expected.
func TestList_BadKeyStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":{"message":"API key not valid"}}`)
	}))
	t.Cleanup(srv.Close)

	var ce *ConnectError
	_, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorOpenAI, BaseURL: srv.URL, Key: "sk-wrong-key-0000", BadKeyStatus: 400})
	require.True(t, errors.As(err, &ce))
	require.Equal(t, models.ErrCodeProviderKeyRejected, ce.Code)

	_, err = NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorOpenAI, BaseURL: srv.URL, Key: "sk-wrong-key-0000"})
	require.True(t, errors.As(err, &ce))
	require.Equal(t, models.ErrCodeProviderIncompatible, ce.Code)
}

// A provider with no models answers an empty list, never null.
func TestList_NoModelsIsAnEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"object":"list","data":[]}`)
	}))
	t.Cleanup(srv.Close)
	got, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorOpenAI, BaseURL: srv.URL})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestPresetTargets(t *testing.T) {
	presets := Presets(map[models.ModelProvider]string{
		models.ModelProviderZAI:    "https://api.z.ai/api/anthropic",
		models.ModelProviderGoogle: "https://generativelanguage.googleapis.com/v1beta/openai/",
	}, "localhost")
	zai, _ := findPreset(presets, "zai")
	got := zai.target(zai.Flavor, zai.BaseURL, "k")
	require.Equal(t, Target{Flavor: FlavorOpenAI, BaseURL: zaiListURL, Key: "k"}, got, "listed where the key is checked")

	google, _ := findPreset(presets, "google")
	require.Equal(t, 400, google.target(google.Flavor, google.BaseURL, "k").BadKeyStatus)

	openrouter, _ := findPreset(presets, PresetOpenRouter)
	require.Equal(t, openRouterKeyCheckURL, openrouter.target(openrouter.Flavor, openrouter.BaseURL, "k").KeyCheckURL)

	openai, _ := findPreset(presets, "openai")
	openaiTarget := openai.target(openai.Flavor, "", "k")
	require.NotNil(t, openaiTarget.CannotChat, "its listing drops models that can't chat")
	openaiTarget.CannotChat = nil
	require.Equal(t, Target{Flavor: FlavorOpenAI, Key: "k"}, openaiTarget, "and no other quirks")
}

// A provider's error can echo what it was sent. The cause is logged, so the
// key is taken out of it.
func TestList_ErrorNeverCarriesTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"bad request, you sent %s"}}`, r.Header.Get("Authorization"))
	}))
	t.Cleanup(srv.Close)

	const echoed = "sk-echoed-key-4321" // made up
	_, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorOpenAI, BaseURL: srv.URL, Key: echoed})
	var ce *ConnectError
	require.True(t, errors.As(err, &ce))
	require.Equal(t, models.ErrCodeProviderIncompatible, ce.Code, "classified before redacting")
	require.NotContains(t, ce.Error(), echoed)
	require.Contains(t, ce.Error(), "[redacted]")
}

// An Anthropic-compatible server that ignores after_id answers the same page
// every time. The listing stops instead of asking until the timeout.
func TestListAnthropic_StopsWhenTheCursorDoesNotMove(t *testing.T) {
	for name, body := range map[string]string{
		"has_more left out":             `{"data":[{"id":"m1","display_name":"M1","created_at":"2026-01-01T00:00:00Z","type":"model"}],"first_id":"m1","last_id":"m1"}`,
		"has_more true, cursor repeats": `{"data":[{"id":"m1","display_name":"M1","created_at":"2026-01-01T00:00:00Z","type":"model"}],"has_more":true,"first_id":"m1","last_id":"m1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var s seen
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.record(r)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, body)
			}))
			t.Cleanup(srv.Close)

			got, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorAnthropic, BaseURL: srv.URL})
			require.NoError(t, err)
			require.LessOrEqual(t, len(s.requests), 2)
			require.NotEmpty(t, got)
		})
	}
}

// A server that always says there is more, with a new cursor each time, is
// asked a bounded number of times.
func TestListAnthropic_StopsAtThePageCap(t *testing.T) {
	var s seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		id := fmt.Sprintf("m%d", len(s.requests))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"display_name":"x","created_at":"2026-01-01T00:00:00Z","type":"model"}],"has_more":true,"first_id":%q,"last_id":%q}`, id, id, id)
	}))
	t.Cleanup(srv.Close)

	got, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorAnthropic, BaseURL: srv.URL})
	require.NoError(t, err)
	require.Len(t, s.requests, maxListPages)
	require.Len(t, got, maxListPages)
}

// z.ai is listed through its OpenAI-compatible endpoint only at its default
// address; pointed elsewhere, it is listed where it was pointed.
func TestZAIListingFollowsTheConfiguredAddress(t *testing.T) {
	require.Equal(t, provider.DefaultZAIBaseURL, zaiDefaultURL, "the default the redirect is keyed on")

	atDefault := Presets(map[models.ModelProvider]string{models.ModelProviderZAI: provider.DefaultZAIBaseURL}, "localhost")
	zai, _ := findPreset(atDefault, "zai")
	require.Equal(t, zaiListURL, zai.target(zai.Flavor, zai.BaseURL, "k").BaseURL)

	elsewhere := Presets(map[models.ModelProvider]string{models.ModelProviderZAI: "https://open.bigmodel.cn/api/anthropic"}, "localhost")
	zai, _ = findPreset(elsewhere, "zai")
	got := zai.target(zai.Flavor, zai.BaseURL, "k")
	require.Equal(t, Target{Flavor: FlavorAnthropic, BaseURL: "https://open.bigmodel.cn/api/anthropic", Key: "k"}, got)
}

func TestPresetKeysURLs(t *testing.T) {
	presets := Presets(nil, "localhost")
	openai, _ := findPreset(presets, "openai")
	require.Equal(t, "https://platform.openai.com/api-keys", openai.KeysURL)
	openrouter, _ := findPreset(presets, PresetOpenRouter)
	require.Equal(t, "https://openrouter.ai/keys", openrouter.KeysURL)
}

// OpenRouter reports each model's name, parameters and input modalities;
// most servers report only the id, which leaves capabilities unknown.
func TestListedFromOpenAI_ReadsReportedCapabilities(t *testing.T) {
	m := listedFromOpenAI("openai/gpt-4o", `{"id":"openai/gpt-4o","name":"OpenAI: GPT-4o","supported_parameters":["tools","temperature"],"architecture":{"input_modalities":["text","image"]}}`)
	require.Equal(t, "OpenAI: GPT-4o", m.DisplayName)
	require.True(t, *m.ToolSupport)
	require.True(t, *m.VisionSupport)

	m = listedFromOpenAI("text-only", `{"id":"text-only","supported_parameters":["temperature"],"architecture":{"input_modalities":["text"]}}`)
	require.False(t, *m.ToolSupport, "reported as not supported")
	require.False(t, *m.VisionSupport)

	m = listedFromOpenAI("llama3", `{"id":"llama3","object":"model","owned_by":"library"}`)
	require.Nil(t, m.ToolSupport, "not reported")
	require.Nil(t, m.VisionSupport)
	require.Empty(t, m.DisplayName)
}

func TestWithKnownCapabilities(t *testing.T) {
	no := false
	anthropic := Preset{ID: "anthropic"}
	got := withKnownCapabilities(anthropic, FlavorAnthropic, []ListedModel{{ID: "claude"}})
	require.True(t, *got[0].ToolSupport)
	require.True(t, *got[0].VisionSupport)

	custom := Preset{ID: PresetCustom}
	got = withKnownCapabilities(custom, FlavorAnthropic, []ListedModel{{ID: "glm"}})
	require.True(t, *got[0].ToolSupport, "Anthropic-compatible servers take tools")
	require.Nil(t, got[0].VisionSupport, "images unknown for others")

	got = withKnownCapabilities(custom, FlavorOpenAI, []ListedModel{{ID: "x", ToolSupport: &no}})
	require.False(t, *got[0].ToolSupport, "what the provider reported wins")
	require.Nil(t, got[0].VisionSupport)
}

// A built-in provider's model the app already knows is prefilled with what it
// knows; the same id from another provider is not.
func TestWithKnownCapabilities_FromTheBuiltInModels(t *testing.T) {
	known := models.CatalogModel("gpt-5.1")
	require.NotNil(t, known)
	openai := Preset{ID: "openai", Builtin: true}
	got := withKnownCapabilities(openai, FlavorOpenAI, []ListedModel{{ID: "gpt-5.1"}, {ID: "gpt-9-unknown"}})
	require.Equal(t, known.ToolSupport, *got[0].ToolSupport)
	require.Equal(t, known.VisionSupport, *got[0].VisionSupport)
	require.Nil(t, got[1].ToolSupport, "a model the app doesn't know stays unknown")

	openrouter := Preset{ID: PresetOpenRouter}
	got = withKnownCapabilities(openrouter, FlavorOpenAI, []ListedModel{{ID: "gpt-5.1"}})
	require.Nil(t, got[0].ToolSupport, "only for the provider's own connection")
}

// OpenAI's listing includes models that can't chat; they are not offered.
// The ids are from a real listing (October 2026).
func TestOpenAICannotChat(t *testing.T) {
	for _, id := range []string{
		"babbage-002", "davinci-002", "text-embedding-3-small", "omni-moderation-latest", "tts-1-hd",
		"whisper-1", "gpt-image-2", "chatgpt-image-latest", "sora-2-pro", "gpt-audio-mini",
		"gpt-realtime-2.1-mini", "gpt-live-1", "gpt-transcribe", "gpt-4o-mini-transcribe",
		"gpt-4o-mini-tts-2025-12-15", "gpt-3.5-turbo-instruct-0914",
	} {
		require.True(t, openAICannotChat(id), id)
	}
	for _, id := range []string{
		"gpt-5.4", "gpt-5.4-mini", "gpt-5.6-luna", "gpt-6-astra", "gpt-4o", "gpt-4.1-nano", "gpt-3.5-turbo",
		"o3", "o4-mini", "o3-deep-research", "gpt-5-pro", "gpt-5.1-codex-max", "gpt-5-search-api", "chat-latest",
	} {
		require.False(t, openAICannotChat(id), id)
	}
}

func TestList_DropsModelsTheProviderSaysCannotChat(t *testing.T) {
	srv := openAIServer(t, &seen{})
	got, err := NewLister(nil, false).List(context.Background(), Target{
		Flavor: FlavorOpenAI, BaseURL: srv.URL + "/v1",
		CannotChat: func(id string) bool { return id == "zeta" },
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "alpha", got[0].ID)

	presets := Presets(map[models.ModelProvider]string{models.ModelProviderOpenAI: "https://api.openai.com/v1"}, "localhost")
	openai, _ := findPreset(presets, "openai")
	require.NotNil(t, openai.target(FlavorOpenAI, openai.BaseURL, "k").CannotChat, "OpenAI's preset filters its listing")
	openrouter, _ := findPreset(presets, PresetOpenRouter)
	require.Nil(t, openrouter.target(FlavorOpenAI, openrouter.BaseURL, "k").CannotChat, "others are offered as listed")
}

// Ollama's OpenAI-compatible list reports only ids; its native /api/show says
// what each model can do. A failed lookup leaves that model unknown.
func TestList_OllamaDetails(t *testing.T) {
	var s seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"qwen3:0.6b","object":"model"},{"id":"llava:7b","object":"model"},{"id":"broken","object":"model"}]}`)
		case "/api/show":
			var body struct{ Model string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch body.Model {
			case "qwen3:0.6b":
				_, _ = fmt.Fprint(w, `{"capabilities":["completion","tools","thinking"]}`)
			case "llava:7b":
				_, _ = fmt.Fprint(w, `{"capabilities":["completion","vision"]}`)
			default:
				w.WriteHeader(http.StatusInternalServerError)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	got, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorOpenAI, BaseURL: srv.URL + "/v1", OllamaDetails: true})
	require.NoError(t, err, "a failed lookup does not fail the listing")
	byID := map[string]ListedModel{}
	for _, m := range got {
		byID[m.ID] = m
	}
	require.True(t, *byID["qwen3:0.6b"].ToolSupport)
	require.False(t, *byID["qwen3:0.6b"].VisionSupport)
	require.False(t, *byID["llava:7b"].ToolSupport)
	require.True(t, *byID["llava:7b"].VisionSupport)
	require.Nil(t, byID["broken"].ToolSupport)
	require.Contains(t, s.paths, "/api/show", "asked at the server's root, not under /v1")

	ollama, _ := findPreset(Presets(nil, "localhost"), PresetOllama)
	require.True(t, ollama.target(ollama.Flavor, ollama.BaseURL, "").OllamaDetails)
}

// lmStudioServer answers like LM Studio: an OpenAI-compatible list with ids
// only, and a native list in version 1 or 0 (or none).
func lmStudioServer(t *testing.T, native string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/models":
			_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"qwen3-0.6b","object":"model"},{"id":"gemma-3-4b","object":"model"},{"id":"text-embedding-nomic-embed-text-v1.5","object":"model"}]}`)
		case r.URL.Path == "/api/v1/models" && native == "v1":
			_, _ = fmt.Fprint(w, `{"models":[
				{"type":"llm","key":"qwen3-0.6b","display_name":"Qwen3 0.6B","capabilities":{"vision":false,"trained_for_tool_use":true}},
				{"type":"llm","key":"gemma-3-4b","display_name":"Gemma 3 4B","capabilities":{"vision":true,"trained_for_tool_use":false}},
				{"type":"embedding","key":"text-embedding-nomic-embed-text-v1.5","display_name":"Nomic Embed Text v1.5"}]}`)
		case r.URL.Path == "/api/v0/models" && native == "v0":
			_, _ = fmt.Fprint(w, `{"object":"list","data":[
				{"id":"qwen3-0.6b","type":"llm","capabilities":["tool_use"]},
				{"id":"gemma-3-4b","type":"vlm"},
				{"id":"text-embedding-nomic-embed-text-v1.5","type":"embeddings"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestList_LMStudioDetails(t *testing.T) {
	for _, version := range []string{"v1", "v0"} {
		t.Run(version, func(t *testing.T) {
			srv := lmStudioServer(t, version)
			got, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorOpenAI, BaseURL: srv.URL + "/v1", LMStudioDetails: true})
			require.NoError(t, err)
			require.Len(t, got, 2, "the embedding model can't chat, so it is not offered")
			byID := map[string]ListedModel{}
			for _, m := range got {
				byID[m.ID] = m
			}
			require.True(t, *byID["qwen3-0.6b"].ToolSupport)
			require.False(t, *byID["qwen3-0.6b"].VisionSupport)
			require.False(t, *byID["gemma-3-4b"].ToolSupport)
			require.True(t, *byID["gemma-3-4b"].VisionSupport)
			if version == "v1" {
				require.Equal(t, "Qwen3 0.6B", byID["qwen3-0.6b"].DisplayName)
			}
		})
	}

	t.Run("no native list", func(t *testing.T) {
		srv := lmStudioServer(t, "none")
		got, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorOpenAI, BaseURL: srv.URL + "/v1", LMStudioDetails: true})
		require.NoError(t, err)
		require.Len(t, got, 3, "kept as listed")
		require.Nil(t, got[0].ToolSupport)
	})

	lmstudio, _ := findPreset(Presets(nil, "localhost"), PresetLMStudio)
	require.True(t, lmstudio.target(lmstudio.Flavor, lmstudio.BaseURL, "").LMStudioDetails)
}

// An Ollama embedding model lacks "completion", so it is not offered for chat.
func TestList_OllamaDropsModelsThatCannotChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"qwen3:0.6b","object":"model"},{"id":"nomic-embed-text","object":"model"}]}`)
		case "/api/show":
			var body struct{ Model string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Model == "nomic-embed-text" {
				_, _ = fmt.Fprint(w, `{"capabilities":["embedding"]}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"capabilities":["completion","tools"]}`)
		}
	}))
	t.Cleanup(srv.Close)

	got, err := NewLister(nil, false).List(context.Background(), Target{Flavor: FlavorOpenAI, BaseURL: srv.URL + "/v1", OllamaDetails: true})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "qwen3:0.6b", got[0].ID)
}
