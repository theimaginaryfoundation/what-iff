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

// expressionGridCanvasTemplate is the canvas prompt; %s is the row-major layout built from the requested keys.
const expressionGridCanvasTemplate = `Render as a single square image: a 3×3 grid of nine equally-sized portrait panels on one flush canvas.

Requirements:
- Nine panels in a perfect 3×3 layout (row-major: %s).
- Panels must meet edge-to-edge with NO gutters, NO margins, NO white strips or blank gaps between cells — dividing lines (if any) must be hair-thin and drawn inside the art, not empty whitespace.
- Do not add outer margins or padding around the grid; artwork fills the square edge-to-edge.
- Do not render any text, captions, or labels in the panels.
- The same character appears in every panel with consistent design; square composition, readable faces, consistent lighting and style.

Match the character design described in the preceding paragraph.`

// expressionGridReferenceInstructions is appended when a reference image is sent to the image model.
const expressionGridReferenceInstructions = `The attached image is a style and character reference: match its art style, rendering technique, palette, and character design as closely as possible. Do not copy its composition or framing — produce the 3×3 expression grid described above.`

// buildExpressionGridCanvasInstructions renders the canvas prompt for nine row-major expression keys.
// Keys are humanized ("in-love" → "in love") since the image model reads them as prose.
func buildExpressionGridCanvasInstructions(keys []string) string {
	rows := make([]string, 0, 3)
	for r := 0; r < 3; r++ {
		cells := make([]string, 0, 3)
		for c := 0; c < 3; c++ {
			if idx := r*3 + c; idx < len(keys) {
				cells = append(cells, humanizeExpressionKey(keys[idx]))
			}
		}
		rows = append(rows, strings.Join(cells, " | "))
	}
	return fmt.Sprintf(expressionGridCanvasTemplate, strings.Join(rows, "; "))
}

// truncateExpressionGridPrompt caps the image prompt at the model's accepted length.
func truncateExpressionGridPrompt(prompt string) string {
	if len(prompt) > 16000 {
		return prompt[:16000]
	}
	return prompt
}

// humanizeExpressionKey turns a URL-safe key into prompt prose.
func humanizeExpressionKey(key string) string {
	return strings.NewReplacer("-", " ", "_", " ").Replace(strings.TrimSpace(key))
}

// expressionGridReference is an optional reference image for grid generation.
type expressionGridReference struct {
	bytes []byte
	mime  string
	// sendToImageModel also passes the reference to the image model (edit endpoint),
	// not only to the likeness pass.
	sendToImageModel bool
}

// GenerateDefaultExpressionGrid runs likeness (nano) + one medium-quality square image generation,
// splices the 3×3 grid into PNG cells, uploads all nine cells, and upserts each expression key.
// Quota: not metered (personality tooling); approximate cost ~nano chat + one medium image generation (~$0.01).
//
// Semantics / retries:
//   - Safe to call multiple times: UpsertPersonalityExpression overwrites rows for the same keys.
//   - Partial failure: if a mid-loop step fails, earlier cells may already be persisted while later keys
//     are unchanged — callers may retry; subsequent runs overwrite keys that succeed again.
//   - HTTP handlers may skip work when all default keys already have images unless force=true (see personality handler).
func (a *Agent) GenerateDefaultExpressionGrid(ctx context.Context, userID, personalityID uuid.UUID) ([]models.PersonalityExpression, error) {
	if err := a.checkExpressionGridConfigured(); err != nil {
		return nil, err
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

	// The cover image, when present, grounds the likeness pass only.
	var ref expressionGridReference
	if person.CoverImageID != nil {
		ref.bytes, ref.mime = a.loadExpressionReferenceImage(ctx, userID, *person.CoverImageID)
	}

	cells, err := a.generateExpressionGridCells(ctx, person, ExpressionGridKeys, ref)
	if err != nil {
		return nil, err
	}

	for i, key := range ExpressionGridKeys {
		imgID, err := a.uploadExpressionCellAttachment(ctx, userID, personalityID, key, cells[i])
		if err != nil {
			return nil, fmt.Errorf("expression %q: %w", key, err)
		}
		req := models.UpdatePersonalityExpressionRequest{
			ImageSet: true,
			ImageID:  &imgID,
		}
		if _, err := a.ds.UpsertPersonalityExpression(ctx, userID, personalityID, key, req); err != nil {
			return nil, fmt.Errorf("expression %q: upsert expression: %w", key, err)
		}
	}

	out, err := a.ds.ListPersonalityExpressions(ctx, userID, personalityID)
	if err != nil {
		return nil, fmt.Errorf("list expressions after grid: %w", err)
	}
	return out, nil
}

func (a *Agent) checkExpressionGridConfigured() error {
	if a == nil || a.ds == nil || a.OpenAIProvider == nil {
		return fmt.Errorf("expression grid: agent not configured")
	}
	// Mock/local mode: deliberate denial — grid generation is inference + image calls.
	if a.nonVendorLLM() {
		return fmt.Errorf("expression grid generation is disabled under LLM_BACKEND=mock/local")
	}
	if a.fileStore == nil {
		return fmt.Errorf("expression grid: file store not configured")
	}
	return nil
}

// loadExpressionReferenceImage resolves an owned image attachment to bytes for use as a reference.
// Falls back to the thumbnail when the full image exceeds maxExpressionReferenceImageBytes.
// Returns nil bytes (and logs) when the image cannot be used; callers proceed without a reference.
func (a *Agent) loadExpressionReferenceImage(ctx context.Context, userID, attachmentID uuid.UUID) ([]byte, string) {
	// GetFileAttachment already enforces user ownership; an error here means
	// the attachment is missing or cross-user, so we skip gracefully.
	att, err := a.ds.GetFileAttachment(ctx, userID, attachmentID)
	if err != nil || att == nil {
		a.logger.Warn("expression grid: failed to load reference image; proceeding without it",
			zap.String("image_id", attachmentID.String()),
			zap.Error(err))
		return nil, ""
	}
	// ResolveAttachmentImageBytes returns (bytes, mimeType); key-miss errors are
	// logged internally at debug level, so we warn only when bytes are empty.
	rawBytes, rawMIME := storage.ResolveAttachmentImageBytes(ctx, a.logger, a.fileStore, userID, att, false)
	if len(rawBytes) > maxExpressionReferenceImageBytes {
		a.logger.Info("expression grid: reference image exceeds size limit; using thumbnail",
			zap.String("image_id", attachmentID.String()),
			zap.Int("bytes", len(rawBytes)),
			zap.Int("limit", maxExpressionReferenceImageBytes))
		rawBytes, rawMIME = storage.ResolveAttachmentImageBytes(ctx, a.logger, a.fileStore, userID, att, true)
	}
	if len(rawBytes) == 0 {
		a.logger.Warn("expression grid: reference image resolved to empty bytes; proceeding without reference",
			zap.String("image_id", attachmentID.String()))
	}
	return capExpressionReferenceImage(rawBytes, rawMIME)
}

// generateExpressionGridCells runs the likeness pass and one grid image call for nine row-major keys,
// returning nine PNG cells in the same order as keys.
func (a *Agent) generateExpressionGridCells(ctx context.Context, person *models.Personality, keys []string, ref expressionGridReference) ([][]byte, error) {
	if len(keys) != 9 {
		return nil, fmt.Errorf("expression grid: expected 9 keys, got %d", len(keys))
	}

	likeness, err := a.inferExpressionGridLikeness(ctx, strings.TrimSpace(person.SystemPrompt), ref.bytes, ref.mime)
	if err != nil {
		return nil, err
	}
	if likeness == "" {
		likeness = "A distinctive character portrait consistent with the personality, suitable for a grid of facial expressions."
		a.logger.Warn("expression grid: empty likeness segment; using fallback prose")
	}

	canvasInstructions := buildExpressionGridCanvasInstructions(keys)
	if person.ImageStyle != "" && person.ImageStyle != "auto" {
		canvasInstructions += "\n\nArt style: " + person.ImageStyle
	}

	fullPrompt := truncateExpressionGridPrompt(strings.TrimSpace(likeness) + "\n\n" + canvasInstructions)

	var b64PNG string
	if ref.sendToImageModel && len(ref.bytes) > 0 {
		refPrompt := truncateExpressionGridPrompt(fullPrompt + "\n\n" + expressionGridReferenceInstructions)
		b64PNG, err = a.OpenAIProvider.EditImagePNGBase64WithQuality(ctx, refPrompt, provider.ImageQualityMedium, ref.bytes, ref.mime)
		if err != nil {
			// The reference is best-effort: a rejected edit (model/format/moderation) should not
			// sink the run, so fall back to prompt-only generation (likeness already saw the image).
			a.logger.Warn("expression grid: reference-image generation failed; falling back to prompt-only",
				zap.Error(err))
			b64PNG = ""
		}
	}
	if b64PNG == "" {
		b64PNG, err = a.OpenAIProvider.GenerateImagePNGBase64WithQuality(ctx, fullPrompt, provider.ImageQualityMedium)
		if err != nil {
			return nil, fmt.Errorf("generate grid image: %w", err)
		}
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
	return cells, nil
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

// uploadExpressionCellAttachment persists one grid cell image in S3 (not file_content) as a
// personality-pinned gallery attachment and returns its ID. It does not assign the expression slot.
func (a *Agent) uploadExpressionCellAttachment(ctx context.Context, userID, personalityID uuid.UUID, expressionKey string, pngBytes []byte) (uuid.UUID, error) {
	if len(pngBytes) == 0 {
		return uuid.Nil, fmt.Errorf("empty cell png")
	}
	name := fmt.Sprintf("expression-%s.png", expressionKey)

	created, err := a.ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:          name,
		FileType:      "image/png",
		PersonalityID: &personalityID,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create file attachment: %w", err)
	}
	if created == nil {
		return uuid.Nil, fmt.Errorf("create file attachment: nil model")
	}

	s3Key := storage.FileKeyForPersonality(userID, personalityID, created.ID, name)
	if err := a.fileStore.UploadFile(ctx, s3Key, pngBytes, "image/png"); err != nil {
		_ = a.ds.DeleteFileAttachment(ctx, userID, created.ID)
		return uuid.Nil, fmt.Errorf("upload full image: %w", err)
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
		return uuid.Nil, fmt.Errorf("persist file attachment s3 key: %w", err)
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

	return created.ID, nil
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
