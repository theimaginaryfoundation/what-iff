package provider

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

// The encodings match the stdlib's, and the image's encoded bytes are allocated once: the
// result itself, plus the encoder's small fixed buffer (what-iff#419).
func TestImageEncodingMatchesStdlibInOneCopy(t *testing.T) {
	raw := make([]byte, 1<<20+1) // not a multiple of 3, so the encoding is padded
	for i := range raw {
		raw[i] = byte(i * 31)
	}
	want := base64.StdEncoding.EncodeToString(raw)

	require.Equal(t, want, encodeImageBase64(raw))
	require.Equal(t, "data:image/png;base64,"+want, imageDataURL("image/png", raw))
	require.Empty(t, encodeImageBase64(nil))

	// testing.Benchmark counts every allocation in the process, so other tests' goroutines
	// add noise. Half a copy of headroom absorbs that and still fails on a second copy.
	oneCopy := float64(len(want) + len(want)/2)
	require.LessOrEqual(t, allocatedBytes(func() { _ = encodeImageBase64(raw) }), oneCopy)
	require.LessOrEqual(t, allocatedBytes(func() { _ = imageDataURL("image/png", raw) }), oneCopy)
}

func allocatedBytes(f func()) float64 {
	r := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			f()
		}
	})
	return float64(r.AllocedBytesPerOp())
}
