package tools

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// CreateMemoryTool's cheapest branch is the invalid-JSON-args guard, which
// fires before any embedding call or datastore access, so ds/oaiClient can
// stay nil.
func TestCreateMemoryTool_InvalidArgsReturnsErrorResult(t *testing.T) {
	t.Parallel()

	tool := &VectorStoreMemoryTool{logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New()}

	out, err := tool.CreateMemoryTool(context.Background(), chat, []byte(`not json`))
	require.NoError(t, err)
	require.Contains(t, out, "invalid arguments")
	require.Contains(t, out, `"success":false`)
}
