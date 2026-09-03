package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
)

func newExpressionReferenceTestProvider(baseURL string) *OpenAIProvider {
	client := openai.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(baseURL),
		option.WithMaxRetries(0),
	)
	return NewOpenAIProvider(nil, &client, nil, nil)
}

func TestOpenAIExpressionReferenceEditPassesCanonicalImageThrough(t *testing.T) {
	canonical := []byte("canonical-image-bytes")
	var sawCanonical bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/images/edits", r.URL.Path)
		require.NoError(t, r.ParseMultipartForm(8<<20))

		for field, files := range r.MultipartForm.File {
			if !strings.HasPrefix(field, "image") {
				continue
			}
			for _, header := range files {
				f, err := header.Open()
				require.NoError(t, err)
				got, err := io.ReadAll(f)
				require.NoError(t, err)
				require.NoError(t, f.Close())
				if string(got) == string(canonical) {
					sawCanonical = true
				}
			}
		}

		require.True(t, sawCanonical, "canonical image bytes must reach the provider image-edit request")
		require.Contains(t, r.FormValue("prompt"), "happy")
		require.Contains(t, r.FormValue("prompt"), "thin rectangular glasses")
		require.Equal(t, "high", r.FormValue("input_fidelity"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"b3V0cHV0LXBuZw=="}]}`))
	}))
	defer srv.Close()

	p := newExpressionReferenceTestProvider(srv.URL)
	result, err := p.GenerateExpressionFromReference(context.Background(), ExpressionReferenceRequest{
		CanonicalImage:     canonical,
		CanonicalImageMIME: "image/png",
		Expression:         "happy",
		Constraints:        "short dark hair; thin rectangular glasses",
	})
	require.NoError(t, err)
	require.Equal(t, []byte("output-png"), result.PNG)
	require.Equal(t, ExpressionGenerationMethodReferenceEdit, result.GenerationMethod)
	require.Equal(t, ReferenceCapabilitySupported, result.ReferenceCapability)
	require.Equal(t, "openai", result.Provider)
}

func TestOpenAIExpressionReferenceEditRejectsMissingCanonicalImage(t *testing.T) {
	p := &OpenAIProvider{}
	_, err := p.GenerateExpressionFromReference(context.Background(), ExpressionReferenceRequest{
		Expression: "happy",
	})
	require.ErrorContains(t, err, "canonical image is required")
}

func TestOpenAIExpressionReferenceCapabilityIsExplicit(t *testing.T) {
	p := &OpenAIProvider{}
	require.Equal(t, ReferenceCapabilitySupported, p.ExpressionReferenceCapability())
	require.Equal(t, "openai", p.ExpressionProviderName())
}
