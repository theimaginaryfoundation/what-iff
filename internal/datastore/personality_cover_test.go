package datastore

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/schema"
)

func TestToPersonalityModel_WithCoverImageHydratesIDAndURL(t *testing.T) {
	t.Parallel()

	coverID := uuid.New()
	model := toPersonalityModel(&ent.Personality{
		ID:           uuid.New(),
		Name:         "Vera",
		SystemPrompt: "You are Vera.",
		Edges: ent.PersonalityEdges{
			CoverImage: &ent.FileAttachment{ID: coverID},
		},
	})

	require.NotNil(t, model)
	require.NotNil(t, model.CoverImageID)
	require.Equal(t, coverID, *model.CoverImageID)
	require.NotNil(t, model.CoverImageURL)
	require.Equal(t, "/api/image-gallery/"+coverID.String()+"?size=full", *model.CoverImageURL)
}

func TestToPersonalityModel_WithoutCoverImageKeepsCoverFieldsNil(t *testing.T) {
	t.Parallel()

	model := toPersonalityModel(&ent.Personality{
		ID:           uuid.New(),
		Name:         "Vera",
		SystemPrompt: "You are Vera.",
	})

	require.NotNil(t, model)
	require.Nil(t, model.CoverImageID)
	require.Nil(t, model.CoverImageURL)
}

func TestToPersonalityModel_FallsBackToDefaultExpressionImage(t *testing.T) {
	t.Parallel()

	happyID := uuid.New()
	defaultID := uuid.New()
	model := toPersonalityModel(&ent.Personality{
		ID:           uuid.New(),
		Name:         "Vera",
		SystemPrompt: "You are Vera.",
		Edges: ent.PersonalityEdges{
			Expressions: []*ent.PersonalityExpression{
				{
					ExpressionKey: "happy",
					Edges: ent.PersonalityExpressionEdges{
						Image: &ent.FileAttachment{ID: happyID},
					},
				},
				{
					ExpressionKey: "default",
					Edges: ent.PersonalityExpressionEdges{
						Image: &ent.FileAttachment{ID: defaultID},
					},
				},
			},
		},
	})

	require.NotNil(t, model)
	require.NotNil(t, model.CoverImageID)
	require.Equal(t, defaultID, *model.CoverImageID)
	require.NotNil(t, model.CoverImageURL)
	require.Equal(t, "/api/image-gallery/"+defaultID.String()+"?size=full", *model.CoverImageURL)
}

func TestToPersonalityModel_ExplicitCoverImageWinsOverExpressionImage(t *testing.T) {
	t.Parallel()

	coverID := uuid.New()
	defaultID := uuid.New()
	model := toPersonalityModel(&ent.Personality{
		ID:           uuid.New(),
		Name:         "Vera",
		SystemPrompt: "You are Vera.",
		Edges: ent.PersonalityEdges{
			CoverImage: &ent.FileAttachment{ID: coverID},
			Expressions: []*ent.PersonalityExpression{
				{
					ExpressionKey: "default",
					Edges: ent.PersonalityExpressionEdges{
						Image: &ent.FileAttachment{ID: defaultID},
					},
				},
			},
		},
	})

	require.NotNil(t, model)
	require.NotNil(t, model.CoverImageID)
	require.Equal(t, coverID, *model.CoverImageID)
}

func TestToPersonalityModel_MapsAccentAndThumbnailCircle(t *testing.T) {
	t.Parallel()

	accent := "#C2572A"
	model := toPersonalityModel(&ent.Personality{
		ID:           uuid.New(),
		Name:         "Vera",
		SystemPrompt: "You are Vera.",
		AccentColor:  &accent,
		ThumbnailCircle: &schema.PersonalityThumbnailCircle{
			CX: 0.5,
			CY: 0.42,
			R:  0.34,
		},
	})

	require.NotNil(t, model)
	require.NotNil(t, model.AccentColor)
	require.Equal(t, accent, *model.AccentColor)
	require.NotNil(t, model.ThumbnailCircle)
	require.InEpsilon(t, 0.5, model.ThumbnailCircle.CX, 0.0001)
	require.InEpsilon(t, 0.42, model.ThumbnailCircle.CY, 0.0001)
	require.InEpsilon(t, 0.34, model.ThumbnailCircle.R, 0.0001)
}
