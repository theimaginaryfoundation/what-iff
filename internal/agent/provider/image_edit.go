package provider

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // register JPEG: references are often photos
	"image/png"
	"net/http"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register WebP: an accepted upload type
)

// ImageRole says what an input image is for (ADR 0x045). The roles shape the instructions the
// image model is given, so markup is read as instructions rather than drawn.
type ImageRole string

const (
	// ImageRoleBase is the image being changed; a mask applies to it. At most one, always first.
	ImageRoleBase ImageRole = "base"
	// ImageRoleMarkup is a copy of the base with the user's marks drawn on it. The marks say where
	// and what to change; they are never content.
	ImageRoleMarkup ImageRole = "markup"
	// ImageRoleSketch is a rough drawing whose composition the result should follow, not its style.
	ImageRoleSketch ImageRole = "sketch"
	// ImageRoleReference shows a subject, character or style to carry into the result.
	ImageRoleReference ImageRole = "reference"
)

// ImageInput is one image handed to an image model.
type ImageInput struct {
	Data     []byte
	MIMEType string
	Role     ImageRole
}

// ImageEditRequest is a generation conditioned on input images: an edit of a base image (with or
// without a mask), or a new image from a sketch or references.
type ImageEditRequest struct {
	Prompt  string
	Quality ImageQuality
	// AspectRatio is the shape asked for; empty follows the first image's shape.
	AspectRatio ImageAspectRatio
	// Images are the inputs, the base (if any) first.
	Images []ImageInput
	// Mask marks the area of Images[0] to change: white is changed, black is kept. Any size; it is
	// stretched to the base image. Nil leaves the whole image open to change.
	Mask []byte
}

// ImageEditor is an ImageGenerator that also takes input images. Vendors without one cannot take
// reference images, and the generate_image tool moves such a request to a vendor that can.
type ImageEditor interface {
	ImageGenerator
	EditImage(ctx context.Context, req ImageEditRequest) (GeneratedImage, error)
}

// MaxEditImages is how many input images a request may carry, mask aside: enough for a base, its
// markup and a reference, and well inside both editing vendors' limits (OpenAI takes 16).
const MaxEditImages = 3

// validate checks the request before any vendor sees it.
func (r ImageEditRequest) validate() error {
	if strings.TrimSpace(r.Prompt) == "" {
		return fmt.Errorf("prompt is required")
	}
	if len(r.Images) == 0 {
		return fmt.Errorf("at least one input image is required")
	}
	if len(r.Images) > MaxEditImages {
		return fmt.Errorf("at most %d input images are supported, got %d", MaxEditImages, len(r.Images))
	}
	for i, img := range r.Images {
		if len(img.Data) == 0 {
			return fmt.Errorf("input image %d is empty", i+1)
		}
		if img.Role == ImageRoleBase && i != 0 {
			return fmt.Errorf("the base image must come first")
		}
	}
	if len(r.Mask) > 0 && r.Images[0].Role != ImageRoleBase {
		return fmt.Errorf("a mask needs a base image to apply to")
	}
	return nil
}

// editAspectRatio is the shape to ask for: the requested one, else the closest of the three to the
// first image (square when it cannot be read).
func (r ImageEditRequest) editAspectRatio() ImageAspectRatio {
	if r.AspectRatio != "" {
		return r.AspectRatio
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(r.Images[0].Data))
	if err != nil || cfg.Width == 0 || cfg.Height == 0 {
		return ImageAspectRatioSquare
	}
	ratio := float64(cfg.Width) / float64(cfg.Height)
	switch {
	case ratio >= 1.25:
		return ImageAspectRatioLandscape
	case ratio <= 0.8:
		return ImageAspectRatioPortrait
	default:
		return ImageAspectRatioSquare
	}
}

// inputImageMIME is the image's type sniffed from its bytes, which vendors check against the
// data; the declared type is only a fallback for bytes the sniffer cannot name.
func inputImageMIME(img ImageInput) string {
	if mime := http.DetectContentType(img.Data); strings.HasPrefix(mime, "image/") {
		return mime
	}
	return img.MIMEType
}

// maskGuideNote is what vendors without a native mask are told about the guide image.
const maskGuideNote = "is image 1 with the area to change tinted magenta. Change only that area, " +
	"blending it seamlessly into its surroundings, and keep everything outside it exactly as in image 1. " +
	"The magenta tint only marks the area: never draw it"

// editInstructions is the text that tells the model what each input image is, placed before the
// user's prompt. guideIndex is the 1-based position of a mask guide image (0 when there is none, or
// the vendor takes the mask natively, which nativeMask says).
func editInstructions(images []ImageInput, guideIndex int, nativeMask bool, userPrompt string) string {
	var lines []string
	for i, img := range images {
		n := i + 1
		switch img.Role {
		case ImageRoleBase:
			lines = append(lines, fmt.Sprintf("Image %d is the image to edit. Keep its subject, style, lighting and framing unless the request says otherwise.", n))
		case ImageRoleMarkup:
			lines = append(lines, fmt.Sprintf("Image %d is a copy of the image with the user's markup drawn over it. "+
				"The marks (strokes, circles, arrows, scribbles, handwritten notes) show where and what to change. "+
				"They are instructions, not content: never reproduce them.", n))
		case ImageRoleSketch:
			lines = append(lines, fmt.Sprintf("Image %d is the user's rough sketch. Follow its composition, layout and shapes, "+
				"but render it properly in the requested style rather than copying the sketch's lines or colours.", n))
		default:
			lines = append(lines, fmt.Sprintf("Image %d is a reference for subject, character or style.", n))
		}
	}
	if guideIndex > 0 {
		lines = append(lines, fmt.Sprintf("Image %d %s.", guideIndex, maskGuideNote))
	} else if nativeMask {
		lines = append(lines, "Only the masked area of image 1 may change; keep everything else exactly as it is.")
	}
	return strings.Join(lines, "\n") + "\n\nRequest: " + strings.TrimSpace(userPrompt)
}

// maskThreshold is the brightness (0–255) above which a mask pixel marks the area to change.
const maskThreshold = 128

// decodeImage decodes an input image.
func decodeImage(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if b := img.Bounds(); b.Dx() == 0 || b.Dy() == 0 {
		return nil, fmt.Errorf("image has no pixels")
	}
	return img, nil
}

// maskAt reads mask as the area to change, stretched to w×h: true where a pixel is bright and
// opaque. A transparent pixel is kept whatever its colour, so a mask drawn on a transparent
// canvas works as well as one drawn on black.
func maskAt(mask []byte, w, h int) ([]bool, error) {
	src, err := decodeImage(mask)
	if err != nil {
		return nil, fmt.Errorf("mask: %w", err)
	}
	scaled := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.NearestNeighbor.Scale(scaled, scaled.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	out := make([]bool, w*h)
	any := false
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := scaled.NRGBAAt(x, y)
			lum := (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
			if lum*int(c.A)/255 >= maskThreshold {
				out[y*w+x] = true
				any = true
			}
		}
	}
	if !any {
		return nil, fmt.Errorf("mask marks no area to change (paint the area white)")
	}
	return out, nil
}

// alphaMaskPNG turns a white-on-black mask into OpenAI's form for a w×h base: a PNG whose fully
// transparent pixels are the area to change.
func alphaMaskPNG(mask []byte, w, h int) ([]byte, error) {
	area, err := maskAt(mask, w, h)
	if err != nil {
		return nil, err
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !area[y*w+x] {
				out.SetNRGBA(x, y, color.NRGBA{A: 255})
			}
		}
	}
	return encodePNG(out)
}

// guideTint is the colour the area to change is tinted in a mask guide image: magenta, which
// photos and drawings rarely use, so it reads as a mark rather than part of the picture.
var guideTint = color.NRGBA{R: 255, G: 0, B: 255, A: 255}

// guideTintStrength is how much of the tint covers the area (0–1): enough to be unmistakable,
// little enough that what is underneath still shows.
const guideTintStrength = 0.55

// maskGuidePNG draws the mask onto the base for vendors without a native mask: the base with the
// area to change tinted magenta.
func maskGuidePNG(base, mask []byte) ([]byte, error) {
	src, err := decodeImage(base)
	if err != nil {
		return nil, fmt.Errorf("base image: %w", err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	area, err := maskAt(mask, w, h)
	if err != nil {
		return nil, err
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.Copy(out, image.Point{}, src, b, xdraw.Src, nil)
	mix := func(a, t uint8) uint8 {
		return uint8(float64(a)*(1-guideTintStrength) + float64(t)*guideTintStrength)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !area[y*w+x] {
				continue
			}
			c := out.NRGBAAt(x, y)
			out.SetNRGBA(x, y, color.NRGBA{R: mix(c.R, guideTint.R), G: mix(c.G, guideTint.G), B: mix(c.B, guideTint.B), A: 255})
		}
	}
	return encodePNG(out)
}

// imageSize reads an image's dimensions without decoding its pixels.
func imageSize(data []byte) (int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, fmt.Errorf("read image size: %w", err)
	}
	if cfg.Width == 0 || cfg.Height == 0 {
		return 0, 0, fmt.Errorf("image has no pixels")
	}
	return cfg.Width, cfg.Height, nil
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode PNG: %w", err)
	}
	return buf.Bytes(), nil
}
