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
	assert.Zero(t, ContextWindowFor("some-local-model"))
}
