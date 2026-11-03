package provider

import (
	"errors"
	"strings"
)

// contextOverflowMarkers are how the providers word a request whose input does not fit the
// model's context window (lower-cased). Matching the message is the only common signal: the
// providers use different status codes and error types for it, and some return a plain 400.
var contextOverflowMarkers = []string{
	"context_length_exceeded",              // OpenAI error code
	"maximum context length",               // OpenAI, DeepSeek, Mistral, Qwen (OpenAI-compatible)
	"prompt is too long",                   // Anthropic
	"exceed context limit",                 // Anthropic: input length and max_tokens exceed context limit
	"exceeds the maximum number of tokens", // Gemini
	"range of input length should be",      // Qwen (DashScope)
	"context window",                       // generic wording ("exceeds the context window")
}

// IsContextOverflowError reports whether err is a provider refusing a request because its input
// does not fit the model's context window, which compacting the input can fix.
func IsContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	var sv *SafetyViolationError
	if errors.As(err, &sv) {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, m := range contextOverflowMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}
