package datastore

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPersonalityPromptHistoryAPIExists is the first regression for issue #42.
// The durable behavior needs explicit list + revert seams rather than silently
// overwriting system_prompt. Behavior-level append-only tests are added with the
// implementation once these public datastore seams exist.
func TestPersonalityPromptHistoryAPIExists(t *testing.T) {
	datastoreType := reflect.TypeOf(&Datastore{})

	_, hasList := datastoreType.MethodByName("ListPersonalityPromptChanges")
	require.True(t, hasList, "personality prompt edits need a durable history listing seam")

	_, hasRevert := datastoreType.MethodByName("RevertPersonalityPromptChange")
	require.True(t, hasRevert, "personality prompt history needs an explicit revert seam")
}
