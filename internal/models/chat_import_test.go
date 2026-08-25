package models

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestImportProgress_MarshalsImportedIDs(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	b, err := json.Marshal(ImportProgress{
		Phase:       "complete",
		Imported:    1,
		ImportedIDs: []uuid.UUID{id},
	})
	require.NoError(t, err)
	require.Contains(t, string(b), `"imported_ids"`)
	require.Contains(t, string(b), id.String())
}
