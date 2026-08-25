package utils

import (
	"encoding/binary"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

// DecodeTextToUTF8 normalizes text file bytes into a valid UTF-8 string.
//
// Supported inputs:
// - UTF-8 (optionally with BOM)
// - UTF-16LE / UTF-16BE (requires BOM)
//
// If the input is not valid UTF-8 and does not have a UTF-16 BOM, an error is returned.
func DecodeTextToUTF8(b []byte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}

	// UTF-8 BOM
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}

	// UTF-16 BOM
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		// UTF-16LE
		return decodeUTF16(b[2:], binary.LittleEndian)
	}
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		// UTF-16BE
		return decodeUTF16(b[2:], binary.BigEndian)
	}

	if utf8.Valid(b) {
		return string(b), nil
	}

	return "", fmt.Errorf("text is not valid UTF-8 (and no UTF-16 BOM present)")
}

func decodeUTF16(b []byte, order binary.ByteOrder) (string, error) {
	if len(b)%2 != 0 {
		return "", fmt.Errorf("invalid UTF-16 byte length: %d", len(b))
	}

	u16 := make([]uint16, 0, len(b)/2)
	for i := 0; i < len(b); i += 2 {
		u16 = append(u16, order.Uint16(b[i:i+2]))
	}

	runes := utf16.Decode(u16)
	return string(runes), nil
}
