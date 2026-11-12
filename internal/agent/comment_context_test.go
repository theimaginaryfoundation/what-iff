package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestFileCommentContextStaysWithItsMessage(t *testing.T) {
	comment := models.AdditionalContextItem{Type: models.AdditionalContextTypeFileComment, Content: "This message is a comment on the file \"plan.md\"."}
	memory := models.AdditionalContextItem{Type: models.AdditionalContextTypeMemory, Content: "likes short answers"}
	msg := &models.ChatMessage{Message: "tighten this", AdditionalContext: []models.AdditionalContextItem{memory, comment}}

	require.Equal(t, "[sys:x] tighten this\n\n"+comment.Content, withFileCommentContext("[sys:x] tighten this", msg))
	require.Equal(t, "hi", withFileCommentContext("hi", &models.ChatMessage{AdditionalContext: []models.AdditionalContextItem{memory}}))
	require.Equal(t, []models.AdditionalContextItem{comment}, sentWithMessage(msg.AdditionalContext))
}

func TestFileCommentContextIsNotMergedWithTheConversationsContext(t *testing.T) {
	modelCtx := &provider.ModelContext{}
	appendMergedAdditionalContext(modelCtx, []models.AdditionalContextItem{
		{Type: models.AdditionalContextTypeFileComment, Content: "This message is a comment on the file \"plan.md\"."},
	})
	require.Empty(t, modelCtx.Segments)
}
