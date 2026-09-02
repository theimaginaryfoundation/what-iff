package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func testGridCells() [][]byte {
	cells := make([][]byte, len(ExpressionGridKeys))
	for i, key := range ExpressionGridKeys {
		cells[i] = []byte("grid-" + key)
	}
	return cells
}

func TestPersistGridFallbackExpressionsRecordsNineExplicitFallbackReceipts(t *testing.T) {
	t.Parallel()

	canonicalID := uuid.New()
	fake := &recordingExpressionImageProvider{
		capability: provider.ReferenceCapabilitySupported,
		name:       "fake-reference-provider",
	}
	persisted := make([]persistedExpressionCell, 0, len(ExpressionGridKeys))
	a := &Agent{testHooks: agentTestHooks{
		ExpressionImageProviderOverride: fake,
		ExpressionPersistCellOverride: func(_ context.Context, _, _ uuid.UUID, key string, pngBytes []byte, receipt expressionGenerationReceipt) error {
			persisted = append(persisted, persistedExpressionCell{
				key:     key,
				png:     append([]byte(nil), pngBytes...),
				receipt: receipt,
			})
			return nil
		},
	}}

	personalityID := uuid.New()
	cells := testGridCells()
	err := a.persistGridFallbackExpressions(
		context.Background(),
		uuid.New(),
		personalityID,
		&models.Personality{CoverImageID: &canonicalID},
		cells,
	)
	require.NoError(t, err)
	require.Len(t, persisted, len(ExpressionGridKeys))
	require.Empty(t, fake.requests, "fallback persistence must not invoke reference generation")

	for i, key := range ExpressionGridKeys {
		got := persisted[i]
		require.Equal(t, key, got.key)
		require.Equal(t, cells[i], got.png)
		require.Equal(t, personalityID, got.receipt.PersonalityID)
		require.Equal(t, key, got.receipt.ExpressionKey)
		require.Equal(t, &canonicalID, got.receipt.CanonicalImageID)
		require.Equal(t, canonicalImageVersion(&canonicalID), got.receipt.CanonicalImageVersion)
		require.Equal(t, provider.ExpressionGenerationMethodGridFallback, got.receipt.GenerationMethod)
		require.Equal(t, provider.ReferenceCapabilitySupported, got.receipt.ReferenceCapability)
		require.False(t, got.receipt.ReferenceInputSupplied)
		require.Equal(t, "fake-reference-provider", got.receipt.Provider)
		require.NoError(t, got.receipt.validate())
	}
}

func TestPersistGridFallbackExpressionsMakesUnavailableProviderExplicit(t *testing.T) {
	t.Parallel()

	var persisted []expressionGenerationReceipt
	a := &Agent{testHooks: agentTestHooks{
		ExpressionPersistCellOverride: func(_ context.Context, _, _ uuid.UUID, _ string, _ []byte, receipt expressionGenerationReceipt) error {
			persisted = append(persisted, receipt)
			return nil
		},
	}}

	err := a.persistGridFallbackExpressions(context.Background(), uuid.New(), uuid.New(), nil, testGridCells())
	require.NoError(t, err)
	require.Len(t, persisted, len(ExpressionGridKeys))
	for _, receipt := range persisted {
		require.Nil(t, receipt.CanonicalImageID)
		require.Empty(t, receipt.CanonicalImageVersion)
		require.Equal(t, provider.ReferenceCapabilityUnavailable, receipt.ReferenceCapability)
		require.Equal(t, "unavailable", receipt.Provider)
		require.Equal(t, provider.ExpressionGenerationMethodGridFallback, receipt.GenerationMethod)
		require.False(t, receipt.ReferenceInputSupplied)
		require.NoError(t, receipt.validate())
	}
}

func TestPersistGridFallbackExpressionsRejectsWrongCellCountBeforeWriting(t *testing.T) {
	t.Parallel()

	persistCalls := 0
	a := &Agent{testHooks: agentTestHooks{
		ExpressionPersistCellOverride: func(context.Context, uuid.UUID, uuid.UUID, string, []byte, expressionGenerationReceipt) error {
			persistCalls++
			return nil
		},
	}}

	err := a.persistGridFallbackExpressions(context.Background(), uuid.New(), uuid.New(), nil, testGridCells()[:8])
	require.ErrorContains(t, err, "grid fallback: expected 9 cells, got 8")
	require.Zero(t, persistCalls)
}

func TestPersistGridFallbackExpressionsStopsAndNamesFailedSlot(t *testing.T) {
	t.Parallel()

	fake := &recordingExpressionImageProvider{
		capability: provider.ReferenceCapabilitySupported,
		name:       "fake",
	}
	persistCalls := 0
	a := &Agent{testHooks: agentTestHooks{
		ExpressionImageProviderOverride: fake,
		ExpressionPersistCellOverride: func(_ context.Context, _, _ uuid.UUID, key string, _ []byte, _ expressionGenerationReceipt) error {
			persistCalls++
			if key == "sad" {
				return errors.New("storage exploded")
			}
			return nil
		},
	}}

	err := a.persistGridFallbackExpressions(context.Background(), uuid.New(), uuid.New(), &models.Personality{}, testGridCells())
	require.ErrorContains(t, err, `expression "sad": storage exploded`)
	require.Equal(t, 3, persistCalls, "generation must stop on the first failed slot")
}
