package agent

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
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
		return nil, &providerkeys.NotConfiguredError{Provider: models.ModelProviderAnthropic}
	})}
	a := &Agent{logger: zap.NewNop(), ClaudeProvider: provider.NewClaudeProvider("", nil, noKey)}
	ctx, run := withBackgroundRun(context.Background())

	err := a.extractMemoriesWithScratchpadDeltaClaude(ctx, uuid.New(), uuid.New(),
		&provider.ModelContext{}, &provider.ModelContext{}, claudeChatCtx(), nil)
	backgroundRunFrom(ctx).step(models.BackgroundStepMemory, err)

	require.Len(t, run.outcome.Failed, 1)
	got := run.outcome.Failed[0]
	require.Equal(t, models.BackgroundStepMemory, got.Step)
	require.Equal(t, models.BackgroundNoticeProviderNotConfigured, got.Code)
	require.Equal(t, "Anthropic", got.Provider)
	require.Equal(t, archivalClaudeModel, got.Model)
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
	f.a.utilityTargetFor(ctx, provider.UtilityRoleQuick)

	require.Equal(t, []models.BackgroundNotice{{Step: "fallback_memory", Code: models.BackgroundNoticeModelUnavailable}}, run.outcome.Failed)
	require.Equal(t, []models.BackgroundStep{"fallback_quick"}, run.outcome.Succeeded)
}
