package stcard

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

// SillyTavern PNG cards carry the card JSON, base64-encoded, in a PNG tEXt chunk: keyword `chara`
// for v2 cards and `ccv3` for v3. The picture itself is the character's avatar.
const (
	pngKeywordV2 = "chara"
	pngKeywordV3 = "ccv3"
)

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// IsPNG reports whether b starts with the PNG file signature.
func IsPNG(b []byte) bool { return bytes.HasPrefix(b, pngSignature) }

type pngChunk struct {
	typ  string
	data []byte
}

// readPNGChunks splits a PNG into its chunks, validating lengths (CRCs are not verified: a
// corrupt image is the image library's problem, not a reason to lose the card).
func readPNGChunks(b []byte) ([]pngChunk, error) {
	if !IsPNG(b) {
		return nil, fmt.Errorf("%w: not a PNG image", ErrInvalidCard)
	}
	var chunks []pngChunk
	for pos := len(pngSignature); pos < len(b); {
		if pos+8 > len(b) {
			return nil, fmt.Errorf("%w: truncated PNG", ErrInvalidCard)
		}
		size := int(binary.BigEndian.Uint32(b[pos : pos+4]))
		typ := string(b[pos+4 : pos+8])
		end := pos + 8 + size + 4
		if size < 0 || end > len(b) || end < pos {
			return nil, fmt.Errorf("%w: truncated PNG", ErrInvalidCard)
		}
		chunks = append(chunks, pngChunk{typ: typ, data: b[pos+8 : pos+8+size]})
		pos = end
	}
	return chunks, nil
}

// textChunk splits a tEXt chunk into keyword and text.
func textChunk(data []byte) (keyword, text string, ok bool) {
	i := bytes.IndexByte(data, 0)
	if i <= 0 {
		return "", "", false
	}
	return string(data[:i]), string(data[i+1:]), true
}

// ExtractFromPNG returns the card JSON embedded in a SillyTavern PNG card. A v3 (`ccv3`) chunk is
// preferred over a v2 (`chara`) one when both are present, as SillyTavern does.
func ExtractFromPNG(png []byte) ([]byte, error) {
	chunks, err := readPNGChunks(png)
	if err != nil {
		return nil, err
	}
	var v2, v3 string
	for _, c := range chunks {
		if c.typ != "tEXt" {
			continue
		}
		switch keyword, text, _ := textChunk(c.data); keyword {
		case pngKeywordV2:
			v2 = text
		case pngKeywordV3:
			v3 = text
		}
	}
	encoded := v3
	if encoded == "" {
		encoded = v2
	}
	if encoded == "" {
		return nil, fmt.Errorf("%w: this PNG has no embedded character card", ErrInvalidCard)
	}
	card, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: the embedded card is not valid base64", ErrInvalidCard)
	}
	return card, nil
}

// EmbedInPNG returns png with cardJSON stored as a `chara` tEXt chunk just before IEND, replacing
// any card already embedded in the image.
func EmbedInPNG(png, cardJSON []byte) ([]byte, error) {
	chunks, err := readPNGChunks(png)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.Write(pngSignature)
	wrote := false
	for _, c := range chunks {
		if c.typ == "tEXt" {
			if keyword, _, _ := textChunk(c.data); keyword == pngKeywordV2 || keyword == pngKeywordV3 {
				continue
			}
		}
		if c.typ == "IEND" {
			writePNGChunk(&out, "tEXt", append([]byte(pngKeywordV2+"\x00"), base64.StdEncoding.EncodeToString(cardJSON)...))
			wrote = true
		}
		writePNGChunk(&out, c.typ, c.data)
	}
	if !wrote {
		return nil, fmt.Errorf("%w: PNG has no IEND chunk", ErrInvalidCard)
	}
	return out.Bytes(), nil
}

func writePNGChunk(w *bytes.Buffer, typ string, data []byte) {
	var head [4]byte
	binary.BigEndian.PutUint32(head[:], uint32(len(data)))
	w.Write(head[:])
	w.WriteString(typ)
	w.Write(data)
	crc := crc32.NewIEEE()
	crc.Write([]byte(typ))
	crc.Write(data)
	binary.BigEndian.PutUint32(head[:], crc.Sum32())
	w.Write(head[:])
}
