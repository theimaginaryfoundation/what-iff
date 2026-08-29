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

func TestScratchpadContentForOperation_AppendOnlyAddsDelta(t *testing.T) {
	t.Parallel()

	got, err := scratchpadContentForOperation(scratchpadOperationAppend, "existing context", "new context")
	require.NoError(t, err)
	require.Equal(t, "existing context\nnew context", got)
}

func TestScratchpadContentForOperation_AppendToEmptyScratchpad(t *testing.T) {
	t.Parallel()

	got, err := scratchpadContentForOperation(scratchpadOperationAppend, "", "new context")
	require.NoError(t, err)
	require.Equal(t, "new context", got)
}

func TestScratchpadContentForOperation_ReplaceAndLegacyDefault(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{scratchpadOperationReplace, ""} {
		got, err := scratchpadContentForOperation(operation, "old", "replacement")
		require.NoError(t, err)
		require.Equal(t, "replacement", got)
	}
}

func TestScratchpadContentForOperation_RejectsUnknownOperation(t *testing.T) {
	t.Parallel()

	_, err := scratchpadContentForOperation("merge", "old", "new")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported scratchpad operation")
}
