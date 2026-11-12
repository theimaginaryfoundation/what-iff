package officecomments

import (
	"archive/zip"
	"errors"
	"io"
	"strings"
)

// Limits on what reading a package may cost: its parts are inflated into memory and parsed, and a
// file is the user's (or anyone's who shared it), so a small zip that inflates to gigabytes, or a
// part nested millions of elements deep, is refused rather than read.
const (
	// MaxPartBytes is the most one part may inflate to.
	MaxPartBytes = 64 << 20
	// MaxReadBytes is the most all the parts one change reads may inflate to together.
	MaxReadBytes = 256 << 20
	// MaxXMLDepth is the deepest a part's elements may nest; Office's own parts stay far below it.
	MaxXMLDepth = 256
)

// ErrTooLarge means a part of the file inflates past MaxPartBytes, or the parts read past
// MaxReadBytes, or nest past MaxXMLDepth.
var ErrTooLarge = errors.New("the file is too large or too deeply nested to read")

// Budget counts the bytes a package's parts inflate to as they are read (see MaxReadBytes).
type Budget struct{ left int64 }

// NewBudget is a Budget of MaxReadBytes.
func NewBudget() *Budget { return &Budget{left: MaxReadBytes} }

// Read inflates f, refusing a part past MaxPartBytes or past what is left of the budget, whatever
// size its header claims.
func (b *Budget) Read(f *zip.File) ([]byte, error) {
	limit := min(int64(MaxPartBytes), b.left)
	if f.UncompressedSize64 > uint64(limit) {
		return nil, ErrTooLarge
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	b.left -= int64(len(data))
	return data, nil
}

// XMLText is s without the characters XML 1.0 cannot hold, escaped or not: C0 controls other
// than tab, line feed and carriage return, U+FFFE and U+FFFF. Pasted text can hold them (a
// vertical tab, a form feed), and one in a part makes Office refuse the file; the browser's
// escapeXml drops the same ones.
func XMLText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r == 0xfffe || r == 0xffff {
			return -1
		}
		return r
	}, s)
}
