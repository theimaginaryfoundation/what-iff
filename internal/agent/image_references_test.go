package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// fakeImageEditor is a vendor that edits; it records each edit request.
type fakeImageEditor struct {
	fakeImageGenerator
	mu       sync.Mutex
	requests []provider.ImageEditRequest
}

func (f *fakeImageEditor) EditImage(_ context.Context, req provider.ImageEditRequest) (provider.GeneratedImage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	return f.image, f.err
}

func testPNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// turnImageAttachment is an image generate_image made earlier in the turn (bytes in memory).
func turnImageAttachment(name string, data []byte) *models.FileAttachment {
	return &models.FileAttachment{ID: uuid.New(), Name: name, FileType: "image/png", FileContent: base64.StdEncoding.EncodeToString(data)}
}

func withTurnImages(atts ...*models.FileAttachment) context.Context {
	chatCtx := &chatContext{}
	chatCtx.rememberTurnImages(atts)
	return context.WithValue(context.Background(), turnChatContextKey{}, chatCtx)
}

func editArgs(t *testing.T, v map[string]any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	require.NoError(t, err)
	return out
}

func TestGenerateImageTool_EditsWithTheChosenVendor(t *testing.T) {
	t.Parallel()
	meter := &recordingMeter{allow: true}
	gemini := &fakeImageEditor{fakeImageGenerator: fakeImageGenerator{provider: models.ModelProviderGoogle, image: provider.GeneratedImage{Base64: "/9j/4AAQ", MIMEType: "image/jpeg"}}}
	openAI := &fakeImageEditor{fakeImageGenerator: fakeImageGenerator{provider: models.ModelProviderOpenAI}}
	a := &Agent{logger: zap.NewNop(), meter: meter, imageGenerators: map[models.ModelProvider]provider.ImageGenerator{
		models.ModelProviderOpenAI: openAI,
		models.ModelProviderGoogle: gemini,
	}}
	a.testHooks.ImageGenerationPreference = preferring(models.ModelProviderGoogle)

	base := testPNG(t, 8, 4, color.White)
	markup := testPNG(t, 8, 4, color.Black)
	mask := testPNG(t, 8, 4, color.White)
	ctx := withTurnImages(
		turnImageAttachment("photo.png", base),
		turnImageAttachment("photo-markup.png", markup),
		turnImageAttachment("photo-mask.png", mask),
	)
	args := editArgs(t, map[string]any{
		"prompt": "make the sky purple",
		// The markup comes first in the list; the base still goes to the vendor first.
		"reference_images": []any{map[string]any{"image": "photo-markup.png", "role": "markup"}, map[string]any{"image": "PHOTO.png", "role": "base"}},
		"mask_image":       "photo-mask.png",
	})
	out, atts, err := a.generateImageTool(ctx, &models.Chat{ID: uuid.New(), UserID: uuid.New()}, args)
	require.NoError(t, err)
	require.Len(t, atts, 1)
	require.Zero(t, openAI.calls.Load()+int32(len(openAI.requests)))

	require.Len(t, gemini.requests, 1)
	req := gemini.requests[0]
	require.Equal(t, "make the sky purple", req.Prompt)
	require.Equal(t, provider.ImageAspectRatio(""), req.AspectRatio, "an edit keeps its base's shape")
	require.Len(t, req.Images, 2)
	require.Equal(t, provider.ImageRoleBase, req.Images[0].Role)
	require.Equal(t, base, req.Images[0].Data)
	require.Equal(t, "image/png", req.Images[0].MIMEType)
	require.Equal(t, provider.ImageRoleMarkup, req.Images[1].Role)
	require.Equal(t, mask, req.Mask)

	var result generateImageToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.True(t, result.Success)
	require.Equal(t, "google", result.Provider)
	require.Equal(t, 2, result.ReferenceImages)
	require.Equal(t, "photo-mask.png", result.MaskImage, "the receipt shows the mask was applied")
	require.Equal(t, []generateImageInputReceipt{
		{Image: "photo.png", Role: "base"},
		{Image: "photo-markup.png", Role: "markup"},
		{Image: "photo-mask.png", Role: "mask"},
	}, result.Inputs)
	require.Equal(t, "same_as_base", result.AspectRatio)

	require.Len(t, meter.records, 1)
	require.Equal(t, "tool_edit", meter.records[0].Metadata["path"])
	require.Equal(t, "google", meter.records[0].ImageProvider)
}

// A vendor that cannot edit is refused, never swapped for one that can (ADR 0x045 §5).
func TestGenerateImageTool_EditRefusedWhenTheChosenVendorCannotEdit(t *testing.T) {
	t.Parallel()
	meter := &recordingMeter{allow: true}
	zai := &fakeImageGenerator{provider: models.ModelProviderZAI}
	openAI := &fakeImageEditor{fakeImageGenerator: fakeImageGenerator{provider: models.ModelProviderOpenAI}}
	a := &Agent{logger: zap.NewNop(), meter: meter, imageGenerators: map[models.ModelProvider]provider.ImageGenerator{
		models.ModelProviderOpenAI: openAI,
		models.ModelProviderZAI:    zai,
	}}
	a.testHooks.ImageGenerationPreference = preferring(models.ModelProviderZAI)

	ctx := withTurnImages(turnImageAttachment("photo.png", testPNG(t, 2, 2, color.White)))
	out, atts, err := a.generateImageTool(ctx, &models.Chat{ID: uuid.New(), UserID: uuid.New()}, editArgs(t, map[string]any{
		"prompt": "add a hat", "reference_images": []any{"photo.png"},
	}))
	require.NoError(t, err)
	require.Empty(t, atts)
	var result generateImageToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Contains(t, result.Error, "Z.ai")
	require.Contains(t, result.Error, "Settings")
	require.Zero(t, zai.calls.Load())
	require.Empty(t, openAI.requests)
	require.Empty(t, meter.records)
}

func TestGenerateImageTool_EditArgumentErrors(t *testing.T) {
	t.Parallel()
	openAI := &fakeImageEditor{fakeImageGenerator: fakeImageGenerator{provider: models.ModelProviderOpenAI}}
	a := &Agent{logger: zap.NewNop(), imageGenerators: map[models.ModelProvider]provider.ImageGenerator{models.ModelProviderOpenAI: openAI}}
	png := testPNG(t, 2, 2, color.White)
	ctx := withTurnImages(turnImageAttachment("cat.png", png), turnImageAttachment("dog.png", png))

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"unknown name lists the images here", map[string]any{"prompt": "x", "reference_images": []any{"bird.png"}}, `no image named "bird.png" in this conversation; images here: cat.png, dog.png`},
		{"mask without an image", map[string]any{"prompt": "x", "mask_image": "cat.png"}, `mask_image "cat.png" needs the image it masks`},
		{"two bases", map[string]any{"prompt": "x", "reference_images": []any{map[string]any{"image": "cat.png", "role": "base"}, map[string]any{"image": "dog.png", "role": "base"}}}, "only one reference image can be the base"},
		{"unknown role", map[string]any{"prompt": "x", "reference_images": []any{map[string]any{"image": "cat.png", "role": "vibe"}}}, `a mask goes in mask_image`},
		{"too many", map[string]any{"prompt": "x", "reference_images": []any{"cat.png", "dog.png", "cat.png", "dog.png"}}, "at most 3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := a.generateImageTool(ctx, &models.Chat{ID: uuid.New(), UserID: uuid.New()}, editArgs(t, tc.args))
			require.NoError(t, err)
			var result generateImageToolResult
			require.NoError(t, json.Unmarshal([]byte(out), &result))
			require.False(t, result.Success)
			require.Contains(t, result.Error, tc.want)
		})
	}
	require.Empty(t, openAI.requests)
}

func TestImageInputs_MaskMakesTheFirstReferenceTheBase(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	png := testPNG(t, 2, 2, color.White)
	ctx := withTurnImages(turnImageAttachment("a.png", png), turnImageAttachment("m.png", png))
	in, err := a.resolveImageInputs(ctx, &models.Chat{}, []generateImageReference{{Image: "a.png"}}, "m.png")
	require.NoError(t, err)
	require.Equal(t, provider.ImageRoleBase, in.images[0].Role)
	require.Equal(t, png, in.mask)
	require.Equal(t, []generateImageInputReceipt{{Image: "a.png", Role: "base"}, {Image: "m.png", Role: "mask"}}, in.receipt)
}

// Models reach for role "mask" in reference_images; it is taken as mask_image.
func TestImageInputs_ARoleMaskReferenceIsTheMask(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	base, mask := testPNG(t, 2, 2, color.White), testPNG(t, 2, 2, color.Black)
	ctx := withTurnImages(turnImageAttachment("a.png", base), turnImageAttachment("m.png", mask))
	in, err := a.resolveImageInputs(ctx, &models.Chat{}, []generateImageReference{{Image: "a.png", Role: "base"}, {Image: "m.png", Role: "Mask"}}, "")
	require.NoError(t, err)
	require.Len(t, in.images, 1)
	require.Equal(t, mask, in.mask)
	require.Equal(t, "m.png", in.maskName())

	_, err = a.resolveImageInputs(ctx, &models.Chat{}, []generateImageReference{{Image: "a.png"}, {Image: "m.png", Role: "mask"}}, "m.png")
	require.ErrorContains(t, err, "only one mask per request")

	_, err = a.resolveImageInputs(ctx, &models.Chat{}, []generateImageReference{{Image: "a.png"}, {Role: "mask"}}, "")
	require.ErrorContains(t, err, "needs an image name", "never dropped silently")
}

// A mask alone (the user attached it without its image) is refused in words the agent can act on.
func TestImageInputs_AMaskWithoutItsImageSaysWhatToDo(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	ctx := withTurnImages(turnImageAttachment("photo-mask.png", testPNG(t, 2, 2, color.White)))
	_, err := a.resolveImageInputs(ctx, &models.Chat{}, nil, "photo-mask.png")
	require.ErrorContains(t, err, `mask_image "photo-mask.png" needs the image it masks`)
	require.ErrorContains(t, err, "ask the user to attach it")
}

func TestImageInputs_RefusesNonImages(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	ctx := withTurnImages(turnImageAttachment("notes.png", []byte("just some text, honestly")))
	_, err := a.resolveImageInputs(ctx, &models.Chat{}, []generateImageReference{{Image: "notes.png"}}, "")
	require.ErrorContains(t, err, "notes.png is not an image")
}

func TestGenerateImageReference_AcceptsABareName(t *testing.T) {
	t.Parallel()
	var refs []generateImageReference
	require.NoError(t, json.Unmarshal([]byte(`["a.png", {"image": "b.png", "role": "sketch"}]`), &refs))
	require.Equal(t, []generateImageReference{{Image: "a.png"}, {Image: "b.png", Role: "sketch"}}, refs)
}
