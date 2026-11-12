package imageutil

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/stretchr/testify/require"
)

// withOrientation inserts an EXIF APP1 segment carrying orientation o right after a
// JPEG's start-of-image marker, in the given byte order.
func withOrientation(t *testing.T, jpg []byte, o uint16, order binary.ByteOrder) []byte {
	t.Helper()
	tiff := make([]byte, 8+2+12+4)
	if order == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	order.PutUint16(tiff[2:], 42)
	order.PutUint32(tiff[4:], 8) // IFD0 right after the header
	order.PutUint16(tiff[8:], 1) // one entry
	entry := tiff[10:]
	order.PutUint16(entry[0:], 0x0112) // Orientation
	order.PutUint16(entry[2:], 3)      // SHORT
	order.PutUint32(entry[4:], 1)
	order.PutUint16(entry[8:], o)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segment := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
	segment = append(segment, payload...)
	out := append([]byte{}, jpg[:2]...)
	out = append(out, segment...)
	return append(out, jpg[2:]...)
}

// leftRedRightBlue is a 64×32 JPEG: the left half red, the right half blue.
func leftRedRightBlue(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			c := color.RGBA{R: 220, B: 20, A: 255}
			if x >= 32 {
				c = color.RGBA{R: 20, B: 220, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}))
	return buf.Bytes()
}

func TestJPEGOrientationReadsEitherByteOrder(t *testing.T) {
	t.Parallel()
	jpg := leftRedRightBlue(t)
	require.Equal(t, 1, jpegOrientation(jpg), "no EXIF: upright")
	require.Equal(t, 6, jpegOrientation(withOrientation(t, jpg, 6, binary.BigEndian)))
	require.Equal(t, 8, jpegOrientation(withOrientation(t, jpg, 8, binary.LittleEndian)))
	require.Equal(t, 1, jpegOrientation(withOrientation(t, jpg, 42, binary.BigEndian)), "out of range")
	require.Equal(t, 1, jpegOrientation([]byte("not a jpeg")))
	require.Equal(t, 1, jpegOrientation(withOrientation(t, jpg, 6, binary.BigEndian)[:30]), "truncated")
}

// Each orientation puts the source's top-left pixel where EXIF says it belongs.
func TestApplyOrientationMovesTheTopLeftPixel(t *testing.T) {
	t.Parallel()
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	marker := color.NRGBA{R: 255, A: 255}
	src.SetNRGBA(0, 0, marker)
	cases := map[int]struct {
		w, h, x, y int
	}{
		1: {3, 2, 0, 0},
		2: {3, 2, 2, 0},
		3: {3, 2, 2, 1},
		4: {3, 2, 0, 1},
		5: {2, 3, 0, 0},
		6: {2, 3, 1, 0},
		7: {2, 3, 1, 2},
		8: {2, 3, 0, 2},
	}
	for o, want := range cases {
		out := toNRGBA(applyOrientation(src, o))
		require.Equal(t, image.Rect(0, 0, want.w, want.h), out.Bounds(), "orientation %d", o)
		require.Equal(t, marker, out.NRGBAAt(want.x, want.y), "orientation %d", o)
	}
}

// A portrait phone photo (stored sideways, EXIF orientation 6) is stored upright.
func TestNormalizeForUploadTurnsAPhoneJPEGUpright(t *testing.T) {
	t.Parallel()
	input := withOrientation(t, leftRedRightBlue(t), 6, binary.BigEndian)

	var output bytes.Buffer
	_, contentType, err := NormalizeForUpload(bytes.NewReader(input), &output, "phone.jpg", DefaultUploadImageMaxPx)
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", contentType)
	require.Zero(t, jpegOrientation(output.Bytes())-1, "no orientation left to apply twice")
	img, err := jpeg.Decode(&output)
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 32, 64), img.Bounds())
	// Turned clockwise, the left (red) half is now the top.
	r, _, b, _ := img.At(16, 8).RGBA()
	require.Greater(t, r, b, "top is red")
	r, _, b, _ = img.At(16, 56).RGBA()
	require.Greater(t, b, r, "bottom is blue")
}
