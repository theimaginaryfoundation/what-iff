package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
	"go.uber.org/zap"
)

// refusingAdapter answers "ok" after refusing its first `refusals` calls on safety grounds. It
// holds `images` images until StripImages takes them.
type refusingAdapter struct {
	refusals int
	images   int
	calls    int
	forced   int
}

func safetyRefusal() error {
	return &provider.SafetyViolationError{Violation: &provider.SafetyViolation{
		Provider: models.SafetyViolationProviderOpenAI, ProviderCode: "moderation_blocked", ProviderMessage: "rejected",
	}}
}

func (f *refusingAdapter) Call(context.Context) (*provider.GenerateResponse, []provider.ToolUse, error) {
	f.calls++
	if f.calls <= f.refusals {
		return nil, nil, safetyRefusal()
	}
	return &provider.GenerateResponse{Text: "ok"}, nil, nil
}
func (f *refusingAdapter) AppendToolResults([]provider.ToolResult) {}
func (f *refusingAdapter) ForceFinalResponse(context.Context) (*provider.GenerateResponse, error) {
	f.forced++
	return &provider.GenerateResponse{Text: "forced"}, nil
}
func (f *refusingAdapter) WebSearchCompletedCount() int     { return 0 }
func (f *refusingAdapter) SetTextDeltaHandler(func(string)) {}
func (f *refusingAdapter) StripImages() int {
	n := f.images
	f.images = 0
	return n
}

// textOnlyAdapter is a refusingAdapter that cannot strip anything (no ImageStripper).
type textOnlyAdapter struct{ inner *refusingAdapter }

func (f textOnlyAdapter) Call(ctx context.Context) (*provider.GenerateResponse, []provider.ToolUse, error) {
	return f.inner.Call(ctx)
}
func (f textOnlyAdapter) AppendToolResults(r []provider.ToolResult) { f.inner.AppendToolResults(r) }
func (f textOnlyAdapter) ForceFinalResponse(ctx context.Context) (*provider.GenerateResponse, error) {
	return f.inner.ForceFinalResponse(ctx)
}
func (f textOnlyAdapter) WebSearchCompletedCount() int       { return 0 }
func (f textOnlyAdapter) SetTextDeltaHandler(h func(string)) {}

func loopChatContext() *chatContext {
	return &chatContext{chat: &models.Chat{ID: uuid.New()}}
}

func TestAgentLoop_RetriesARefusedCallWithoutImages(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	adapter := &refusingAdapter{refusals: 1, images: 2}
	chatCtx := loopChatContext()

	result, _, _, err := a.handleAgentLoop(context.Background(), chatCtx, adapter)

	require.NoError(t, err)
	require.Equal(t, "ok", result.Text)
	require.Equal(t, 2, adapter.calls, "the refused call is retried once")
	require.Equal(t, 2, chatCtx.imagesWithheld, "recorded for the reply's context breakdown")
	require.Equal(t, modeltypes.ImagesWithheldSafetyRefusal, chatCtx.imagesWithheldReason)
}

func TestAgentLoop_RefusalWithoutImagesIsReturned(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}

	for name, adapter := range map[string]provider.AgentAdapter{
		"nothing to strip":      &refusingAdapter{refusals: 1},
		"adapter cannot strip":  textOnlyAdapter{inner: &refusingAdapter{refusals: 1, images: 1}},
		"refused again without": &refusingAdapter{refusals: 2, images: 1},
	} {
		chatCtx := loopChatContext()
		_, _, _, err := a.handleAgentLoop(context.Background(), chatCtx, adapter)

		_, refused := provider.IsSafetyViolationError(err)
		require.True(t, refused, "%s: the turn's safety-violation path still handles it", name)
	}
}

func TestRetryWithoutImagesOnRefusal_LeavesOtherErrorsAlone(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	adapter := &refusingAdapter{images: 3}
	calls := 0
	boom := errors.New("connection reset")

	err := a.retryWithoutImagesOnRefusal(context.Background(), loopChatContext(), adapter, func() error {
		calls++
		return boom
	})

	require.ErrorIs(t, err, boom)
	require.Equal(t, 1, calls)
	require.Equal(t, 3, adapter.images, "nothing stripped for a non-safety failure")
}

func TestBuildContextBreakdown_RecordsImagesWithheld(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindUserMessage, provider.RoleUser, "hello", false)
	chatCtx := loopChatContext()
	chatCtx.imagesWithheld = 2

	breakdown := a.buildContextBreakdown(chatCtx, mc, 0, nil)

	require.NotNil(t, breakdown)
	require.Equal(t, 2, breakdown.ImagesWithheld)
	require.Empty(t, breakdown.ImagesWithheldReason, "set directly here; withholdImages records the reason")

	chatCtx = loopChatContext()
	chatCtx.withholdImages(2, modeltypes.ImagesWithheldNoVision)
	breakdown = a.buildContextBreakdown(chatCtx, mc, 0, nil)
	require.Equal(t, 2, breakdown.ImagesWithheld)
	require.Equal(t, modeltypes.ImagesWithheldNoVision, breakdown.ImagesWithheldReason)

	breakdown = a.buildContextBreakdown(loopChatContext(), mc, 0, nil)
	require.Zero(t, breakdown.ImagesWithheld)
	require.Empty(t, breakdown.ImagesWithheldReason)
}

func TestWithholdImages_FirstReasonStands(t *testing.T) {
	t.Parallel()
	chatCtx := loopChatContext()
	chatCtx.withholdImages(0, modeltypes.ImagesWithheldNoVision)
	require.Zero(t, chatCtx.imagesWithheld)
	require.Empty(t, chatCtx.imagesWithheldReason, "nothing withheld, no reason")

	chatCtx.withholdImages(1, modeltypes.ImagesWithheldSafetyRefusal)
	chatCtx.withholdImages(2, modeltypes.ImagesWithheldNoVision)
	require.Equal(t, 3, chatCtx.imagesWithheld)
	require.Equal(t, modeltypes.ImagesWithheldSafetyRefusal, chatCtx.imagesWithheldReason)
}

func TestCountImageAttachments(t *testing.T) {
	t.Parallel()
	require.Equal(t, 2, countImageAttachments([]*models.FileAttachment{
		{FileType: "image/png"}, {FileType: "application/pdf"}, nil, {FileType: "image/jpeg"},
	}))
	require.Zero(t, countImageAttachments(nil))
}

func TestCheckpointSummaryWithoutImages(t *testing.T) {
	t.Parallel()
	withImage := &provider.ModelContext{}
	withImage.AppendUserMessage(provider.RoleUser, "look", []provider.UserMessageImage{{RawBytes: []byte("png"), MediaType: "image/png"}}, false)
	textOnly := &provider.ModelContext{}
	textOnly.Append(provider.SegmentKindUserMessage, provider.RoleUser, "hi", false)
	respID := "resp_1"

	stripped, ok := checkpointSummaryWithoutImages(checkpointSummarySource{ModelContext: withImage, AssistantReply: "reply"})
	require.True(t, ok)
	require.False(t, modelContextHasImages(stripped.ModelContext))
	require.True(t, modelContextHasImages(withImage), "the source context is not modified")
	require.Equal(t, "reply", stripped.AssistantReply)

	stripped, ok = checkpointSummaryWithoutImages(checkpointSummarySource{PreviousResponseID: &respID, ModelContext: textOnly})
	require.True(t, ok, "a chained summary is retried from the explicit context")
	require.Nil(t, stripped.PreviousResponseID)

	_, ok = checkpointSummaryWithoutImages(checkpointSummarySource{ModelContext: textOnly})
	require.False(t, ok, "same request again: no retry")
	_, ok = checkpointSummaryWithoutImages(checkpointSummarySource{PreviousResponseID: &respID})
	require.False(t, ok, "nothing to rebuild from")
}

func TestSummarizeConversationForCheckpoint_RetriesARefusalWithoutImages(t *testing.T) {
	t.Parallel()
	var (
		mu     sync.Mutex
		bodies []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		n := len(bodies)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"moderation_blocked","message":"Your request was rejected by the safety system.","type":"invalid_request_error"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"resp_2","object":"response","created_at":1,"status":"completed","model":"m","output":[{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"the summary","annotations":[]}]}]}`))
	}))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newNoRetryTestOpenAIProvider(srv.URL)}
	mc := &provider.ModelContext{}
	mc.AppendUserMessage(provider.RoleUser, "what do you think of this picture?", []provider.UserMessageImage{{RawBytes: []byte("png"), MediaType: "image/png"}}, false)
	respID := "resp_1"
	chatCtx := &chatContext{chat: &models.Chat{ID: uuid.New()}}

	summary, err := a.summarizeConversationForCheckpoint(context.Background(), uuid.New(), chatCtx, checkpointSummarySource{
		PreviousResponseID: &respID, ModelContext: mc, AssistantReply: "a lovely picture",
	})

	require.NoError(t, err)
	require.Equal(t, "the summary", summary)
	require.Len(t, bodies, 2)
	require.Contains(t, bodies[0], `"previous_response_id":"resp_1"`)
	var retry map[string]any
	require.NoError(t, json.Unmarshal([]byte(bodies[1]), &retry))
	require.NotContains(t, retry, "previous_response_id", "the retry does not chain onto the refused context")
	require.NotContains(t, bodies[1], "input_image")
	require.True(t, strings.Contains(bodies[1], "what do you think of this picture?"), "it summarizes the same turn")
}
