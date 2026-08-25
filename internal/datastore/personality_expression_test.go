package datastore

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
)

func TestToPersonalityExpressionModel_WithImage(t *testing.T) {
	t.Parallel()

	imageID := uuid.New()
	label := "Happy"
	createdAt := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)

	model := toPersonalityExpressionModel(&ent.PersonalityExpression{
		ExpressionKey: "happy",
		Label:         &label,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		Edges: ent.PersonalityExpressionEdges{
			Image: &ent.FileAttachment{ID: imageID},
		},
	})

	require.NotNil(t, model)
	require.Equal(t, "happy", model.ExpressionKey)
	require.Equal(t, label, *model.Label)
	require.Equal(t, imageID, *model.ImageID)
	require.Equal(t, "/api/image-gallery/"+imageID.String()+"?size=full", *model.ImageURL)
	require.Equal(t, createdAt, model.CreatedAt)
	require.Equal(t, updatedAt, model.UpdatedAt)
}

func TestToPersonalityExpressionModel_WithoutImageKeepsNullableFieldsNil(t *testing.T) {
	t.Parallel()

	model := toPersonalityExpressionModel(&ent.PersonalityExpression{
		ExpressionKey: "excited",
		CreatedAt:     time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 4, 28, 13, 0, 0, 0, time.UTC),
	})

	require.NotNil(t, model)
	require.Equal(t, "excited", model.ExpressionKey)
	require.Nil(t, model.Label)
	require.Nil(t, model.ImageID)
	require.Nil(t, model.ImageURL)
}

func TestPersonalityExpressionImageURL(t *testing.T) {
	t.Parallel()

	require.Nil(t, personalityExpressionImageURL(nil))

	imageID := uuid.New()
	url := personalityExpressionImageURL(&imageID)
	require.NotNil(t, url)
	require.Equal(t, "/api/image-gallery/"+imageID.String()+"?size=full", *url)
}
