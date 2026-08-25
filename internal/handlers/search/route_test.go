package search

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRouteFor_KnownTypes(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	tests := []struct {
		resourceType string
		want         string
	}{
		{TypeChat, "/chat/" + id.String()},
		{TypePersonality, "/personality/" + id.String()},
		{TypeRitual, "/ritual/" + id.String()},
		{TypeMemory, "/memory/" + id.String()},
		{TypeImage, "/image-gallery"},
	}

	for _, tc := range tests {
		t.Run(tc.resourceType, func(t *testing.T) {
			require.Equal(t, tc.want, routeFor(tc.resourceType, id))
		})
	}
}

func TestRouteFor_UnknownTypeReturnsEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, routeFor("unknown", uuid.New()))
	require.Empty(t, routeFor("", uuid.New()))
}
