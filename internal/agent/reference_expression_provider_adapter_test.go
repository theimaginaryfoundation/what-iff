package agent

import "testing"

func TestGridFallbackCannotClaimReferenceConditioning(t *testing.T) {
	if ExpressionGenerationMethodGridFallback == ExpressionGenerationMethodReferenceEdit ||
		ExpressionGenerationMethodGridFallback == ExpressionGenerationMethodReferenceGeneration {
		t.Fatal("grid fallback must remain distinguishable from reference-conditioned generation")
	}
}
