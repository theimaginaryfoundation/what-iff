package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// solidPNG is a w×h PNG of one colour.
func solidPNG(t *testing.T, w, h int, c color.Color) []byte {
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

// leftHalfMask is a w×h mask that marks the left half for change.
func leftHalfMask(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				img.Set(x, y, color.White)
			} else {
				img.Set(x, y, color.Black)
			}
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func decodeNRGBA(t *testing.T, data []byte) *image.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	out := image.NewNRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}

func TestAlphaMaskPNG_TransparentWhereTheMaskIsWhite(t *testing.T) {
	t.Parallel()
	// A small mask is stretched to the base's size.
	out, err := alphaMaskPNG(leftHalfMask(t, 4, 2), 8, 4)
	require.NoError(t, err)
	img := decodeNRGBA(t, out)
	require.Equal(t, image.Rect(0, 0, 8, 4), img.Bounds())
	require.Equal(t, uint8(0), img.NRGBAAt(1, 1).A, "left half may change")
	require.Equal(t, uint8(255), img.NRGBAAt(6, 1).A, "right half is kept")
}

func TestMaskAt_TransparentPixelsAreKept(t *testing.T) {
	t.Parallel()
	area, err := maskAt(solidPNG(t, 2, 2, color.NRGBA{R: 255, G: 255, B: 255, A: 0}), 2, 2)
	require.Error(t, err, "a white but transparent mask marks nothing")
	require.Nil(t, area)
	require.ErrorContains(t, err, "marks no area")
}

func TestMaskGuidePNG_TintsOnlyTheMaskedArea(t *testing.T) {
	t.Parallel()
	base := solidPNG(t, 8, 4, color.NRGBA{R: 0, G: 200, B: 0, A: 255})
	out, err := maskGuidePNG(base, leftHalfMask(t, 8, 4))
	require.NoError(t, err)
	img := decodeNRGBA(t, out)
	tinted, kept := img.NRGBAAt(1, 1), img.NRGBAAt(6, 1)
	require.Equal(t, color.NRGBA{R: 0, G: 200, B: 0, A: 255}, kept)
	require.Greater(t, tinted.R, uint8(100))
	require.Greater(t, tinted.B, uint8(100))
	require.Less(t, tinted.G, uint8(200))
}

func TestEditInstructions(t *testing.T) {
	t.Parallel()
	images := []ImageInput{{Role: ImageRoleBase}, {Role: ImageRoleMarkup}, {Role: ImageRoleSketch}}
	text := editInstructions(images, 4, false, "  make it night  ")
	require.Contains(t, text, "Image 1 is the image to edit")
	require.Contains(t, text, "Image 2 is a copy of the image with the user's markup")
	require.Contains(t, text, "never reproduce them")
	require.Contains(t, text, "Image 3 is the user's rough sketch")
	require.Contains(t, text, "Image 4 is image 1 with the area to change tinted magenta")
	require.True(t, strings.HasSuffix(text, "Request: make it night"))

	native := editInstructions(images[:1], 0, true, "x")
	require.Contains(t, native, "Only the masked area of image 1 may change")
	require.NotContains(t, native, "magenta")
}

func TestImageEditRequest_Validate(t *testing.T) {
	t.Parallel()
	img := ImageInput{Data: []byte{1}}
	base := ImageInput{Data: []byte{1}, Role: ImageRoleBase}
	cases := map[string]struct {
		req  ImageEditRequest
		want string
	}{
		"no prompt":         {ImageEditRequest{Images: []ImageInput{img}}, "prompt is required"},
		"no images":         {ImageEditRequest{Prompt: "p"}, "at least one input image"},
		"too many":          {ImageEditRequest{Prompt: "p", Images: []ImageInput{img, img, img, img}}, "at most 3"},
		"base not first":    {ImageEditRequest{Prompt: "p", Images: []ImageInput{img, base}}, "base image must come first"},
		"mask without base": {ImageEditRequest{Prompt: "p", Images: []ImageInput{img}, Mask: []byte{1}}, "a mask needs a base image"},
	}
	for name, tc := range cases {
		require.ErrorContains(t, tc.req.validate(), tc.want, name)
	}
	require.NoError(t, ImageEditRequest{Prompt: "p", Images: []ImageInput{base}, Mask: []byte{1}}.validate())
}

func TestImageEditRequest_AspectRatioFollowsTheBase(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		w, h int
		want ImageAspectRatio
	}{{30, 20, ImageAspectRatioLandscape}, {20, 30, ImageAspectRatioPortrait}, {21, 20, ImageAspectRatioSquare}} {
		req := ImageEditRequest{Images: []ImageInput{{Data: solidPNG(t, tc.w, tc.h, color.White)}}}
		require.Equal(t, tc.want, req.editAspectRatio())
	}
	asked := ImageEditRequest{AspectRatio: ImageAspectRatioPortrait, Images: []ImageInput{{Data: solidPNG(t, 30, 20, color.White)}}}
	require.Equal(t, ImageAspectRatioPortrait, asked.editAspectRatio())
}

func TestOpenAIEditImage_SendsEveryImageAndAnAlphaMask(t *testing.T) {
	t.Parallel()
	result := solidPNG(t, 2, 2, color.White)
	var fields map[string][]string
	var files map[string][][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/images/edits", r.URL.Path)
		require.NoError(t, r.ParseMultipartForm(32<<20))
		fields = r.MultipartForm.Value
		files = map[string][][]byte{}
		for key, headers := range r.MultipartForm.File {
			for _, h := range headers {
				f, err := h.Open()
				require.NoError(t, err)
				data, _ := io.ReadAll(f)
				files[key] = append(files[key], data)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(result) + `"}]}`))
	}))
	t.Cleanup(srv.Close)

	gen := newTestOpenAIProvider(srv.URL).ImageGenerator().(ImageEditor)
	base := solidPNG(t, 30, 20, color.White)
	out, err := gen.EditImage(context.Background(), ImageEditRequest{
		Prompt:  "a red door",
		Quality: ImageQualityMedium,
		Images:  []ImageInput{{Data: base, MIMEType: "image/png", Role: ImageRoleBase}, {Data: solidPNG(t, 2, 2, color.Black), MIMEType: "image/png", Role: ImageRoleMarkup}},
		Mask:    leftHalfMask(t, 3, 2),
	})
	require.NoError(t, err)
	require.Equal(t, "image/png", out.MIMEType)

	require.Len(t, files["image[]"], 2, "both input images are sent")
	require.Len(t, files["mask"], 1)
	mask := decodeNRGBA(t, files["mask"][0])
	require.Equal(t, image.Rect(0, 0, 30, 20), mask.Bounds(), "the mask matches the base's size")
	require.NotContains(t, fields, "input_fidelity", "the image model refuses it")
	require.Equal(t, []string{"1536x1024"}, fields["size"], "a landscape base gets a landscape result")
	require.Equal(t, []string{"medium"}, fields["quality"])
	require.Contains(t, fields["prompt"][0], "Only the masked area of image 1 may change")
	require.Contains(t, fields["prompt"][0], "Request: a red door")
}

func TestGeminiEditImage_SendsImagesAndAGuideForTheMask(t *testing.T) {
	t.Parallel()
	f := newFakeImageVendor(t)
	result := testImageBytes(t, "png")
	f.respond = func(w http.ResponseWriter) {
		writeJSON(w, http.StatusOK, map[string]any{"candidates": []any{map[string]any{
			"finishReason": "STOP",
			"content":      map[string]any{"parts": []any{map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(result)}}}},
		}}})
	}
	g := NewGeminiImageGenerator(f.srv.Client(), f.srv.URL, nil)
	base := solidPNG(t, 8, 4, color.White)
	_, err := g.EditImage(context.Background(), ImageEditRequest{
		Prompt: "a cat on the left",
		Images: []ImageInput{{Data: base, Role: ImageRoleBase}},
		Mask:   leftHalfMask(t, 8, 4),
	})
	require.NoError(t, err)

	parts := f.lastBody["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	require.Len(t, parts, 3, "instructions, the base, the guide")
	text := parts[0].(map[string]any)["text"].(string)
	require.Contains(t, text, "Image 2 is image 1 with the area to change tinted magenta")
	require.Equal(t, base64.StdEncoding.EncodeToString(base), parts[1].(map[string]any)["inlineData"].(map[string]any)["data"])
	require.Equal(t, "image/png", parts[2].(map[string]any)["inlineData"].(map[string]any)["mimeType"])
	cfg := f.lastBody["generationConfig"].(map[string]any)["imageConfig"].(map[string]any)
	_, hasRatio := cfg["aspectRatio"]
	require.False(t, hasRatio, "an edit leaves the shape to the input image")
}
