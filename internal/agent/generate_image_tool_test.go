package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// newNoRetryTestOpenAIProvider builds a provider whose client never retries, so
// per-request success/failure sequencing in tests is deterministic.
func newNoRetryTestOpenAIProvider(baseURL string) *provider.OpenAIProvider {
	client := openai.NewClient(option.WithAPIKey("test-key"), option.WithBaseURL(baseURL), option.WithMaxRetries(0))
	return provider.NewOpenAIProvider(nil, &client, nil, nil)
}

// --- marshalGenerateImageToolResult ---

func TestMarshalGenerateImageToolResult_RoundTrips(t *testing.T) {
	t.Parallel()
	out, err := marshalGenerateImageToolResult(generateImageToolResult{Success: true, Prompt: "a cat"})
	require.NoError(t, err)
	var got generateImageToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.True(t, got.Success)
	require.Equal(t, "a cat", got.Prompt)
}

// --- clampGenerateImageCount ---

func TestClampGenerateImageCount(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want int
	}{
		{-1, 1},
		{0, 1},
		{1, 1},
		{4, 4},
		{5, 4},
		{100, 4},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, clampGenerateImageCount(tc.in))
	}
}

// --- parseGenerateImageQuality ---

func TestParseGenerateImageQuality(t *testing.T) {
	t.Parallel()
	low := "low"
	medium := "MEDIUM"
	high := " high "
	empty := ""
	bogus := "ultra"

	cases := []struct {
		name    string
		in      *string
		want    provider.ImageQuality
		wantErr bool
	}{
		{"nil defaults to low", nil, provider.ImageQualityLow, false},
		{"empty defaults to low", &empty, provider.ImageQualityLow, false},
		{"low", &low, provider.ImageQualityLow, false},
		{"medium case-insensitive", &medium, provider.ImageQualityMedium, false},
		{"high trimmed", &high, provider.ImageQualityHigh, false},
		{"invalid", &bogus, provider.ImageQualityLow, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseGenerateImageQuality(tc.in)
			require.Equal(t, tc.want, got)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// --- generateImageTool ---

func imagesGenerateJSONServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func imagesSuccessBody(b64 string) string {
	return `{"created":1,"data":[{"b64_json":"` + b64 + `"}]}`
}

func TestGenerateImageTool_InvalidArgsJSONReturnsErrorResult(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	out, atts, err := a.generateImageTool(context.Background(), &models.Chat{}, []byte("not json"))
	require.NoError(t, err)
	require.Nil(t, atts)
	var result generateImageToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Contains(t, result.Error, "invalid arguments")
}

func TestGenerateImageTool_EmptyPromptReturnsErrorResult(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	args, err := json.Marshal(generateImageToolArgs{Prompt: "   "})
	require.NoError(t, err)
	out, atts, err := a.generateImageTool(context.Background(), &models.Chat{}, args)
	require.NoError(t, err)
	require.Nil(t, atts)
	var result generateImageToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Equal(t, "prompt is required", result.Error)
}

func TestGenerateImageTool_InvalidQualityReturnsErrorResult(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	badQuality := "ultra"
	args, err := json.Marshal(generateImageToolArgs{Prompt: "a cat", Quality: &badQuality})
	require.NoError(t, err)
	out, atts, err := a.generateImageTool(context.Background(), &models.Chat{}, args)
	require.NoError(t, err)
	require.Nil(t, atts)
	var result generateImageToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Contains(t, result.Error, "invalid quality")
}

func TestGenerateImageTool_SuccessSingleImage(t *testing.T) {
	t.Parallel()
	srv := imagesGenerateJSONServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(imagesSuccessBody("aGVsbG8=")))
	})
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	prefix := "my prefix/weird\\name"
	args, err := json.Marshal(generateImageToolArgs{Prompt: "a cat", FilenamePrefix: &prefix})
	require.NoError(t, err)

	out, atts, err := a.generateImageTool(context.Background(), &models.Chat{UserID: uuid.New(), ID: uuid.New()}, args)
	require.NoError(t, err)
	require.Len(t, atts, 1)
	require.Equal(t, "image/png", atts[0].FileType)
	require.Contains(t, atts[0].Name, "my_prefix_weird_name")

	var result generateImageToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.True(t, result.Success)
	require.Equal(t, 1, result.Count)
	require.Len(t, result.Images, 1)
}

func TestGenerateImageTool_AllRequestsFailReturnsToolError(t *testing.T) {
	t.Parallel()
	srv := imagesGenerateJSONServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newNoRetryTestOpenAIProvider(srv.URL)}
	args, err := json.Marshal(generateImageToolArgs{Prompt: "a cat"})
	require.NoError(t, err)

	out, atts, err := a.generateImageTool(context.Background(), &models.Chat{UserID: uuid.New(), ID: uuid.New()}, args)
	require.Error(t, err)
	require.Empty(t, out)
	require.Nil(t, atts)
	require.Contains(t, err.Error(), "failed to generate image")
}

func TestGenerateImageTool_SafetyViolationPersistsEventAndReturnsAssistantMessage(t *testing.T) {
	t.Parallel()
	srv := imagesGenerateJSONServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"moderation_blocked","message":"rejected by the safety system"}}`))
	})
	defer srv.Close()

	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO .*safety_violation_events.*").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL), ds: ds}
	args, err := json.Marshal(generateImageToolArgs{Prompt: "a cat"})
	require.NoError(t, err)

	chat := &models.Chat{UserID: uuid.New(), ID: uuid.New(), Name: "chat name"}
	out, atts, err := a.generateImageTool(context.Background(), chat, args)
	require.NoError(t, err)
	require.Nil(t, atts)

	var result generateImageToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Equal(t, safetyViolationAssistantMessage, result.Error)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGenerateImageTool_PartialFailureReportsCountsInResult(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := imagesGenerateJSONServer(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(imagesSuccessBody("aGVsbG8=")))
	})
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newNoRetryTestOpenAIProvider(srv.URL)}
	count := 2
	args, err := json.Marshal(generateImageToolArgs{Prompt: "a cat", Count: &count})
	require.NoError(t, err)

	out, atts, err := a.generateImageTool(context.Background(), &models.Chat{UserID: uuid.New(), ID: uuid.New()}, args)
	require.NoError(t, err)
	require.Len(t, atts, 1)

	var result generateImageToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.True(t, result.Success)
	require.Equal(t, 2, result.Count)
	require.Contains(t, result.Error, "some requests failed")
}
