package agent

// ExpressionGenerationMethod records how an expression image was produced.
type ExpressionGenerationMethod string

const (
	ExpressionGenerationMethodReferenceEdit       ExpressionGenerationMethod = "reference_edit"
	ExpressionGenerationMethodReferenceGeneration ExpressionGenerationMethod = "reference_generation"
	ExpressionGenerationMethodGridFallback        ExpressionGenerationMethod = "grid_fallback"
)

// ReferenceCapability records whether reference-conditioned generation is truly available.
type ReferenceCapability string

const (
	ReferenceCapabilitySupported   ReferenceCapability = "supported"
	ReferenceCapabilityUnsupported ReferenceCapability = "unsupported"
	ReferenceCapabilityUnavailable ReferenceCapability = "unavailable"
	ReferenceCapabilityFailed      ReferenceCapability = "failed"
)

// ExpressionReferenceRequest is provider-neutral: the domain supplies canonical image bytes
// plus the requested expression and text constraints. Provider adapters decide whether this
// maps to an image edit, reference-image generation, or another provider-native operation.
type ExpressionReferenceRequest struct {
	CanonicalImage     []byte
	CanonicalImageMIME string
	Expression         string
	Constraints        string
}

// ExpressionReferenceResult carries the generated PNG plus inspectable provenance.
type ExpressionReferenceResult struct {
	PNG                 []byte
	GenerationMethod    ExpressionGenerationMethod
	ReferenceCapability ReferenceCapability
	Provider             string
}

// ExpressionReferenceGenerator is the optional quality-path capability implemented by image providers.
// Providers that do not implement it deliberately fall back to the existing 3x3 grid path.
type ExpressionReferenceGenerator interface {
	GenerateExpressionFromReference(req ExpressionReferenceRequest) (ExpressionReferenceResult, error)
}
