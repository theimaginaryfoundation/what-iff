package agent

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"sync"
)

// mockRitualPNGBase64 returns a small genuine PNG (encoded once, cached) used
// as the image-ritual fixture under MOCK_LLM. Encoding at runtime instead of
// embedding a base64 blob guarantees a valid PNG signature and keeps the
// fixture reviewable.
var mockRitualPNGBase64 = sync.OnceValue(func() string {
	const size = 8
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for x := 0; x < size; x++ {
		for y := 0; y < size; y++ {
			// Simple checkerboard so the fixture is visually recognizable.
			if (x+y)%2 == 0 {
				img.Set(x, y, color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xFF})
			} else {
				img.Set(x, y, color.RGBA{R: 0xEE, G: 0xEE, B: 0xEE, A: 0xFF})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		// Encoding a tiny in-memory RGBA image with fixed, deterministic input
		// cannot fail at runtime — this is a hard invariant, not a user-input-
		// dependent path. sync.OnceValue caches a panicking call and re-panics
		// on every later call, which is the desired behavior here: a change
		// that somehow breaks this invariant should fail loudly and
		// consistently in mock mode rather than silently serving a zero-value
		// fixture on retries.
		panic("mock ritual PNG encode failed: " + err.Error())
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
})
