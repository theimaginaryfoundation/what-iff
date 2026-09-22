package provider

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"go.uber.org/zap"
)

// GenerateImagePNGBase64 generates a single PNG image via the ImageEngine model and returns the base64 payload.
//
// The returned string is the raw base64 data (no data: prefix).
func (a *OpenAIProvider) GenerateImagePNGBase64(ctx context.Context, prompt string) (string, error) {
	return a.GenerateImagePNGBase64WithOptions(ctx, prompt, ImageQualityLow, ImageAspectRatioSquare)
}

// GenerateImagePNGBase64WithQuality generates a single square PNG image with the requested quality and returns the base64 payload.
//
// The returned string is the raw base64 data (no data: prefix).
func (a *OpenAIProvider) GenerateImagePNGBase64WithQuality(ctx context.Context, prompt string, quality ImageQuality) (string, error) {
	return a.GenerateImagePNGBase64WithOptions(ctx, prompt, quality, ImageAspectRatioSquare)
}

// GenerateImagePNGBase64WithOptions generates a single PNG image with the requested quality and aspect ratio and returns the base64 payload.
//
// The returned string is the raw base64 data (no data: prefix).
func (a *OpenAIProvider) GenerateImagePNGBase64WithOptions(ctx context.Context, prompt string, quality ImageQuality, aspectRatio ImageAspectRatio) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	resp, err := a.oaiClient.Images.Generate(ctx, buildImageGenerateParams(prompt, quality, aspectRatio))
	if err != nil {
		return "", err
	}
	return a.firstImageB64(resp)
}

// EditImagePNGBase64WithQuality generates a single square PNG image conditioned on a reference image
// (the images edit endpoint, no mask) and returns the base64 payload. The reference steers style and
// character design; the prompt still decides composition.
//
// The returned string is the raw base64 data (no data: prefix).
func (a *OpenAIProvider) EditImagePNGBase64WithQuality(ctx context.Context, prompt string, quality ImageQuality, referenceImage []byte, referenceMIME string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if len(referenceImage) == 0 {
		return "", fmt.Errorf("reference image is required")
	}

	resp, err := a.oaiClient.Images.Edit(ctx, buildImageEditParams(prompt, quality, referenceImage, referenceMIME))
	if err != nil {
		return "", err
	}
	return a.firstImageB64(resp)
}

func (a *OpenAIProvider) firstImageB64(resp *openai.ImagesResponse) (string, error) {
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

func buildImageGenerateParams(prompt string, quality ImageQuality, aspectRatio ImageAspectRatio) openai.ImageGenerateParams {
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

	// Map the requested aspect ratio to the model's supported pixel dimensions.
	// Default to square for any unrecognized value.
	size := "1024x1024"
	switch aspectRatio {
	case ImageAspectRatioLandscape:
		size = "1536x1024"
	case ImageAspectRatioPortrait:
		size = "1024x1536"
	default:
		size = "1024x1024"
	}

	return openai.ImageGenerateParams{
		Prompt:       prompt,
		Model:        openai.ImageModel(ImageEngine),
		Quality:      oaiQuality,
		Moderation:   openai.ImageGenerateParamsModerationLow,
		Size:         openai.ImageGenerateParamsSize(size),
		OutputFormat: openai.ImageGenerateParamsOutputFormat("png"),
	}
}

// referenceImageFilenames maps accepted reference MIME types to an upload filename; the edit
// endpoint sniffs the multipart filename/content type, so both must agree.
var referenceImageFilenames = map[string]string{
	"image/png":  "reference.png",
	"image/jpeg": "reference.jpg",
	"image/webp": "reference.webp",
}

func buildImageEditParams(prompt string, quality ImageQuality, referenceImage []byte, referenceMIME string) openai.ImageEditParams {
	oaiQuality := openai.ImageEditParamsQualityLow
	switch quality {
	case ImageQualityMedium:
		oaiQuality = openai.ImageEditParamsQualityMedium
	case ImageQualityHigh:
		oaiQuality = openai.ImageEditParamsQualityHigh
	}

	mime := strings.ToLower(strings.TrimSpace(referenceMIME))
	filename, ok := referenceImageFilenames[mime]
	if !ok {
		mime, filename = "image/png", "reference.png"
	}

	return openai.ImageEditParams{
		Prompt: prompt,
		Image: openai.ImageEditParamsImageUnion{
			OfFile: openai.File(bytes.NewReader(referenceImage), filename, mime),
		},
		Model:        openai.ImageModel(ImageEngine),
		Quality:      oaiQuality,
		Size:         openai.ImageEditParamsSize1024x1024,
		OutputFormat: openai.ImageEditParamsOutputFormatPNG,
	}
}
