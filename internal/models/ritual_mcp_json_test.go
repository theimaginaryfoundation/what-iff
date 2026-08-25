package models

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRitual_MCPServerIDs_JSONOmitMeansNil(t *testing.T) {
	var r Ritual
	err := json.Unmarshal([]byte(`{}`), &r)
	require.NoError(t, err)
	require.Nil(t, r.MCPServerIDs)
}

func TestRitual_MCPServerIDs_JSONRoundTrip(t *testing.T) {
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	ids := []uuid.UUID{id}
	r := Ritual{MCPServerIDs: &ids}
	raw, err := json.Marshal(r)
	require.NoError(t, err)

	var out Ritual
	require.NoError(t, json.Unmarshal(raw, &out))
	require.NotNil(t, out.MCPServerIDs)
	require.Equal(t, ids, *out.MCPServerIDs)
}
