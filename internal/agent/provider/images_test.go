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
