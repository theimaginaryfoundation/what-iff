package models

import "strings"

// contextWindows are input context windows, in tokens, by model-name prefix (first match wins, so
// a more specific prefix comes before its family). Values are the documented default windows; a
// model that offers a larger window behind an opt-in is listed at its default. They only steer
// when the Advanced loop compacts its context (ADR 0x026), where under-stating a window just
// compacts sooner, so a family's entry is its smallest member's window and an unlisted model gets
// DefaultContextWindow.
var contextWindows = []struct {
	prefix string
	tokens int64
}{
	{"claude-", 200_000},
	{"gpt-4.1", 1_000_000},
	{"gpt-4o", 128_000},
	{"gpt-5-chat", 128_000},
	{"gpt-5", 400_000},
	{"o1", 200_000},
	{"o3", 200_000},
	{"o4", 200_000},
	{"gemini-", 1_000_000},
	{"deepseek", 128_000},
	{"qwen-max", 32_000},
	{"qwen", 128_000},
	{"mistral-saba", 32_000},
	{"mistral", 128_000},
	{"codestral", 256_000},
	{"magistral", 128_000},
	{"devstral", 128_000},
	{"glm-", 128_000},
}

// DefaultContextWindow is the window assumed for a model the table does not list: a common size
// that few current chat models are below, so compaction still protects them.
const DefaultContextWindow int64 = 128_000

// ContextWindowFor returns a model's input context window in tokens (DefaultContextWindow when it
// is not listed).
func ContextWindowFor(name string) int64 {
	n := normalizeModelName(name)
	for _, w := range contextWindows {
		if strings.HasPrefix(n, w.prefix) {
			return w.tokens
		}
	}
	return DefaultContextWindow
}
