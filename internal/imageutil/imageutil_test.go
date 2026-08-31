package imageutil

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeForUploadStreamsPNG(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	require.NoError(t, png.Encode(&input, image.NewRGBA(image.Rect(0, 0, 4000, 2000))))

	var output bytes.Buffer
	fileName, err := NormalizeForUpload(&input, &output, "original.jpeg", DefaultUploadImageMaxPx)
	require.NoError(t, err)
	require.Equal(t, "original.png", fileName)

	normalized, format, err := image.Decode(&output)
	require.NoError(t, err)
	require.Equal(t, "png", format)
	require.Equal(t, 2048, normalized.Bounds().Dx())
	require.Equal(t, 1024, normalized.Bounds().Dy())
}

func TestScaledDimensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		srcW, srcH           int
		maxPx                int
		expectedW, expectedH int
	}{
		{name: "wide", srcW: 4000, srcH: 2000, maxPx: 2048, expectedW: 2048, expectedH: 1024},
		{name: "tall", srcW: 2000, srcH: 4000, maxPx: 2048, expectedW: 1024, expectedH: 2048},
		{name: "one pixel wide", srcW: 1, srcH: 4000, maxPx: 2048, expectedW: 1, expectedH: 2048},
		{name: "one pixel tall", srcW: 4000, srcH: 1, maxPx: 2048, expectedW: 2048, expectedH: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			width, height := scaledDimensions(tt.srcW, tt.srcH, tt.maxPx)
			require.Equal(t, tt.expectedW, width)
			require.Equal(t, tt.expectedH, height)
		})
	}
}
