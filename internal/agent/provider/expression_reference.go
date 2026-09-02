package provider

import "context"

// ExpressionGenerationMethod records how an expression asset was produced.
// Reference-conditioned methods are intentionally distinct from the legacy grid
// path so callers cannot claim identity-preserving generation when no image was
// supplied to the image model.
type ExpressionGenerationMethod string

const (
	ExpressionGenerationMethodReferenceEdit       ExpressionGenerationMethod = "reference_edit"
	ExpressionGenerationMethodReferenceGeneration ExpressionGenerationMethod = "reference_generation"
	ExpressionGenerationMethodGridFallback        ExpressionGenerationMethod = "grid_fallback"
)

// ReferenceCapability records whether a provider can genuinely accept a visual
// reference for expression generation.
type ReferenceCapability string

const (
	ReferenceCapabilitySupported   ReferenceCapability = "supported"
	ReferenceCapabilityUnsupported ReferenceCapability = "unsupported"
	ReferenceCapabilityUnavailable ReferenceCapability = "unavailable"
	ReferenceCapabilityFailed      ReferenceCapability = "failed"
)

// ExpressionReferenceRequest is provider-neutral. The expression domain supplies
// the immutable canonical portrait plus the requested expression and supplemental
// text constraints; the adapter decides which provider-native operation to use.
type ExpressionReferenceRequest struct {
	CanonicalImage     []byte
	CanonicalImageMIME string
	Expression         string
	Constraints        string
}

// ExpressionReferenceResult carries a generated PNG plus provider provenance.
type ExpressionReferenceResult struct {
	PNG                 []byte
	GenerationMethod    ExpressionGenerationMethod
	ReferenceCapability ReferenceCapability
	Provider             string
}

// ExpressionImageProvider is the quality-path capability for expression images.
// Implementations must only report a reference-conditioned GenerationMethod when
// the canonical image was actually supplied to the provider request.
type ExpressionImageProvider interface {
	ExpressionReferenceCapability() ReferenceCapability
	ExpressionProviderName() string
	GenerateExpressionFromReference(ctx context.Context, req ExpressionReferenceRequest) (ExpressionReferenceResult, error)
}
