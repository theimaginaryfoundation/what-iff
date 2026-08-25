package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToolResultOutput_NonEmpty(t *testing.T) {
	t.Parallel()
	got := toolResultOutput(ToolResult{ID: "x", Output: `{"ok":true}`, IsErr: false})
	require.Equal(t, `{"ok":true}`, got)
}

func TestToolResultOutput_EmptySuccess(t *testing.T) {
	t.Parallel()
	got := toolResultOutput(ToolResult{ID: "x", Output: "", IsErr: false})
	require.JSONEq(t, `{"message":"Tool executed successfully with no output"}`, got)
}

func TestToolResultOutput_EmptyError(t *testing.T) {
	t.Parallel()
	got := toolResultOutput(ToolResult{ID: "x", Output: "", IsErr: true})
	require.Equal(t, "unknown error occurred", got)
}
