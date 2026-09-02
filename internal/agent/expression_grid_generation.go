package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// imageStyleNone aliases models.ImageStyleNone for use within this package.
const imageStyleNone = models.ImageStyleNone

// maxExpressionReferenceImageBytes limits reference images inlined as data URLs for vision calls.
const maxExpressionReferenceImageBytes = 3 * 1024 * 1024 // 3MB

// capExpressionReferenceImage drops reference bytes that exceed maxExpressionReferenceImageBytes.
func capExpressionReferenceImage(bytes []byte, mime string) ([]byte, string) {
	if len(bytes) <= maxExpressionReferenceImageBytes {
		return bytes, mime
	}
	return nil, ""
}

// ExpressionGridKeys is the 3×3 grid layout in row-major order (includes "thinking" at index 8).
var ExpressionGridKeys = []string{
	"happy", "content", "sad",
	"angry", "surprised", "confused",
	"tired", "in-love", "thinking",
}

const expressionLikenessInstructions = `You are a portrait art director. Given a personality system prompt, produce a single prose paragraph suitable for image generation. 
Preserve any explicitly established canonical appearance — including species, nonhuman traits, age, and gender presentation — as written. 
Treat metaphorical or poetic language as tone and mood instruction, not as species or morphology. Unless the prompt explicitly establishes nonhuman or fantastical traits, do not invent animal features, markings, or symbolic morphology. 
Express personality through facial expression, posture, clothing, styling, lighting, and composition. Include physical appearance, build, distinguishing features, art style, and a restrained palette description with 2–3 color anchors. 
Output only the prose paragraph.`

const expressionGridCanvasInstructions = `Render as a single square image: a 3×3 grid of nine equally-sized portrait panels on one flush canvas.

Requirements:
- Nine panels in a perfect 3×3 layout (row-major: happy | content | sad; angry | surprised | confused; tired | in-love | thinking).
- Panels must meet edge-to-edge with NO gutters, NO margins, NO white strips or blank gaps between cells — dividing lines (if any) must be hair-thin and drawn inside the art, not empty whitespace.
- Do not add outer margins or padding around the grid; artwork fills the square edge-to-edge.
- The same character appears in every panel with consistent design; square composition, readable faces, consistent lighting and style.

Match the character design described in the preceding paragraph.`

// GenerateDefaultExpressionGrid runs likeness (nano) + one medium-quality square image generation,
// splices the 3×3 grid into PNG cells, uploads all nine cells, and upserts each expression key.
// When EXPRESSION_REFERENCE_GENERATION_ENABLED=true, Phase 1 then independently regenerates
// happy/content from the immutable cover portrait through a provider-native reference operation.
// The grid remains the deliberate fallback for unsupported providers and the other seven slots.
//
// Quota: personality tooling is not quota-metered. Reference-conditioned Phase 1 adds up to two
// medium-quality image-edit calls when the feature flag is enabled and a canonical cover is available.
//
// Semantics / retries:
//   - Safe to call multiple times: UpsertPersonalityExpression overwrites rows for the same keys.
//   - Partial failure: if a mid-loop step fails, earlier cells may already be persisted while later keys
//     are unchanged — callers may retry; subsequent runs overwrite keys that succeed again.
//   - HTTP handlers may skip work when all default keys already have images unless force=true (see personality handler).
func (a *Agent) GenerateDefaultExpressionGrid(ctx context.Context, userID, personalityID uuid.UUID) ([]models.PersonalityExpression, error) {
	if a == nil || a.ds == nil || a.OpenAIProvider == nil {
		return nil, fmt.Errorf("expression grid: agent not configured")
	}
	// Mock/local mode: deliberate denial — grid generation is inference + image calls.
	if a.nonVendorLLM() {
		return nil, fmt.Errorf("expression grid generation is disabled under LLM_BACKEND=mock/local")
	}
	if a.fileStore == nil {
		return nil, fmt.Errorf("expression grid: file store not configured")
	}

	person, err := a.ds.GetPersonality(ctx, userID, personalityID)
	if err != nil {
		return nil, fmt.Errorf("get personality: %w", err)
	}
	if person == nil {
		return nil, fmt.Errorf("personality not found")
	}

	// Skip all generation when style is "none".
	if person.ImageStyle == imageStyleNone {
		return []models.PersonalityExpression{}, nil
	}

	ctx = telemetry.WithCallPath(ctx, telemetry.CallPathExpressionGrid)

	// Resolve canonical portrait bytes from the cover image when available. These
	// bytes serve two different purposes:
	//   1. likeness extraction for the legacy grid fallback, and
	//   2. the actual image input for the reference-conditioned quality path.
	var referenceImageBytes []byte
	var referenceImageMIME string
	if person.CoverImageID != nil {
		// GetFileAttachment already enforces user ownership; an error here means
		// the attachment is missing or cross-user, so we skip gracefully.
		coverAtt, err := a.ds.GetFileAttachment(ctx, userID, *person.CoverImageID)
		if err != nil {
			a.logger.Warn("expression grid: failed to load cover image for reference; proceeding without it",
				zap.String("cover_image_id", person.CoverImageID.String()),
				zap.Error(err))
		} else if coverAtt != nil {
			// ResolveAttachmentImageBytes returns (bytes, mimeType); key-miss errors are
			// logged internally at debug level, so we warn only when bytes are empty.
			rawBytes, rawMIME := storage.ResolveAttachmentImageBytes(ctx, a.logger, a.fileStore, userID, coverAtt, false)
			if len(rawBytes) > maxExpressionReferenceImageBytes {
				a.logger.Warn("expression grid: reference image exceeds size limit; proceeding without reference",
					zap.String("cover_image_id", person.CoverImageID.String()),
					zap.Int("bytes", len(rawBytes)),
					zap.Int("limit", maxExpressionReferenceImageBytes))
			} else if len(rawBytes) == 0 {
				a.logger.Warn("expression grid: cover image resolved to empty bytes; proceeding without reference",
					zap.String("cover_image_id", person.CoverImageID.String()))
			}
			referenceImageBytes, referenceImageMIME = capExpressionReferenceImage(rawBytes, rawMIME)
		}
	}

	likeness, err := a.inferExpressionGridLikeness(ctx, strings.TrimSpace(person.SystemPrompt), referenceImageBytes, referenceImageMIME)
	if err != nil {
		return nil, err
	}
	if likeness == "" {
		likeness = "A distinctive character portrait consistent with the personality, suitable for a grid of facial expressions."
		a.logger.Warn("expression grid: empty likeness segment; using fallback prose")
	}

	// This prose remains the primary appearance input for the legacy grid, but is
	// supplemental-only when the actual canonical image is sent through the
	// reference-conditioned provider path below.
	referenceConstraints := strings.TrimSpace(likeness)
	canvasInstructions := expressionGridCanvasInstructions
	if person.ImageStyle != "" && person.ImageStyle != "auto" {
		styleConstraint := "Art style: " + person.ImageStyle
		canvasInstructions += "\n\n" + styleConstraint
		referenceConstraints += "\n\n" + styleConstraint
	}

	fullPrompt := strings.TrimSpace(likeness) + "\n\n" + canvasInstructions
	if len(fullPrompt) > 16000 {
		fullPrompt = fullPrompt[:16000]
	}

	b64PNG, err := a.OpenAIProvider.GenerateImagePNGBase64WithQuality(ctx, fullPrompt, provider.ImageQualityMedium)
	if err != nil {
		return nil, fmt.Errorf("generate grid image: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(b64PNG)
	if err != nil {
		return nil, fmt.Errorf("decode generated image: %w", err)
	}

	cells, err := SlicePNGGrid3x3(raw)
	if err != nil {
		return nil, fmt.Errorf("slice grid: %w", err)
	}
	if len(cells) != 9 {
		return nil, fmt.Errorf("slice grid: expected 9 cells, got %d", len(cells))
	}

	expressionProvider := a.expressionImageProvider()
	referenceCapability, expressionProviderName := expressionProviderState(expressionProvider)
	for i := range ExpressionGridKeys {
		key := ExpressionGridKeys[i]
		receipt := expressionGenerationReceipt{
			PersonalityID:          personalityID,
			ExpressionKey:          key,
			CanonicalImageID:       person.CoverImageID,
			CanonicalImageVersion:  canonicalImageVersion(person.CoverImageID),
			GenerationMethod:       provider.ExpressionGenerationMethodGridFallback,
			ReferenceCapability:    referenceCapability,
			ReferenceInputSupplied: false,
			Provider:               expressionProviderName,
		}
		if err := a.uploadPersonalityExpressionCell(ctx, userID, personalityID, key, cells[i], receipt); err != nil {
			return nil, fmt.Errorf("expression %q: %w", key, err)
		}
	}

	// Phase 1 quality slice: overwrite only happy/content from independent edits of
	// the same immutable canonical portrait. If the configured adapter does not
	// truly support reference input, the grid assets remain in place and their
	// receipts continue to say grid_fallback.
	if err := a.maybeGeneratePhaseOneReferenceExpressions(
		ctx,
		userID,
		personalityID,
		person,
		referenceImageBytes,
		referenceImageMIME,
		referenceConstraints,
	); err != nil {
		return nil, fmt.Errorf("reference expression generation: %w", err)
	}

	out, err := a.ds.ListPersonalityExpressions(ctx, userID, personalityID)
	if err != nil {
		return nil, fmt.Errorf("list expressions after grid: %w", err)
	}
	return out, nil
}

// inferExpressionGridLikeness produces a prose paragraph describing the character's appearance
// for use as an image-generation prompt. When referenceImageBytes is non-nil the model receives
// the image as a vision input alongside the text prompt, grounding the description visually.
// referenceImageMIME is the MIME type of the image (e.g. "image/jpeg"); defaults to "image/png".
func (a *Agent) inferExpressionGridLikeness(ctx context.Context, systemPrompt string, referenceImageBytes []byte, referenceImageMIME string) (string, error) {
	referenceImageBytes, referenceImageMIME = capExpressionReferenceImage(referenceImageBytes, referenceImageMIME)
	if systemPrompt == "" && len(referenceImageBytes) == 0 {
		return "", nil
	}

	var input responses.ResponseNewParamsInputUnion

	if len(referenceImageBytes) > 0 {
		// Build a multipart user message with text + reference image.
		textPart := responses.ResponseInputContentParamOfInputText("Personality system prompt:\n\n" + systemPrompt)
		// mimeType carries the attachment's actual media type (e.g. "image/jpeg", "image/png").
		// Defaulting to "image/png" only as a last resort; correct type matters for
		// the vision model to decode the image properly.
		mimeType := referenceImageMIME
		if mimeType == "" {
			mimeType = "image/png"
		}
		dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(referenceImageBytes)
		imgPart := responses.ResponseInputContentUnionParam{
			OfInputImage: &responses.ResponseInputImageParam{
				ImageURL: openai.String(dataURL),
				Detail:   responses.ResponseInputImageDetailAuto,
			},
		}
		var parts responses.ResponseInputMessageContentListParam
		parts = append(parts, textPart, imgPart)
		input = responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{
				responses.ResponseInputItemParamOfMessage(parts, responses.EasyInputMessageRoleUser),
			},
		}
	} else {
		input = responses.ResponseNewParamsInputUnion{
			OfString: openai.String("Personality system prompt:\n\n" + systemPrompt),
		}
	}

	params := responses.ResponseNewParams{
		Model:           chatNameModel,
		Temperature:     openai.Float(0.4),
		MaxOutputTokens: openai.Int(768),
		Instructions:    openai.String(expressionLikenessInstructions),
		Input:           input,
	}
	resp, err := a.OpenAIProvider.CallWithRetry(ctx, params)
	if err != nil {
		return "", fmt.Errorf("likeness inference: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("likeness inference: nil response")
	}
	return strings.TrimSpace(resp.OutputText()), nil
}

// uploadPersonalityExpressionCell persists one expression image in S3 (not file_content),
// writes a machine-readable generation receipt next to it, and upserts the expression slot.
func (a *Agent) uploadPersonalityExpressionCell(
	ctx context.Context,
	userID, personalityID uuid.UUID,
	expressionKey string,
	pngBytes []byte,
	receipt expressionGenerationReceipt,
) error {
	if len(pngBytes) == 0 {
		return fmt.Errorf("empty cell png")
	}
	name := fmt.Sprintf("expression-%s.png", expressionKey)

	created, err := a.ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          name,
		FileType:      "image/png",
		PersonalityID: &personalityID,
	})
	if err != nil {
		return fmt.Errorf("create file attachment: %w", err)
	}
	if created == nil {
		return fmt.Errorf("create file attachment: nil model")
	}

	s3Key := storage.FileKeyForPersonality(userID, personalityID, created.ID, name)
	if err := a.fileStore.UploadFile(ctx, s3Key, pngBytes, "image/png"); err != nil {
		_ = a.ds.DeleteFileAttachment(ctx, userID, created.ID)
		return fmt.Errorf("upload full image: %w", err)
	}
	if err := a.ds.SetFileAttachmentS3Key(ctx, userID, created.ID, s3Key); err != nil {
		if delErr := a.fileStore.DeleteFile(ctx, s3Key); delErr != nil {
			a.logger.Warn("expression grid: failed to delete s3 object after persist key failure",
				zap.String("attachment_id", created.ID.String()),
				zap.String("s3_key", s3Key),
				zap.Error(delErr))
		}
		if delErr := a.ds.DeleteFileAttachment(ctx, userID, created.ID); delErr != nil {
			a.logger.Warn("expression grid: failed to delete attachment row after persist key failure",
				zap.String("attachment_id", created.ID.String()),
				zap.Error(delErr))
		}
		return fmt.Errorf("persist file attachment s3 key: %w", err)
	}

	receipt.OutputImageID = created.ID
	if err := a.persistExpressionGenerationReceipt(ctx, s3Key, receipt); err != nil {
		_ = a.fileStore.DeleteFile(ctx, s3Key+".generation.json")
		_ = a.fileStore.DeleteFile(ctx, s3Key)
		_ = a.ds.DeleteFileAttachment(ctx, userID, created.ID)
		return fmt.Errorf("persist generation receipt: %w", err)
	}

	thumb, err := imageutil.GenerateThumbnail(pngBytes, imageutil.DefaultThumbnailMaxPx)
	if err == nil && len(thumb) > 0 {
		thumbKey := storage.FileKeyForImageThumbnail(userID, created.ID)
		if err := a.fileStore.UploadFile(ctx, thumbKey, thumb, "image/jpeg"); err != nil {
			a.logger.Warn("expression grid: thumbnail upload failed",
				zap.String("attachment_id", created.ID.String()),
				zap.Error(err))
		}
	} else if err != nil {
		a.logger.Warn("expression grid: thumbnail generation failed",
			zap.String("attachment_id", created.ID.String()),
			zap.Error(err))
	}

	imgID := created.ID
	req := models.UpdatePersonalityExpressionRequest{
		ImageSet: true,
		ImageID:  &imgID,
	}
	if _, err := a.ds.UpsertPersonalityExpression(ctx, userID, personalityID, expressionKey, req); err != nil {
		return fmt.Errorf("upsert expression: %w", err)
	}
	return nil
}

func imageToRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	b := img.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, img, b.Min, draw.Src)
	return dst
}

// sliceAxisInto3 returns three segment lengths that sum to total, each at least 1 when total >= 3.
// Remainder pixels (total % 3) are added to the last segment(s) so the grid stays top-left aligned.
func sliceAxisInto3(total int) [3]int {
	base := total / 3
	rem := total % 3
	var out [3]int
	for i := 0; i < 3; i++ {
		out[i] = base
		if i >= 3-rem {
			out[i]++
		}
	}
	return out
}

// SlicePNGGrid3x3 decodes a PNG and returns nine row-major PNG-encoded cell blobs (3×3 grid tiles).
// Best-effort path for default expressions: normalizes uniform near-white outer margins when present,
// then splits width and height into three bands; if width or height is not divisible by 3, the last
// column(s) or row(s) absorb the extra 1–2 pixels so generation still succeeds despite trim/model drift.
func SlicePNGGrid3x3(pngData []byte) ([][]byte, error) {
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, fmt.Errorf("decode png: %w", err)
	}
	rgba := imageToRGBA(img)
	b0 := rgba.Bounds()
	w0, h0 := b0.Dx(), b0.Dy()
	if w0 < 3 || h0 < 3 {
		return nil, fmt.Errorf("image too small for 3×3 grid (%dx%d)", w0, h0)
	}
	minTrimSide := max(16, min(w0, h0)/15)
	canvas := imageutil.TrimUniformNearWhiteBorder(rgba, 242, minTrimSide, minTrimSide)
	bounds := canvas.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w < 3 || h < 3 {
		return nil, fmt.Errorf("image too small for 3×3 grid after trim (%dx%d)", w, h)
	}
	img = canvas
	colW := sliceAxisInto3(w)
	rowH := sliceAxisInto3(h)
	var colX, rowY [4]int
	colX[0] = bounds.Min.X
	for c := 0; c < 3; c++ {
		colX[c+1] = colX[c] + colW[c]
	}
	rowY[0] = bounds.Min.Y
	for r := 0; r < 3; r++ {
		rowY[r+1] = rowY[r] + rowH[r]
	}

	out := make([][]byte, 0, 9)
	for row := 0; row < 3; row++ {
		for col := 0; col < 3; col++ {
			cw, ch := colW[col], rowH[row]
			r := image.Rect(0, 0, cw, ch)
			dst := image.NewRGBA(r)
			srcPt := image.Point{
				X: colX[col],
				Y: rowY[row],
			}
			draw.Draw(dst, r, img, srcPt, draw.Src)
			minSide := max(cw/10, ch/10)
			if minSide < 16 {
				minSide = 16
			}
			trimmed := imageutil.TrimUniformNearWhiteBorder(dst, 242, minSide, minSide)
			var buf bytes.Buffer
			if err := png.Encode(&buf, trimmed); err != nil {
				return nil, fmt.Errorf("encode cell png row=%d col=%d: %w", row, col, err)
			}
			out = append(out, buf.Bytes())
		}
	}
	return out, nil
}
