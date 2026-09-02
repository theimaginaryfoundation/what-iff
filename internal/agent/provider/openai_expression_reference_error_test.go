package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
)

func TestOpenAIExpressionReferenceEditCarriesCanonicalFileMetadata(t *testing.T) {
	canonical := []byte("canonical-png-bytes")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/images/edits", r.URL.Path)
		require.NoError(t, r.ParseMultipartForm(8<<20))

		var imageFound bool
		for field, files := range r.MultipartForm.File {
			if !strings.HasPrefix(field, "image") {
				continue
			}
			for _, header := range files {
				imageFound = true
				require.Equal(t, "canonical.png", header.Filename)
				require.Equal(t, "image/png", header.Header.Get("Content-Type"))
				f, err := header.Open()
				require.NoError(t, err)
				got, err := io.ReadAll(f)
				require.NoError(t, err)
				require.NoError(t, f.Close())
				require.Equal(t, canonical, got)
			}
		}
		require.True(t, imageFound)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"b3V0cHV0"}]}`))
	}))
	defer srv.Close()

	p := newExpressionReferenceTestProvider(srv.URL)
	_, err := p.GenerateExpressionFromReference(context.Background(), ExpressionReferenceRequest{
		CanonicalImage:     canonical,
		CanonicalImageMIME: "image/png",
		Expression:         "happy",
		Constraints:        "preserve glasses",
	})
	require.NoError(t, err)
}

func TestOpenAIExpressionReferenceEditRejectsMissingExpressionAndProvider(t *testing.T) {
	p := &OpenAIProvider{}

	_, err := p.GenerateExpressionFromReference(context.Background(), ExpressionReferenceRequest{
		CanonicalImage: []byte("canonical"),
	})
	require.ErrorContains(t, err, "expression is required")

	_, err = p.GenerateExpressionFromReference(context.Background(), ExpressionReferenceRequest{
		CanonicalImage: []byte("canonical"),
		Expression:     "happy",
	})
	require.ErrorContains(t, err, "openai image provider is not configured")
}

func TestOpenAIExpressionReferenceEditRejectsMalformedProviderResponses(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{"no data", http.StatusOK, `{"created":1,"data":[]}`, "images edit response contained no data"},
		{"empty image", http.StatusOK, `{"created":1,"data":[{"b64_json":""}]}`, "images edit response contained empty image data"},
		{"invalid base64", http.StatusOK, `{"created":1,"data":[{"b64_json":"%%%"}]}`, "decode edited image"},
		{"provider error", http.StatusInternalServerError, `{"error":{"message":"boom","type":"server_error"}}`, "boom"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			p := newExpressionReferenceTestProvider(srv.URL)
			_, err := p.GenerateExpressionFromReference(context.Background(), ExpressionReferenceRequest{
				CanonicalImage:     []byte("canonical"),
				CanonicalImageMIME: "image/png",
				Expression:         "happy",
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestBuildExpressionReferenceEditParamsMapsQualityAndPrompt(t *testing.T) {
	req := ExpressionReferenceRequest{
		CanonicalImage: []byte("canonical"),
		Expression:     " confused ",
		Constraints:    " blue hair ",
	}

	low := buildExpressionReferenceEditParams(req, ImageQualityLow)
	medium := buildExpressionReferenceEditParams(req, ImageQualityMedium)
	high := buildExpressionReferenceEditParams(req, ImageQualityHigh)

	require.Equal(t, openai.ImageEditParamsQualityLow, low.Quality)
	require.Equal(t, openai.ImageEditParamsQualityMedium, medium.Quality)
	require.Equal(t, openai.ImageEditParamsQualityHigh, high.Quality)
	require.Equal(t, openai.ImageEditParamsInputFidelityHigh, medium.InputFidelity)
	require.Equal(t, openai.ImageEditParamsOutputFormatPNG, medium.OutputFormat)
	require.Equal(t, openai.ImageEditParamsSize1024x1024, medium.Size)
	require.Contains(t, medium.Prompt, "confused")
	require.Contains(t, medium.Prompt, "blue hair")
}
