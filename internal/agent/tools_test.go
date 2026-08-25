package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChatToolsAddsWebSearch(t *testing.T) {
	tools := getChatTools(ToolConfig{})

	require.Len(t, tools, 1)
	assert.NotNil(t, tools[0].OfWebSearch)
}

func TestGetChatToolsRespectsWebSearchToggle(t *testing.T) {
	tools := getChatTools(ToolConfig{
		DisabledTools: map[string]bool{"web_search": true},
	})

	require.Empty(t, tools)
}
