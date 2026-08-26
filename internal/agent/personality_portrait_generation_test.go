package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// GeneratePersonalityPortraitImage's cheapest branch is the documented
// image-style-none guard, which fires before the agent-configuration check.
func TestGeneratePersonalityPortraitImage_NoneStyleReturnsError(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	attachment, err := a.GeneratePersonalityPortraitImage(context.Background(), uuid.New(), "a prompt", models.ImageStyleNone)
	require.Nil(t, attachment)
	require.Error(t, err)
	require.ErrorContains(t, err, "portrait generation skipped")
}
