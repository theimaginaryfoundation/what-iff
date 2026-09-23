package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const expressionReferenceGenerationEnv = "EXPRESSION_REFERENCE_GENERATION_ENABLED"

// Phase 1 intentionally upgrades only two default expressions. The remaining
// seven continue through the cheap 3x3 grid path until the anchor is proven in
// production and Phase 2 can move the whole set to independent generation.
var phaseOneReferenceExpressionKeys = []string{"happy", "content"}

// expressionGenerationReceipt is persisted next to each generated image in S3.
// It makes the central invariant inspectable without requiring a schema migration:
// a reference-conditioned method may only be recorded when canonical image bytes
// were actually supplied to a provider that declared reference capability.
type expressionGenerationReceipt struct {
	PersonalityID          uuid.UUID                           `json:"personality_id"`
	ExpressionKey          string                              `json:"expression_key"`
	OutputImageID          uuid.UUID                           `json:"output_image_id"`
	CanonicalImageID       *uuid.UUID                          `json:"canonical_image_id"`
	CanonicalImageVersion  string                              `json:"canonical_image_version"`
	GenerationMethod       provider.ExpressionGenerationMethod `json:"generation_method"`
	ReferenceCapability    provider.ReferenceCapability        `json:"reference_capability"`
	ReferenceInputSupplied bool                                `json:"reference_input_supplied"`
	Provider               string                              `json:"provider"`
}

func (r expressionGenerationReceipt) validate() error {
	if r.PersonalityID == uuid.Nil {
		return fmt.Errorf("personality id is required")
	}
	if strings.TrimSpace(r.ExpressionKey) == "" {
		return fmt.Errorf("expression key is required")
	}
	if strings.TrimSpace(r.Provider) == "" {
		return fmt.Errorf("provider is required")
	}

	switch r.GenerationMethod {
	case provider.ExpressionGenerationMethodReferenceEdit, provider.ExpressionGenerationMethodReferenceGeneration:
		if r.ReferenceCapability != provider.ReferenceCapabilitySupported {
			return fmt.Errorf("reference-conditioned generation requires supported capability")
		}
		if r.CanonicalImageID == nil || *r.CanonicalImageID == uuid.Nil {
			return fmt.Errorf("reference-conditioned generation requires canonical image id")
		}
		if strings.TrimSpace(r.CanonicalImageVersion) == "" {
			return fmt.Errorf("reference-conditioned generation requires canonical image version")
		}
		if !r.ReferenceInputSupplied {
			return fmt.Errorf("reference input was not supplied")
		}
	case provider.ExpressionGenerationMethodGridFallback:
		if r.ReferenceInputSupplied {
			return fmt.Errorf("grid fallback cannot claim reference input")
		}
	default:
		return fmt.Errorf("unknown expression generation method %q", r.GenerationMethod)
	}

	return nil
}

// canonicalImageVersion uses the immutable attachment identity as the Phase 1
// version token. Replacing the canonical portrait creates a new attachment ID,
// which makes existing expression receipts mechanically stale-detectable without
// adding a database column before the quality path is proven.
func canonicalImageVersion(id *uuid.UUID) string {
	if id == nil || *id == uuid.Nil {
		return ""
	}
	return "attachment:" + id.String()
}

func referenceExpressionGenerationEnabled() bool {
	raw := strings.TrimSpace(os.Getenv(expressionReferenceGenerationEnv))
	if raw == "" {
		return false
	}
	enabled, err := strconv.ParseBool(raw)
	return err == nil && enabled
}

func referenceQualityPathEligible(enabled, hasCanonicalImage bool, capability provider.ReferenceCapability) bool {
	return enabled && hasCanonicalImage && capability == provider.ReferenceCapabilitySupported
}

// expressionImageProvider is the provider-neutral seam used by the expression
// domain. OpenAI is the first adapter; future provider wiring belongs here rather
// than in expression-generation logic.
func (a *Agent) expressionImageProvider() provider.ExpressionImageProvider {
	if a == nil {
		return nil
	}
	if a.testHooks.ExpressionImageProviderOverride != nil {
		return a.testHooks.ExpressionImageProviderOverride
	}
	if a.OpenAIProvider == nil {
		return nil
	}
	return a.OpenAIProvider
}

func expressionProviderState(p provider.ExpressionImageProvider) (provider.ReferenceCapability, string) {
	if p == nil {
		return provider.ReferenceCapabilityUnavailable, "unavailable"
	}
	return p.ExpressionReferenceCapability(), p.ExpressionProviderName()
}

// persistExpressionCell keeps Phase 1 orchestration testable without weakening
// the production persistence path. The override is reachable only through the
// package's test-hook mechanism, which is forbidden in production execution.
func (a *Agent) persistExpressionCell(
	ctx context.Context,
	userID, personalityID uuid.UUID,
	expressionKey string,
	pngBytes []byte,
	receipt expressionGenerationReceipt,
) error {
	if a != nil && a.testHooks.ExpressionPersistCellOverride != nil {
		return a.testHooks.ExpressionPersistCellOverride(ctx, userID, personalityID, expressionKey, pngBytes, receipt)
	}
	return a.uploadPersonalityExpressionCell(ctx, userID, personalityID, expressionKey, pngBytes, receipt)
}

// persistGridFallbackExpressions records the legacy 3x3 outputs with explicit
// fallback provenance before Phase 1 optionally overwrites selected slots via
// reference-conditioned generation. Keeping this transition in one helper makes
// the fallback/reference boundary directly unit-testable.
func (a *Agent) persistGridFallbackExpressions(
	ctx context.Context,
	userID, personalityID uuid.UUID,
	person *models.Personality,
	cells [][]byte,
) error {
	if len(cells) != len(ExpressionGridKeys) {
		return fmt.Errorf("grid fallback: expected %d cells, got %d", len(ExpressionGridKeys), len(cells))
	}

	var canonicalImageID *uuid.UUID
	if person != nil {
		canonicalImageID = person.CoverImageID
	}

	expressionProvider := a.expressionImageProvider()
	referenceCapability, expressionProviderName := expressionProviderState(expressionProvider)
	for i, key := range ExpressionGridKeys {
		receipt := expressionGenerationReceipt{
			PersonalityID:          personalityID,
			ExpressionKey:          key,
			CanonicalImageID:       canonicalImageID,
			CanonicalImageVersion:  canonicalImageVersion(canonicalImageID),
			GenerationMethod:       provider.ExpressionGenerationMethodGridFallback,
			ReferenceCapability:    referenceCapability,
			ReferenceInputSupplied: false,
			Provider:               expressionProviderName,
		}
		if err := a.persistExpressionCell(ctx, userID, personalityID, key, cells[i], receipt); err != nil {
			return fmt.Errorf("expression %q: %w", key, err)
		}
	}
	return nil
}

func (a *Agent) maybeGeneratePhaseOneReferenceExpressions(
	ctx context.Context,
	userID, personalityID uuid.UUID,
	person *models.Personality,
	canonicalImage []byte,
	canonicalMIME string,
	constraints string,
) error {
	p := a.expressionImageProvider()
	capability, providerName := expressionProviderState(p)
	enabled := referenceExpressionGenerationEnabled()
	hasCanonical := person != nil && person.CoverImageID != nil && len(canonicalImage) > 0
	if !referenceQualityPathEligible(enabled, hasCanonical, capability) {
		if enabled {
			a.logger.Info("expression reference quality path unavailable; retaining grid fallback",
				zap.String("provider", providerName),
				zap.String("reference_capability", string(capability)),
				zap.Bool("has_canonical_image", hasCanonical))
		}
		return nil
	}

	for _, expressionKey := range phaseOneReferenceExpressionKeys {
		// Every expression starts from the same immutable canonical portrait. Never
		// feed a generated expression back in as the next expression's reference.
		result, err := p.GenerateExpressionFromReference(ctx, provider.ExpressionReferenceRequest{
			CanonicalImage:     canonicalImage,
			CanonicalImageMIME: canonicalMIME,
			Expression:         expressionKey,
			Constraints:        constraints,
		})
		if err != nil {
			return fmt.Errorf("reference expression %q: %w", expressionKey, err)
		}
		if len(result.PNG) == 0 {
			return fmt.Errorf("reference expression %q: provider returned empty image", expressionKey)
		}
		if result.GenerationMethod != provider.ExpressionGenerationMethodReferenceEdit &&
			result.GenerationMethod != provider.ExpressionGenerationMethodReferenceGeneration {
			return fmt.Errorf("reference expression %q: provider returned non-reference generation method %q", expressionKey, result.GenerationMethod)
		}

		receipt := expressionGenerationReceipt{
			PersonalityID:          personalityID,
			ExpressionKey:          expressionKey,
			CanonicalImageID:       person.CoverImageID,
			CanonicalImageVersion:  canonicalImageVersion(person.CoverImageID),
			GenerationMethod:       result.GenerationMethod,
			ReferenceCapability:    result.ReferenceCapability,
			ReferenceInputSupplied: true,
			Provider:               result.Provider,
		}
		if err := receipt.validate(); err != nil {
			return fmt.Errorf("reference expression %q receipt: %w", expressionKey, err)
		}
		if err := a.persistExpressionCell(ctx, userID, personalityID, expressionKey, result.PNG, receipt); err != nil {
			return fmt.Errorf("reference expression %q persist: %w", expressionKey, err)
		}
	}

	return nil
}

func (a *Agent) persistExpressionGenerationReceipt(ctx context.Context, imageS3Key string, receipt expressionGenerationReceipt) error {
	if err := receipt.validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("marshal generation receipt: %w", err)
	}
	if err := a.fileStore.UploadFile(ctx, imageS3Key+".generation.json", payload, "application/json"); err != nil {
		return fmt.Errorf("upload generation receipt: %w", err)
	}
	return nil
}
