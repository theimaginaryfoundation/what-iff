package agent

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
)

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
