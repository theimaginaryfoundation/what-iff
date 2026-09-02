package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type recordingExpressionImageProvider struct {
	capability provider.ReferenceCapability
	name       string
	requests   []provider.ExpressionReferenceRequest
	generate   func(provider.ExpressionReferenceRequest) (provider.ExpressionReferenceResult, error)
}

func (p *recordingExpressionImageProvider) ExpressionReferenceCapability() provider.ReferenceCapability {
	return p.capability
}

func (p *recordingExpressionImageProvider) ExpressionProviderName() string {
	return p.name
}

func (p *recordingExpressionImageProvider) GenerateExpressionFromReference(_ context.Context, req provider.ExpressionReferenceRequest) (provider.ExpressionReferenceResult, error) {
	p.requests = append(p.requests, req)
	if p.generate != nil {
		return p.generate(req)
	}
	return provider.ExpressionReferenceResult{
		PNG:                 []byte("png-" + req.Expression),
		GenerationMethod:    provider.ExpressionGenerationMethodReferenceEdit,
		ReferenceCapability: provider.ReferenceCapabilitySupported,
		Provider:            p.name,
	}, nil
}

type persistedExpressionCell struct {
	key     string
	png     []byte
	receipt expressionGenerationReceipt
}

func validReferenceReceipt() expressionGenerationReceipt {
	canonicalID := uuid.New()
	return expressionGenerationReceipt{
		PersonalityID:          uuid.New(),
		ExpressionKey:          "happy",
		CanonicalImageID:       &canonicalID,
		CanonicalImageVersion:  canonicalImageVersion(&canonicalID),
		GenerationMethod:       provider.ExpressionGenerationMethodReferenceEdit,
		ReferenceCapability:    provider.ReferenceCapabilitySupported,
		ReferenceInputSupplied: true,
		Provider:               "test-provider",
	}
}

func TestExpressionGenerationReceiptValidationRejectsInvalidProvenance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		edit func(*expressionGenerationReceipt)
		want string
	}{
		{"missing personality", func(r *expressionGenerationReceipt) { r.PersonalityID = uuid.Nil }, "personality id is required"},
		{"missing expression", func(r *expressionGenerationReceipt) { r.ExpressionKey = "" }, "expression key is required"},
		{"missing provider", func(r *expressionGenerationReceipt) { r.Provider = "" }, "provider is required"},
		{"unsupported capability", func(r *expressionGenerationReceipt) { r.ReferenceCapability = provider.ReferenceCapabilityUnsupported }, "requires supported capability"},
		{"missing canonical image", func(r *expressionGenerationReceipt) { r.CanonicalImageID = nil }, "requires canonical image id"},
		{"nil canonical image", func(r *expressionGenerationReceipt) { id := uuid.Nil; r.CanonicalImageID = &id }, "requires canonical image id"},
		{"missing canonical version", func(r *expressionGenerationReceipt) { r.CanonicalImageVersion = "" }, "requires canonical image version"},
		{"reference not supplied", func(r *expressionGenerationReceipt) { r.ReferenceInputSupplied = false }, "reference input was not supplied"},
		{"grid claims reference", func(r *expressionGenerationReceipt) { r.GenerationMethod = provider.ExpressionGenerationMethodGridFallback }, "grid fallback cannot claim reference input"},
		{"unknown method", func(r *expressionGenerationReceipt) { r.GenerationMethod = provider.ExpressionGenerationMethod("mystery") }, "unknown expression generation method"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			receipt := validReferenceReceipt()
			tc.edit(&receipt)
			require.ErrorContains(t, receipt.validate(), tc.want)
		})
	}

	receipt := validReferenceReceipt()
	receipt.GenerationMethod = provider.ExpressionGenerationMethodReferenceGeneration
	require.NoError(t, receipt.validate())
}

func TestReferenceExpressionGenerationEnabledParsing(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"true", true},
		{"1", true},
		{"false", false},
		{"not-a-bool", false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("value=%q", tc.value), func(t *testing.T) {
			t.Setenv(expressionReferenceGenerationEnv, tc.value)
			require.Equal(t, tc.want, referenceExpressionGenerationEnabled())
		})
	}
}

func TestExpressionProviderStateIsExplicit(t *testing.T) {
	t.Parallel()

	capability, name := expressionProviderState(nil)
	require.Equal(t, provider.ReferenceCapabilityUnavailable, capability)
	require.Equal(t, "unavailable", name)

	fake := &recordingExpressionImageProvider{capability: provider.ReferenceCapabilitySupported, name: "fake"}
	capability, name = expressionProviderState(fake)
	require.Equal(t, provider.ReferenceCapabilitySupported, capability)
	require.Equal(t, "fake", name)
}

func TestPhaseOneReferenceExpressionsUseSameCanonicalImageForEveryExpression(t *testing.T) {
	t.Setenv(expressionReferenceGenerationEnv, "true")

	canonicalID := uuid.New()
	canonical := []byte("same-canonical-image")
	fake := &recordingExpressionImageProvider{
		capability: provider.ReferenceCapabilitySupported,
		name:       "fake-reference-provider",
	}
	persisted := make([]persistedExpressionCell, 0, len(phaseOneReferenceExpressionKeys))
	a := &Agent{
		logger: zap.NewNop(),
		testHooks: agentTestHooks{
			ExpressionImageProviderOverride: fake,
			ExpressionPersistCellOverride: func(_ context.Context, _, _ uuid.UUID, key string, pngBytes []byte, receipt expressionGenerationReceipt) error {
				persisted = append(persisted, persistedExpressionCell{key: key, png: append([]byte(nil), pngBytes...), receipt: receipt})
				return nil
			},
		},
	}

	personalityID := uuid.New()
	person := &models.Personality{CoverImageID: &canonicalID}
	err := a.maybeGeneratePhaseOneReferenceExpressions(
		context.Background(), uuid.New(), personalityID, person,
		canonical, "image/png", "short dark hair; thin rectangular glasses",
	)
	require.NoError(t, err)
	require.Len(t, fake.requests, 2)
	require.Len(t, persisted, 2)

	for i, key := range phaseOneReferenceExpressionKeys {
		req := fake.requests[i]
		require.Equal(t, key, req.Expression)
		require.Equal(t, canonical, req.CanonicalImage, "each expression must start from the exact canonical bytes")
		require.Equal(t, "image/png", req.CanonicalImageMIME)
		require.Equal(t, "short dark hair; thin rectangular glasses", req.Constraints)

		got := persisted[i]
		require.Equal(t, key, got.key)
		require.Equal(t, []byte("png-"+key), got.png)
		require.Equal(t, personalityID, got.receipt.PersonalityID)
		require.Equal(t, &canonicalID, got.receipt.CanonicalImageID)
		require.Equal(t, canonicalImageVersion(&canonicalID), got.receipt.CanonicalImageVersion)
		require.Equal(t, provider.ExpressionGenerationMethodReferenceEdit, got.receipt.GenerationMethod)
		require.Equal(t, provider.ReferenceCapabilitySupported, got.receipt.ReferenceCapability)
		require.True(t, got.receipt.ReferenceInputSupplied)
		require.Equal(t, "fake-reference-provider", got.receipt.Provider)
		require.NoError(t, got.receipt.validate())
	}
}

func TestPhaseOneReferenceExpressionsDeliberatelyRetainFallbackWhenIneligible(t *testing.T) {
	canonicalID := uuid.New()
	canonical := []byte("canonical")

	cases := []struct {
		name       string
		enabled    string
		person     *models.Personality
		image      []byte
		capability provider.ReferenceCapability
	}{
		{"flag disabled", "false", &models.Personality{CoverImageID: &canonicalID}, canonical, provider.ReferenceCapabilitySupported},
		{"missing cover id", "true", &models.Personality{}, canonical, provider.ReferenceCapabilitySupported},
		{"nil personality", "true", nil, canonical, provider.ReferenceCapabilitySupported},
		{"missing canonical bytes", "true", &models.Personality{CoverImageID: &canonicalID}, nil, provider.ReferenceCapabilitySupported},
		{"unsupported provider", "true", &models.Personality{CoverImageID: &canonicalID}, canonical, provider.ReferenceCapabilityUnsupported},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(expressionReferenceGenerationEnv, tc.enabled)
			fake := &recordingExpressionImageProvider{capability: tc.capability, name: "fake"}
			persistCalls := 0
			a := &Agent{
				logger: zap.NewNop(),
				testHooks: agentTestHooks{
					ExpressionImageProviderOverride: fake,
					ExpressionPersistCellOverride: func(context.Context, uuid.UUID, uuid.UUID, string, []byte, expressionGenerationReceipt) error {
						persistCalls++
						return nil
					},
				},
			}
			err := a.maybeGeneratePhaseOneReferenceExpressions(context.Background(), uuid.New(), uuid.New(), tc.person, tc.image, "image/png", "constraints")
			require.NoError(t, err)
			require.Empty(t, fake.requests)
			require.Zero(t, persistCalls)
		})
	}
}

func TestPhaseOneReferenceExpressionsNilProviderRetainsFallback(t *testing.T) {
	t.Setenv(expressionReferenceGenerationEnv, "true")
	canonicalID := uuid.New()
	a := &Agent{logger: zap.NewNop()}
	err := a.maybeGeneratePhaseOneReferenceExpressions(
		context.Background(), uuid.New(), uuid.New(),
		&models.Personality{CoverImageID: &canonicalID}, []byte("canonical"), "image/png", "constraints",
	)
	require.NoError(t, err)
}

func TestPhaseOneReferenceExpressionsSurfaceProviderAndPersistenceFailures(t *testing.T) {
	canonicalID := uuid.New()
	person := &models.Personality{CoverImageID: &canonicalID}
	canonical := []byte("canonical")

	cases := []struct {
		name        string
		result      provider.ExpressionReferenceResult
		providerErr error
		persistErr  error
		want        string
	}{
		{
			name:        "provider error",
			providerErr: errors.New("provider exploded"),
			want:        `reference expression "happy": provider exploded`,
		},
		{
			name: "empty image",
			result: provider.ExpressionReferenceResult{
				GenerationMethod: provider.ExpressionGenerationMethodReferenceEdit,
				ReferenceCapability: provider.ReferenceCapabilitySupported,
				Provider: "fake",
			},
			want: "provider returned empty image",
		},
		{
			name: "non reference method",
			result: provider.ExpressionReferenceResult{
				PNG: []byte("png"), GenerationMethod: provider.ExpressionGenerationMethodGridFallback,
				ReferenceCapability: provider.ReferenceCapabilitySupported, Provider: "fake",
			},
			want: "provider returned non-reference generation method",
		},
		{
			name: "receipt capability mismatch",
			result: provider.ExpressionReferenceResult{
				PNG: []byte("png"), GenerationMethod: provider.ExpressionGenerationMethodReferenceEdit,
				ReferenceCapability: provider.ReferenceCapabilityUnsupported, Provider: "fake",
			},
			want: "receipt: reference-conditioned generation requires supported capability",
		},
		{
			name: "receipt provider missing",
			result: provider.ExpressionReferenceResult{
				PNG: []byte("png"), GenerationMethod: provider.ExpressionGenerationMethodReferenceEdit,
				ReferenceCapability: provider.ReferenceCapabilitySupported, Provider: "",
			},
			want: "receipt: provider is required",
		},
		{
			name: "persistence failure",
			result: provider.ExpressionReferenceResult{
				PNG: []byte("png"), GenerationMethod: provider.ExpressionGenerationMethodReferenceEdit,
				ReferenceCapability: provider.ReferenceCapabilitySupported, Provider: "fake",
			},
			persistErr: errors.New("storage exploded"),
			want:       `reference expression "happy" persist: storage exploded`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(expressionReferenceGenerationEnv, "true")
			fake := &recordingExpressionImageProvider{
				capability: provider.ReferenceCapabilitySupported,
				name:       "fake",
				generate: func(provider.ExpressionReferenceRequest) (provider.ExpressionReferenceResult, error) {
					return tc.result, tc.providerErr
				},
			}
			a := &Agent{
				logger: zap.NewNop(),
				testHooks: agentTestHooks{
					ExpressionImageProviderOverride: fake,
					ExpressionPersistCellOverride: func(context.Context, uuid.UUID, uuid.UUID, string, []byte, expressionGenerationReceipt) error {
						return tc.persistErr
					},
				},
			}
			err := a.maybeGeneratePhaseOneReferenceExpressions(context.Background(), uuid.New(), uuid.New(), person, canonical, "image/png", "constraints")
			require.ErrorContains(t, err, tc.want)
			require.Len(t, fake.requests, 1)
		})
	}
}

type failingReceiptFileStore struct{ err error }

func (s *failingReceiptFileStore) UploadFile(context.Context, string, []byte, string) error { return s.err }
func (s *failingReceiptFileStore) DownloadFile(context.Context, string) ([]byte, error)      { return nil, nil }
func (s *failingReceiptFileStore) DeleteFile(context.Context, string) error                 { return nil }

func TestPersistExpressionGenerationReceiptRejectsInvalidReceiptAndUploadFailure(t *testing.T) {
	t.Parallel()

	invalid := validReferenceReceipt()
	invalid.Provider = ""
	a := &Agent{fileStore: &failingReceiptFileStore{err: errors.New("should not be reached")}}
	require.ErrorContains(t, a.persistExpressionGenerationReceipt(context.Background(), "image.png", invalid), "provider is required")

	valid := validReferenceReceipt()
	a = &Agent{fileStore: &failingReceiptFileStore{err: errors.New("s3 unavailable")}}
	require.ErrorContains(t, a.persistExpressionGenerationReceipt(context.Background(), "image.png", valid), "upload generation receipt: s3 unavailable")
}
