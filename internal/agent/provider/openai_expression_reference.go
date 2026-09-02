package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
)

const openAIExpressionProviderName = "openai"

func (a *OpenAIProvider) ExpressionReferenceCapability() ReferenceCapability {
	return ReferenceCapabilitySupported
}

func (a *OpenAIProvider) ExpressionProviderName() string {
	return openAIExpressionProviderName
}

// GenerateExpressionFromReference uses the provider's native image-edit endpoint
// with high input fidelity. The canonical portrait is sent as image data; the
// text constraints are supplemental and are never substituted for the image.
func (a *OpenAIProvider) GenerateExpressionFromReference(ctx context.Context, req ExpressionReferenceRequest) (ExpressionReferenceResult, error) {
	if len(req.CanonicalImage) == 0 {
		return ExpressionReferenceResult{}, fmt.Errorf("canonical image is required")
	}
	if strings.TrimSpace(req.Expression) == "" {
		return ExpressionReferenceResult{}, fmt.Errorf("expression is required")
	}
	if a == nil || a.oaiClient == nil {
		return ExpressionReferenceResult{}, fmt.Errorf("openai image provider is not configured")
	}

	params := buildExpressionReferenceEditParams(req, ImageQualityMedium)
	resp, err := a.oaiClient.Images.Edit(ctx, params)
	if err != nil {
		return ExpressionReferenceResult{}, err
	}
	if resp == nil {
		return ExpressionReferenceResult{}, fmt.Errorf("nil images edit response")
	}
	if len(resp.Data) < 1 {
		return ExpressionReferenceResult{}, fmt.Errorf("images edit response contained no data")
	}
	b64 := strings.TrimSpace(resp.Data[0].B64JSON)
	if b64 == "" {
		return ExpressionReferenceResult{}, fmt.Errorf("images edit response contained empty image data")
	}
	pngBytes, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ExpressionReferenceResult{}, fmt.Errorf("decode edited image: %w", err)
	}

	return ExpressionReferenceResult{
		PNG:                 pngBytes,
		GenerationMethod:    ExpressionGenerationMethodReferenceEdit,
		ReferenceCapability: ReferenceCapabilitySupported,
		Provider:             openAIExpressionProviderName,
	}, nil
}

func buildExpressionReferenceEditParams(req ExpressionReferenceRequest, quality ImageQuality) openai.ImageEditParams {
	prompt := strings.TrimSpace(fmt.Sprintf(`Preserve the exact visual identity of the character in the supplied canonical portrait.
Keep the same face, facial proportions, apparent age, hair, eyes, glasses/accessories, clothing design, palette, and art style.
Change only the facial expression and the minimum head/shoulder pose needed to communicate: %s.
Do not reinterpret, redesign, age, gender-swap, or replace the character. Produce one square head-and-shoulders portrait with no text, grid, collage, or extra characters.

Supplemental constraints from the personality: %s`, strings.TrimSpace(req.Expression), strings.TrimSpace(req.Constraints)))

	oaiQuality := openai.ImageEditParamsQualityLow
	switch quality {
	case ImageQualityMedium:
		oaiQuality = openai.ImageEditParamsQualityMedium
	case ImageQualityHigh:
		oaiQuality = openai.ImageEditParamsQualityHigh
	}

	return openai.ImageEditParams{
		Image: openai.ImageEditParamsImageUnion{
			OfFile: bytes.NewReader(req.CanonicalImage),
		},
		Prompt:        prompt,
		InputFidelity: openai.ImageEditParamsInputFidelityHigh,
		Model:         openai.ImageModel(ImageEngine),
		OutputFormat:  openai.ImageEditParamsOutputFormatPNG,
		Quality:       oaiQuality,
		Size:          openai.ImageEditParamsSize1024x1024,
	}
}

var _ ExpressionImageProvider = (*OpenAIProvider)(nil)
