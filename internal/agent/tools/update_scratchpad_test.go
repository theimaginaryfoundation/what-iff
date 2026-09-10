package tools

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// UpdateScratchpadTool's cheapest branch is the invalid-JSON-args guard,
// which fires before any datastore access, so datastore can stay nil.
func TestUpdateScratchpadTool_InvalidArgsReturnsErrorResult(t *testing.T) {
	t.Parallel()

	tool := &ScratchpadTool{logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}

	out, err := tool.UpdateScratchpadTool(context.Background(), chat, []byte(`not json`))
	require.NoError(t, err)
	require.Contains(t, out, "invalid arguments")
	require.Contains(t, out, `"success":false`)
}
