package provider

import (
	"encoding/base64"
	"strings"
)

// encodeImageBase64 returns raw's standard base64 encoding, for an inline image block.
//
// It allocates the result once. base64.StdEncoding.EncodeToString allocates the encoded bytes and
// then copies them into the string, and building a data URL with fmt.Sprintf copies them again,
// so every image in a request briefly existed three or four times over (what-iff#419).
func encodeImageBase64(raw []byte) string {
	return base64WithPrefix("", raw)
}

// imageDataURL returns raw as a "data:<mediaType>;base64,…" URL, allocated once (see
// encodeImageBase64).
func imageDataURL(mediaType string, raw []byte) string {
	return base64WithPrefix("data:"+mediaType+";base64,", raw)
}

func base64WithPrefix(prefix string, raw []byte) string {
	var b strings.Builder
	b.Grow(len(prefix) + base64.StdEncoding.EncodedLen(len(raw)))
	b.WriteString(prefix)
	enc := base64.NewEncoder(base64.StdEncoding, &b)
	_, _ = enc.Write(raw) // a strings.Builder never fails a write
	_ = enc.Close()
	return b.String()
}
