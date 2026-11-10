package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerconnections"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A missing key surfaces from the credential transport through the provider's SDK; the
// notice must name it as the cause (so the client can offer "add a key"), with the provider
// and model the step went to.
func TestBackgroundRun_MissingKeyBecomesNotice(t *testing.T) {
	t.Parallel()
	noKey := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, &providerkeys.NotConfiguredError{Provider: models.ModelProviderOpenAI}
	})}
	client := openai.NewClient(option.WithAPIKey("unused"), option.WithHTTPClient(noKey), option.WithMaxRetries(0))
	a := &Agent{logger: zap.NewNop(), OpenAIProvider: provider.NewOpenAIProvider(nil, &client, nil, nil)}
	ctx, run := withBackgroundRun(withTestUtilityModels(context.Background()))

	err := a.extractMemoriesWithScratchpadDeltaClaude(ctx, uuid.New(), uuid.New(),
		&provider.ModelContext{}, &provider.ModelContext{}, claudeChatCtx(), nil)
	backgroundRunFrom(ctx).step(models.BackgroundStepMemory, err)

	out := run.outcome()
	require.Len(t, out.Failed, 1)
	got := out.Failed[0]
	require.Equal(t, models.BackgroundStepMemory, got.Step)
	require.Equal(t, models.BackgroundNoticeProviderNotConfigured, got.Code)
	require.Equal(t, "OpenAI", got.Provider)
	require.Equal(t, testUtilityModel, got.Model, "a Claude chat's memory step runs on the memory role's model")
}

func TestBackgroundNoticeFor(t *testing.T) {
	t.Parallel()
	call := &UtilityCallError{Provider: "Mistral", Model: "mistral-small-latest", Err: errors.New("503")}
	n, ok := backgroundNoticeFor(models.BackgroundStepSummary, call)
	require.True(t, ok)
	require.Equal(t, models.BackgroundNotice{Step: models.BackgroundStepSummary, Code: models.BackgroundNoticeProviderError, Provider: "Mistral", Model: "mistral-small-latest"}, n)

	_, ok = backgroundNoticeFor(models.BackgroundStepSummary, errors.New("database is down"))
	require.False(t, ok, "a failure that isn't the provider's stays in the logs")

	var run *backgroundRun
	run.step(models.BackgroundStepMemory, call) // a nil run records nothing, and doesn't panic
}

// Inside a run, a role passing over a chosen model records a fallback notice; resolving
// without falling back records the fallback step as clear, so an old notice goes away.
func TestBackgroundRun_RecordsFallbacks(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	ctx, run := withBackgroundRun(apicontext.WithUserID(context.Background(), f.alice))
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleMemory, f.aliceModel.ID)
	_, err := f.ent.Model.UpdateOneID(f.aliceModel.ID).SetDeleted(true).Save(context.Background())
	require.NoError(t, err)

	f.a.utilityTargetFor(ctx, provider.UtilityRoleMemory)
	f.a.utilityTargetFor(ctx, provider.UtilityRoleSimple)

	out := run.outcome()
	require.Equal(t, []models.BackgroundNotice{{Step: "fallback_memory", Code: models.BackgroundNoticeModelUnavailable}}, out.Failed)
	require.Equal(t, []models.BackgroundStep{"fallback_simple"}, out.Succeeded)
}

// One checkpoint resolves a role and calls a step several times; it reports each step once,
// and a failure anywhere in the run outweighs a success, so a notice is neither counted many
// times over nor cleared by the same run that saw it fail.
func TestBackgroundRun_OncePerStepFailureWins(t *testing.T) {
	t.Parallel()
	_, run := withBackgroundRun(withTestUtilityModels(context.Background()))
	call := &UtilityCallError{Provider: "Mistral", Model: "m", Err: errors.New("503")}
	run.succeeded(models.BackgroundStepMemory)
	run.step(models.BackgroundStepMemory, call)
	run.step(models.BackgroundStepMemory, call)
	run.succeeded(models.BackgroundStepMemory)
	run.succeeded(models.BackgroundStepSummary)
	run.succeeded(models.BackgroundStepSummary)

	out := run.outcome()
	require.Len(t, out.Failed, 1)
	require.Equal(t, models.BackgroundStepMemory, out.Failed[0].Step)
	require.Equal(t, []models.BackgroundStep{models.BackgroundStepSummary}, out.Succeeded)
}

// A checkpoint that runs out of time, or a server stopping, isn't the provider's doing.
func TestBackgroundRun_IgnoresCancellation(t *testing.T) {
	t.Parallel()
	_, run := withBackgroundRun(withTestUtilityModels(context.Background()))
	run.step(models.BackgroundStepSummary, &UtilityCallError{Provider: "OpenAI", Model: "m", Err: context.DeadlineExceeded})
	run.step(models.BackgroundStepMemory, &UtilityCallError{Provider: "OpenAI", Model: "m", Err: context.Canceled})
	require.Empty(t, run.outcome().Failed)
}

// A provider this deployment has no client for was never asked, so it's a missing provider,
// not a provider error.
func TestBackgroundNoticeFor_MissingProviderClient(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	_, err := a.callUtilityOn(context.Background(), utilityTarget{name: "mistral-small-latest", provider: string(models.ModelProviderMistral)}, utilityTestRequest)
	n, ok := backgroundNoticeFor(models.BackgroundStepMemory, err)
	require.True(t, ok)
	require.Equal(t, models.BackgroundNoticeProviderNotConfigured, n.Code)
	require.Equal(t, "Mistral", n.Provider)
}

// A connection model's provider is named the way a person knows it.
func TestUtilityCallError_NamesConnectionPreset(t *testing.T) {
	t.Parallel()
	err := utilityTarget{name: "x", connection: &ConnectionEndpoint{Preset: "lmstudio"}}.callError(errors.New("boom"))
	var call *UtilityCallError
	require.ErrorAs(t, err, &call)
	require.Equal(t, "LM Studio", call.Provider)
}

// Steps run with a collector on their context, and what they found lands on the chat: a
// failure becomes a notice, and the same step succeeding in a later run clears it.
func TestWithBackgroundNotices_SavesToTheChat(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	ctx := context.Background()
	chat, err := f.ent.Chat.Create().SetName("c").SetOwnerID(f.alice).Save(ctx)
	require.NoError(t, err)
	missing := &providerkeys.NotConfiguredError{Provider: models.ModelProviderAnthropic}

	f.a.withBackgroundNotices(ctx, f.alice, chat.ID, func(ctx context.Context) {
		backgroundRunFrom(ctx).step(models.BackgroundStepMemory, missing)
		backgroundRunFrom(ctx).step(models.BackgroundStepSummary, nil)
	})
	got, err := f.a.ds.ListBackgroundNotices(ctx, f.alice, chat.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, models.BackgroundNoticeProviderNotConfigured, got[0].Code)

	f.a.withBackgroundNotices(ctx, f.alice, chat.ID, func(ctx context.Context) {
		backgroundRunFrom(ctx).step(models.BackgroundStepMemory, nil)
	})
	got, err = f.a.ds.ListBackgroundNotices(ctx, f.alice, chat.ID)
	require.NoError(t, err)
	require.Empty(t, got)
}

// Every connection preset a person can add has a name they'd know, so a notice never shows a
// preset's code.
func TestConnectionPresetName_EveryPreset(t *testing.T) {
	t.Parallel()
	for _, preset := range []string{providerconnections.PresetOpenRouter, providerconnections.PresetOllama, providerconnections.PresetLMStudio, providerconnections.PresetCustom} {
		require.NotEqual(t, preset, connectionPresetName(preset), preset)
	}
}

// Only a provider with no client on the deployment, which a key fixes, tells the account it
// isn't set up; failing to route a model at all is a bug, which stays in the logs.
func TestUtilityClientError_OnlyAMissingKeyIsANotice(t *testing.T) {
	t.Parallel()
	target := utilityTarget{name: "mistral-small-latest", provider: string(models.ModelProviderMistral)}

	n, ok := backgroundNoticeFor(models.BackgroundStepMemory,
		target.clientError(providerNotConfigured("Mistral model %q requested but MISTRAL_API_KEY is not configured", target.name)))
	require.True(t, ok)
	require.Equal(t, models.BackgroundNoticeProviderNotConfigured, n.Code)
	require.Equal(t, "Mistral", n.Provider)

	_, ok = backgroundNoticeFor(models.BackgroundStepMemory,
		target.clientError(errors.New(`unsupported OpenAI Chat Completions provider "mistral" for model "mistral-small-latest"`)))
	require.False(t, ok, "a routing bug is not the account's to fix")
}

// A step that runs out of time must stay a timeout through every API's client and retries, so
// the run drops it rather than blaming the provider; one that flattened the deadline into text
// would show as a provider_error notice.
func TestUtilityCall_TimeoutStaysATimeoutOnEveryAPI(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	stall := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(stall.Close)
	t.Cleanup(func() { close(release) }) // runs first, so Close isn't left waiting on a handler
	a := &Agent{
		logger:          zap.NewNop(),
		OpenAIProvider:  newHTTPTestOpenAIProvider(stall.URL),
		ClaudeProvider:  newHTTPTestClaudeProvider(stall.URL),
		MistralProvider: provider.NewMistralProvider("k", stall.URL, nil, nil),
		GeminiProvider:  provider.NewGeminiProvider("k", stall.URL, nil, nil),
	}
	targets := map[string]utilityTarget{
		"Responses":        {name: "gpt-5.6-luna", provider: string(models.ModelProviderOpenAI)},
		"Messages":         {name: "claude-haiku-4-5", provider: string(models.ModelProviderAnthropic)},
		"Chat Completions": {name: "mistral-small-latest", provider: string(models.ModelProviderMistral)},
		"Gemini":           {name: "gemini-3.5", provider: string(models.ModelProviderGoogle)},
	}
	for name, target := range targets {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			_, err := a.callUtilityTarget(ctx, target, provider.UtilityRequest{Role: provider.UtilityRoleMemory, Input: "summarize"})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			runCtx, run := withBackgroundRun(withTestUtilityModels(context.Background()))
			backgroundRunFrom(runCtx).step(models.BackgroundStepSummary, err)
			out := run.outcome()
			require.Empty(t, out.Failed, "a timeout is not the provider's failure")
			require.Empty(t, out.Succeeded, "nor a success: the next checkpoint redoes the step")
		})
	}
}
