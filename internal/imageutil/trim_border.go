package imageutil

import (
	"image"
	"image/color"
	"image/draw"
)

// TrimUniformNearWhiteBorder removes edge rows and columns where every pixel in that full row or column
// is an opaque near-white pixel (each RGB channel ≥ rgbThreshold). This strips uniform gutters left
// after slicing a generated 3×3 grid without affecting interior content when borders are not uniform.
//
// Returns src unchanged if trimming would shrink below minWidth×minHeight or remove more than half
// the pixels on either axis (guards against mistaken trims).
func TrimUniformNearWhiteBorder(src *image.RGBA, rgbThreshold uint8, minWidth, minHeight int) *image.RGBA {
	if src == nil {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < minWidth || h < minHeight {
		return src
	}
	x0, y0 := b.Min.X, b.Min.Y

	rowAllNearWhite := func(y int) bool {
		for x := 0; x < w; x++ {
			if !rgbaOpaqueNearWhite(src.RGBAAt(x0+x, y0+y), rgbThreshold) {
				return false
			}
		}
		return true
	}
	colAllNearWhite := func(x int, yTop, yBot int) bool {
		for y := yTop; y <= yBot; y++ {
			if !rgbaOpaqueNearWhite(src.RGBAAt(x0+x, y0+y), rgbThreshold) {
				return false
			}
		}
		return true
	}

	top, bot := 0, h-1
	for top < h && rowAllNearWhite(top) {
		top++
	}
	for bot > top && rowAllNearWhite(bot) {
		bot--
	}
	if top > bot {
		return src
	}
	left, right := 0, w-1
	for left < w && colAllNearWhite(left, top, bot) {
		left++
	}
	for right > left && colAllNearWhite(right, top, bot) {
		right--
	}

	nw, nh := right-left+1, bot-top+1
	if nw < minWidth || nh < minHeight {
		return src
	}

	subBounds := image.Rect(x0+left, y0+top, x0+right+1, y0+bot+1)
	sub := src.SubImage(subBounds)
	out := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.Draw(out, out.Bounds(), sub, subBounds.Min, draw.Src)
	return out
}

func rgbaOpaqueNearWhite(c color.RGBA, thr uint8) bool {
	if c.A < 250 {
		return false
	}
	return c.R >= thr && c.G >= thr && c.B >= thr
}
