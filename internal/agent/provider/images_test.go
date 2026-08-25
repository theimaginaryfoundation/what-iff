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
