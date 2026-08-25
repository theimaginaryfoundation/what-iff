package provider

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWrapCanceledWithUsage_ExtractsThroughWrappedErrors(t *testing.T) {
	t.Parallel()

	wrapped := WrapCanceledWithUsage(context.Canceled, CancelUsage{
		InputTokens:  123,
		OutputTokens: 45,
		Available:    true,
		Source:       "claude_stream_usage",
	})
	err := fmt.Errorf("outer: %w", wrapped)

	usage, ok := ExtractCancelUsage(err)
	require.True(t, ok)
	require.Equal(t, int64(123), usage.InputTokens)
	require.Equal(t, int64(45), usage.OutputTokens)
	require.True(t, usage.Available)
	require.Equal(t, "claude_stream_usage", usage.Source)
	require.True(t, errors.Is(err, context.Canceled))
}

func TestWrapCanceledWithUsage_NonCancelPassThrough(t *testing.T) {
	t.Parallel()

	baseErr := errors.New("boom")
	wrapped := WrapCanceledWithUsage(baseErr, CancelUsage{
		InputTokens: 99,
		Available:   true,
		Source:      "ignored",
	})
	require.Same(t, baseErr, wrapped)

	_, ok := ExtractCancelUsage(wrapped)
	require.False(t, ok)
}
