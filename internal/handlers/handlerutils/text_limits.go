package handlerutils

import "unicode/utf16"

const (
	TextLimitWarningThreshold = 20_000
	TextLimitHardMax          = 25_000
)

// UTF16CodeUnitCount returns the JavaScript-style string length
// (number of UTF-16 code units), matching frontend `.length` behavior.
func UTF16CodeUnitCount(s string) int {
	return len(utf16.Encode([]rune(s)))
}
