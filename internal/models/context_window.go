package models

import "strings"

// contextWindows are input context windows, in tokens, by model-name prefix (first match wins, so
// a more specific prefix comes before its family). Values are the documented default windows; a
// model that offers a larger window behind an opt-in is listed at its default. They only steer
// when the Advanced loop compacts its context (ADR 0x026), where under-stating a window just
// compacts sooner, so an unlisted model returns 0 and compaction falls back to the growth rule.
var contextWindows = []struct {
	prefix string
	tokens int64
}{
	{"claude-", 200_000},
	{"gpt-4.1", 1_000_000},
	{"gpt-4o", 128_000},
	{"gpt-5", 400_000},
	{"o1", 200_000},
	{"o3", 200_000},
	{"o4", 200_000},
	{"gemini-", 1_000_000},
	{"deepseek", 128_000},
	{"qwen", 128_000},
	{"mistral", 128_000},
	{"glm-", 128_000},
}

// ContextWindowFor returns a model's input context window in tokens, or 0 when it is not known.
func ContextWindowFor(name string) int64 {
	n := normalizeModelName(name)
	for _, w := range contextWindows {
		if strings.HasPrefix(n, w.prefix) {
			return w.tokens
		}
	}
	return 0
}
