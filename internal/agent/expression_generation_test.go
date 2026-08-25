package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestBuildExpressionPickCatalog_alwaysIncludesLabels(t *testing.T) {
	t.Parallel()
	slots := []models.PersonalityExpression{
		{ExpressionKey: "happy", Label: strPtr("warm and friendly")},
	}
	cat := buildExpressionPickCatalog(slots)
	require.Contains(t, cat, "- happy — warm and friendly")
}

func TestBuildExpressionPickUserMessageBody(t *testing.T) {
	t.Parallel()
	body := buildExpressionPickUserMessageBody("- happy — warm")
	require.Contains(t, body, "Classify the latest assistant reply")
	require.Contains(t, body, "expression_key")
	require.Contains(t, body, "- happy — warm")
	require.Contains(t, body, "MUST NOT invent")
	require.NotContains(t, body, "focused, etc.")
}

func TestParseExpressionPickPayload(t *testing.T) {
	t.Parallel()
	k, r := parseExpressionPickPayload(`{"expression_key":"sad","reasoning":"  Tone was subdued. "}`)
	require.Equal(t, "sad", k)
	require.Equal(t, "Tone was subdued.", r)
}

func TestParseExpressionPickPayload_invalidJSON(t *testing.T) {
	t.Parallel()
	k, r := parseExpressionPickPayload("not json")
	require.Empty(t, k)
	require.Empty(t, r)
}

func strPtr(s string) *string { return &s }
