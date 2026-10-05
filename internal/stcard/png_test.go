package stcard

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withTextChunk splices a tEXt chunk in before IEND.
func withTextChunk(t *testing.T, base []byte, keyword, text string) []byte {
	t.Helper()
	chunks, err := readPNGChunks(base)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	out.Write(pngSignature)
	for _, c := range chunks {
		if c.typ == "IEND" {
			writePNGChunk(&out, "tEXt", []byte(keyword+"\x00"+text))
		}
		writePNGChunk(&out, c.typ, c.data)
	}
	return out.Bytes()
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestEmbedThenExtractRoundTripsAndKeepsAValidImage(t *testing.T) {
	cardJSON := []byte(`{"spec":"chara_card_v2","spec_version":"2.0","data":{"name":"Ada — 狐"}}`)

	embedded, err := EmbedInPNG(testPNG(t), cardJSON)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExtractFromPNG(embedded)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(cardJSON) {
		t.Errorf("card = %s", got)
	}
	img, err := png.Decode(bytes.NewReader(embedded))
	if err != nil {
		t.Fatalf("embedded PNG no longer decodes: %v", err)
	}
	if img.Bounds().Dx() != 4 {
		t.Errorf("image changed: %v", img.Bounds())
	}
}

func TestEmbedReplacesAnExistingCard(t *testing.T) {
	old := withTextChunk(t, testPNG(t), "chara", b64(`{"old":true}`))

	embedded, err := EmbedInPNG(old, []byte(`{"new":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := ExtractFromPNG(embedded); string(got) != `{"new":true}` {
		t.Errorf("card = %s", got)
	}
	if n := strings.Count(string(embedded), "chara\x00"); n != 1 {
		t.Errorf("%d chara chunks, want exactly 1", n)
	}
}

func TestExtractPrefersV3OverV2(t *testing.T) {
	both := withTextChunk(t, withTextChunk(t, testPNG(t), "chara", b64(`{"v":2}`)), "ccv3", b64(`{"v":3}`))
	if got, _ := ExtractFromPNG(both); string(got) != `{"v":3}` {
		t.Errorf("card = %s, want the ccv3 one", got)
	}
}

func TestExtractErrors(t *testing.T) {
	cases := map[string][]byte{
		"not a png":          []byte("hello"),
		"png without a card": testPNG(t),
		"bad base64":         withTextChunk(t, testPNG(t), "chara", "!!!not-base64!!!"),
		"truncated":          testPNG(t)[:20],
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ExtractFromPNG(in); !errors.Is(err, ErrInvalidCard) {
				t.Errorf("err = %v, want ErrInvalidCard", err)
			}
		})
	}
}

func TestParseReadsAPNGCard(t *testing.T) {
	cardJSON := `{"spec":"chara_card_v2","spec_version":"2.0","data":{"name":"Ada","description":"A fox."}}`
	png := withTextChunk(t, testPNG(t), "chara", b64(cardJSON))

	imp, err := Parse(png, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if imp.Name != "Ada" || !strings.Contains(imp.SystemPrompt, "A fox.") {
		t.Errorf("imported = %+v", imp)
	}
}
