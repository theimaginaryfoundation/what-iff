package imageutil

import (
	"image"
	"image/color"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrimUniformNearWhiteBorder_NoUniformWhite_NoChange(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * y), G: 40, B: 40, A: 255})
		}
	}
	out := TrimUniformNearWhiteBorder(img, 242, 4, 4)
	require.Equal(t, img, out)
}

func TestTrimUniformNearWhiteBorder_RemovesWhiteMargin(t *testing.T) {
	t.Parallel()
	const outer = 60
	img := image.NewRGBA(image.Rect(0, 0, outer, outer))
	for y := 0; y < outer; y++ {
		for x := 0; x < outer; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	for y := 20; y < 40; y++ {
		for x := 20; x < 40; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 200, G: 10, B: 10, A: 255})
		}
	}
	out := TrimUniformNearWhiteBorder(img, 242, 8, 8)
	require.Equal(t, 20, out.Bounds().Dx())
	require.Equal(t, 20, out.Bounds().Dy())
	c := out.RGBAAt(10, 10)
	require.Equal(t, uint8(200), c.R)
}
