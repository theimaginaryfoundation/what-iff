package agent

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSlicePNGGrid3x3_NineCells(t *testing.T) {
	t.Parallel()
	const sz = 9
	img := image.NewNRGBA(image.Rect(0, 0, sz, sz))
	idx := 0
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			v := uint8(10 + idx*20)
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
			idx++
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	cells, err := SlicePNGGrid3x3(buf.Bytes())
	require.NoError(t, err)
	require.Len(t, cells, 9)

	for i, blob := range cells {
		im, err := png.Decode(bytes.NewReader(blob))
		require.NoError(t, err, "cell %d", i)
		require.Equal(t, 3, im.Bounds().Dx(), "cell %d width", i)
		require.Equal(t, 3, im.Bounds().Dy(), "cell %d height", i)
	}
}

func TestSliceAxisInto3(t *testing.T) {
	t.Parallel()
	cases := []struct {
		total int
		want  [3]int
	}{
		{9, [3]int{3, 3, 3}},
		{10, [3]int{3, 3, 4}},
		{11, [3]int{3, 4, 4}},
		{12, [3]int{4, 4, 4}},
		{991, [3]int{330, 330, 331}},
		{990, [3]int{330, 330, 330}},
	}
	for _, tc := range cases {
		got := sliceAxisInto3(tc.total)
		require.Equal(t, tc.want, got, "total=%d", tc.total)
		sum := got[0] + got[1] + got[2]
		require.Equal(t, tc.total, sum, "total=%d", tc.total)
	}
}

func TestSlicePNGGrid3x3_NonMultipleOfThreeDimensions(t *testing.T) {
	t.Parallel()
	const sz = 10
	img := image.NewNRGBA(image.Rect(0, 0, sz, sz))
	for y := 0; y < sz; y++ {
		for x := 0; x < sz; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 40, G: 40, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	cells, err := SlicePNGGrid3x3(buf.Bytes())
	require.NoError(t, err)
	require.Len(t, cells, 9)

	// colW [3,3,4], rowH [3,3,4]; row-major cell (r,c) is colW[c] × rowH[r].
	wantW := []int{3, 3, 4, 3, 3, 4, 3, 3, 4}
	wantH := []int{3, 3, 3, 3, 3, 3, 4, 4, 4}
	for i, blob := range cells {
		im, err := png.Decode(bytes.NewReader(blob))
		require.NoError(t, err, "cell %d", i)
		require.Equal(t, wantW[i], im.Bounds().Dx(), "cell %d width", i)
		require.Equal(t, wantH[i], im.Bounds().Dy(), "cell %d height", i)
	}
}

func TestCapExpressionReferenceImage(t *testing.T) {
	t.Parallel()
	small := make([]byte, 1024)
	got, mime := capExpressionReferenceImage(small, "image/png")
	require.Equal(t, small, got)
	require.Equal(t, "image/png", mime)

	large := make([]byte, maxExpressionReferenceImageBytes+1)
	got, mime = capExpressionReferenceImage(large, "image/jpeg")
	require.Nil(t, got)
	require.Equal(t, "", mime)
}
