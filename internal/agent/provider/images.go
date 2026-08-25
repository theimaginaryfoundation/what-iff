package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"go.uber.org/zap"
)

// GenerateImagePNGBase64 generates a single PNG image via gpt-image-1 and returns the base64 payload.
//
// The returned string is the raw base64 data (no data: prefix).
func (a *OpenAIProvider) GenerateImagePNGBase64(ctx context.Context, prompt string) (string, error) {
	return a.GenerateImagePNGBase64WithQuality(ctx, prompt, ImageQualityLow)
}

// GenerateImagePNGBase64WithQuality generates a single PNG image with the requested quality and returns the base64 payload.
//
// The returned string is the raw base64 data (no data: prefix).
func (a *OpenAIProvider) GenerateImagePNGBase64WithQuality(ctx context.Context, prompt string, quality ImageQuality) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	resp, err := a.oaiClient.Images.Generate(ctx, buildImageGenerateParams(prompt, quality))
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("nil images response")
	}
	if len(resp.Data) < 1 {
		a.zapLog().Warn("images response contained no data", zap.String("raw", resp.RawJSON()))
		return "", fmt.Errorf("images response contained no data")
	}
	b64 := strings.TrimSpace(resp.Data[0].B64JSON)
	if b64 == "" {
		a.zapLog().Warn("images response contained empty b64_json", zap.String("raw", resp.RawJSON()))
		return "", fmt.Errorf("images response contained empty image data")
	}
	return b64, nil
}

func buildImageGenerateParams(prompt string, quality ImageQuality) openai.ImageGenerateParams {
	// Default low to avoid accidental cost spikes (quality="auto" can select expensive tiers).
	oaiQuality := openai.ImageGenerateParamsQualityLow
	switch quality {
	case ImageQualityMedium:
		oaiQuality = openai.ImageGenerateParamsQualityMedium
	case ImageQualityHigh:
		oaiQuality = openai.ImageGenerateParamsQualityHigh
	default:
		oaiQuality = openai.ImageGenerateParamsQualityLow
	}

	return openai.ImageGenerateParams{
		Prompt:       prompt,
		Model:        openai.ImageModel(ImageEngine),
		Quality:      oaiQuality,
		Moderation:   openai.ImageGenerateParamsModerationLow,
		Size:         openai.ImageGenerateParamsSize("1024x1024"),
		OutputFormat: openai.ImageGenerateParamsOutputFormat("png"),
	}
}
