package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
)

type receiptCaptureFileStore struct {
	key         string
	content     []byte
	contentType string
}

func (s *receiptCaptureFileStore) UploadFile(_ context.Context, key string, content []byte, contentType string) error {
	s.key = key
	s.content = append([]byte(nil), content...)
	s.contentType = contentType
	return nil
}

func (s *receiptCaptureFileStore) DownloadFile(context.Context, string) ([]byte, error) {
	return nil, nil
}

func (s *receiptCaptureFileStore) DeleteFile(context.Context, string) error {
	return nil
}

func TestReferenceQualityPathEligibilityRequiresRealCapabilityAndCanonicalBytes(t *testing.T) {
	t.Parallel()

	require.True(t, referenceQualityPathEligible(true, true, provider.ReferenceCapabilitySupported))
	require.False(t, referenceQualityPathEligible(false, true, provider.ReferenceCapabilitySupported))
	require.False(t, referenceQualityPathEligible(true, false, provider.ReferenceCapabilitySupported))
	require.False(t, referenceQualityPathEligible(true, true, provider.ReferenceCapabilityUnsupported))
	require.False(t, referenceQualityPathEligible(true, true, provider.ReferenceCapabilityUnavailable))
	require.False(t, referenceQualityPathEligible(true, true, provider.ReferenceCapabilityFailed))
}

func TestReferenceReceiptCannotClaimConditioningWithoutImageInput(t *testing.T) {
	t.Parallel()

	canonicalID := uuid.New()
	receipt := expressionGenerationReceipt{
		PersonalityID:          uuid.New(),
		ExpressionKey:          "happy",
		CanonicalImageID:       &canonicalID,
		CanonicalImageVersion:  canonicalImageVersion(&canonicalID),
		GenerationMethod:       provider.ExpressionGenerationMethodReferenceEdit,
		ReferenceCapability:    provider.ReferenceCapabilitySupported,
		ReferenceInputSupplied: false,
		Provider:               "openai",
	}
	require.ErrorContains(t, receipt.validate(), "reference input was not supplied")
}

func TestGridFallbackReceiptCannotMasqueradeAsReferenceConditioned(t *testing.T) {
	t.Parallel()

	canonicalID := uuid.New()
	receipt := expressionGenerationReceipt{
		PersonalityID:          uuid.New(),
		ExpressionKey:          "happy",
		CanonicalImageID:       &canonicalID,
		CanonicalImageVersion:  canonicalImageVersion(&canonicalID),
		GenerationMethod:       provider.ExpressionGenerationMethodGridFallback,
		ReferenceCapability:    provider.ReferenceCapabilitySupported,
		ReferenceInputSupplied: false,
		Provider:               "openai",
	}
	require.NoError(t, receipt.validate())

	receipt.ReferenceInputSupplied = true
	require.ErrorContains(t, receipt.validate(), "grid fallback cannot claim reference input")
}

func TestCanonicalImageVersionChangesWithCanonicalImage(t *testing.T) {
	t.Parallel()

	first := uuid.New()
	second := uuid.New()
	require.NotEmpty(t, canonicalImageVersion(&first))
	require.NotEqual(t, canonicalImageVersion(&first), canonicalImageVersion(&second))
	require.Empty(t, canonicalImageVersion(nil))
}

func TestPersistExpressionGenerationReceiptRecordsInspectableProvenance(t *testing.T) {
	t.Parallel()

	canonicalID := uuid.New()
	outputID := uuid.New()
	receipt := expressionGenerationReceipt{
		PersonalityID:          uuid.New(),
		ExpressionKey:          "happy",
		OutputImageID:          outputID,
		CanonicalImageID:       &canonicalID,
		CanonicalImageVersion:  canonicalImageVersion(&canonicalID),
		GenerationMethod:       provider.ExpressionGenerationMethodReferenceEdit,
		ReferenceCapability:    provider.ReferenceCapabilitySupported,
		ReferenceInputSupplied: true,
		Provider:               "openai",
	}

	store := &receiptCaptureFileStore{}
	a := &Agent{fileStore: store}
	require.NoError(t, a.persistExpressionGenerationReceipt(context.Background(), "users/u/expression-happy.png", receipt))
	require.Equal(t, "users/u/expression-happy.png.generation.json", store.key)
	require.Equal(t, "application/json", store.contentType)

	var persisted expressionGenerationReceipt
	require.NoError(t, json.Unmarshal(store.content, &persisted))
	require.Equal(t, receipt.PersonalityID, persisted.PersonalityID)
	require.Equal(t, receipt.CanonicalImageID, persisted.CanonicalImageID)
	require.Equal(t, receipt.CanonicalImageVersion, persisted.CanonicalImageVersion)
	require.Equal(t, receipt.GenerationMethod, persisted.GenerationMethod)
	require.Equal(t, receipt.Provider, persisted.Provider)
	require.True(t, persisted.ReferenceInputSupplied)
}
