package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextWindowFor(t *testing.T) {
	assert.Equal(t, int64(200_000), ContextWindowFor("claude-opus-5-5"))
	assert.Equal(t, int64(400_000), ContextWindowFor(" GPT-5.1 "))
	assert.Equal(t, int64(1_000_000), ContextWindowFor("gpt-4.1-mini"))
	assert.Equal(t, int64(128_000), ContextWindowFor("gpt-4o"))
	assert.Equal(t, int64(1_000_000), ContextWindowFor("gemini-2.5-pro"))
	assert.Equal(t, DefaultContextWindow, ContextWindowFor("some-local-model"), "unknown models get the conservative default")
	assert.Equal(t, DefaultContextWindow, ContextWindowFor("mimo-v2.5-pro"))
	assert.Equal(t, int64(128_000), ContextWindowFor("gpt-5-chat-latest"), "the chat alias is smaller than the gpt-5 family")
	assert.Equal(t, int64(32_000), ContextWindowFor("qwen-max"))
	assert.Equal(t, int64(32_000), ContextWindowFor("mistral-saba-latest"))
}
