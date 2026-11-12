package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
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

	params := buildImageGenerateParams(prompt, quality, aspectRatio)
	call := startGenAICall(ctx, a.tel, telemetry.DependencyOpenAI, string(params.Model), genAIOpGenerateImage)
	resp, err := a.oaiClient.Images.Generate(ctx, params)
	endImageCall(call, resp, err)
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

	return a.editImage(ctx, buildImageEditParams(prompt, quality, referenceImage, referenceMIME))
}

// editImage sends one edit request and returns the first image's base64 payload.
func (a *OpenAIProvider) editImage(ctx context.Context, params openai.ImageEditParams) (string, error) {
	call := startGenAICall(ctx, a.tel, telemetry.DependencyOpenAI, string(params.Model), genAIOpEditImage)
	resp, err := a.oaiClient.Images.Edit(ctx, params)
	endImageCall(call, resp, err)
	if err != nil {
		return "", err
	}
	return a.firstImageB64(resp)
}

// endImageCall records an Images API call's duration and, when reported, its token usage.
func endImageCall(call *genAICall, resp *openai.ImagesResponse, err error) {
	if err == nil && resp != nil {
		call.recordUsage(genAIUsage{Input: resp.Usage.InputTokens, Output: resp.Usage.OutputTokens})
	}
	call.end(err)
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
	return openai.ImageEditParams{
		Prompt: prompt,
		Image: openai.ImageEditParamsImageUnion{
			OfFile: referenceImageFile(referenceImage, referenceMIME, "reference"),
		},
		Model:        openai.ImageModel(ImageEngine),
		Quality:      imageEditQuality(quality),
		Size:         openai.ImageEditParamsSize1024x1024,
		OutputFormat: openai.ImageEditParamsOutputFormatPNG,
	}
}

// buildImageEditRequestParams is an edit with every input image and, when set, an alpha mask
// for the first. It sends no input_fidelity: ImageEngine refuses the parameter
// (invalid_input_fidelity_model), and keeps what is outside the mask without it.
func buildImageEditRequestParams(prompt string, quality ImageQuality, aspectRatio ImageAspectRatio, images []ImageInput, alphaMask []byte) openai.ImageEditParams {
	files := make([]io.Reader, 0, len(images))
	for i, img := range images {
		files = append(files, referenceImageFile(img.Data, inputImageMIME(img), fmt.Sprintf("image-%d", i+1)))
	}
	size := openai.ImageEditParamsSize1024x1024
	switch aspectRatio {
	case ImageAspectRatioLandscape:
		size = openai.ImageEditParamsSize1536x1024
	case ImageAspectRatioPortrait:
		size = openai.ImageEditParamsSize1024x1536
	}
	params := openai.ImageEditParams{
		Prompt:       prompt,
		Image:        openai.ImageEditParamsImageUnion{OfFileArray: files},
		Model:        openai.ImageModel(ImageEngine),
		Quality:      imageEditQuality(quality),
		Size:         size,
		OutputFormat: openai.ImageEditParamsOutputFormatPNG,
	}
	if len(alphaMask) > 0 {
		params.Mask = openai.File(bytes.NewReader(alphaMask), "mask.png", "image/png")
	}
	return params
}

func imageEditQuality(quality ImageQuality) openai.ImageEditParamsQuality {
	switch quality {
	case ImageQualityMedium:
		return openai.ImageEditParamsQualityMedium
	case ImageQualityHigh:
		return openai.ImageEditParamsQualityHigh
	default:
		return openai.ImageEditParamsQualityLow
	}
}

// referenceImageFile is an input image as a multipart file whose name and type agree.
func referenceImageFile(data []byte, mime, stem string) io.Reader {
	mime = strings.ToLower(strings.TrimSpace(mime))
	filename, ok := referenceImageFilenames[mime]
	if !ok {
		mime, filename = "image/png", "reference.png"
	}
	if stem != "reference" {
		filename = stem + filename[strings.LastIndex(filename, "."):]
	}
	return openai.File(bytes.NewReader(data), filename, mime)
}
