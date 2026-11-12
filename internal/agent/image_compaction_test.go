package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// noisyPNG is a photo-like PNG (gradients plus noise) as an image vendor would write it.
func noisyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	rng := rand.New(rand.NewSource(7))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n := func(v int) uint8 { return uint8(min(255, max(0, v+rng.Intn(9)-4))) }
			img.SetNRGBA(x, y, color.NRGBA{R: n(x * 255 / w), G: n(y * 255 / h), B: n(100), A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestCompactGeneratedImage(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	raw := noisyPNG(t, 256, 192)

	out := a.compactGeneratedImage(provider.GeneratedImage{Base64: base64.StdEncoding.EncodeToString(raw), MIMEType: "image/png"})
	compact, err := base64.StdEncoding.DecodeString(out.Base64)
	require.NoError(t, err)
	require.Less(t, len(compact), len(raw))
	img, err := png.Decode(bytes.NewReader(compact))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 256, 192), img.Bounds())
	require.Equal(t, "image/png", out.MIMEType)

	jpeg := provider.GeneratedImage{Base64: "/9j/4AAQ", MIMEType: "image/jpeg"}
	require.Equal(t, jpeg, a.compactGeneratedImage(jpeg), "another format is kept as sent")
	broken := provider.GeneratedImage{Base64: base64.StdEncoding.EncodeToString([]byte("not a png")), MIMEType: "image/png"}
	require.Equal(t, broken, a.compactGeneratedImage(broken), "what cannot be decoded is kept as sent")
}

// The attachment the tool returns, which is what gets stored, is the compact PNG.
func TestGenerateImageTool_StoresGeneratedPNGsCompacted(t *testing.T) {
	t.Parallel()
	raw := noisyPNG(t, 256, 192)
	gen := &fakeImageGenerator{provider: models.ModelProviderOpenAI, image: provider.GeneratedImage{Base64: base64.StdEncoding.EncodeToString(raw), MIMEType: "image/png"}}
	a := &Agent{logger: zap.NewNop(), imageGenerators: map[models.ModelProvider]provider.ImageGenerator{models.ModelProviderOpenAI: gen}}

	args, err := json.Marshal(map[string]any{"prompt": "a gradient", "count": 2})
	require.NoError(t, err)
	_, atts, err := a.generateImageTool(context.Background(), &models.Chat{ID: uuid.New(), UserID: uuid.New()}, args)
	require.NoError(t, err)
	require.Len(t, atts, 2)
	for _, att := range atts {
		stored, err := base64.StdEncoding.DecodeString(att.FileContent)
		require.NoError(t, err)
		require.Less(t, len(stored), len(raw))
		require.Equal(t, "image/png", att.FileType)
	}
}
