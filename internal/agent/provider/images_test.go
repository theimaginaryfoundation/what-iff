package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateImagePNGBase64_EmptyPrompt(t *testing.T) {
	p := &OpenAIProvider{}
	_, err := p.GenerateImagePNGBase64(context.Background(), "   ")
	require.Error(t, err)
}

func TestBuildImageGenerateParams_AspectRatioMapsToSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		aspectRatio ImageAspectRatio
		wantSize    string
	}{
		{"square", ImageAspectRatioSquare, "1024x1024"},
		{"landscape", ImageAspectRatioLandscape, "1536x1024"},
		{"portrait", ImageAspectRatioPortrait, "1024x1536"},
		{"unknown defaults to square", ImageAspectRatio("bogus"), "1024x1024"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			params := buildImageGenerateParams("a cat", ImageQualityLow, tc.aspectRatio)
			require.Equal(t, tc.wantSize, string(params.Size))
			require.Equal(t, ImageEngine, string(params.Model))
		})
	}
}

func TestEditImagePNGBase64WithQuality_Validation(t *testing.T) {
	t.Parallel()
	p := &OpenAIProvider{}
	_, err := p.EditImagePNGBase64WithQuality(context.Background(), "  ", ImageQualityMedium, []byte{1}, "image/png")
	require.ErrorContains(t, err, "prompt is required")
	_, err = p.EditImagePNGBase64WithQuality(context.Background(), "a cat", ImageQualityMedium, nil, "image/png")
	require.ErrorContains(t, err, "reference image is required")
}

func TestBuildImageEditParams(t *testing.T) {
	t.Parallel()
	params := buildImageEditParams("a cat", ImageQualityMedium, []byte{1, 2, 3}, "image/jpeg")
	require.Equal(t, ImageEngine, string(params.Model))
	require.Equal(t, "medium", string(params.Quality))
	require.Equal(t, "1024x1024", string(params.Size))
	require.Equal(t, "png", string(params.OutputFormat))
	named, ok := params.Image.OfFile.(interface{ Filename() string })
	require.True(t, ok)
	require.Equal(t, "reference.jpg", named.Filename())

	// Unknown MIME types fall back to PNG naming.
	params = buildImageEditParams("a cat", ImageQualityLow, []byte{1}, "image/heic")
	named, ok = params.Image.OfFile.(interface{ Filename() string })
	require.True(t, ok)
	require.Equal(t, "reference.png", named.Filename())
	require.Equal(t, "low", string(params.Quality))
}
