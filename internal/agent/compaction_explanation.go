package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

const checkpointExplanationMaxTokens = 256

// explainCheckpointChange asks the archival model for a short user-facing account of a state
// transition. It is audit metadata only: callers deliberately treat failure as best-effort so an
// explanation outage can never block the checkpoint that owns the authoritative before/after data.
func (a *Agent) explainCheckpointChange(ctx context.Context, userID uuid.UUID, kind, before, after string) (string, error) {
	before = strings.TrimSpace(before)
	after = strings.TrimSpace(after)
	if before == after {
		return "No change.", nil
	}
	if a == nil || a.OpenAIProvider == nil {
		return "", fmt.Errorf("checkpoint explanation provider unavailable")
	}

	prompt := fmt.Sprintf(`Explain the meaningful change between the BEFORE and AFTER %s states in one short sentence for the user.
Focus on what information was added, removed, clarified, consolidated, or reprioritized.
Do not judge whether the change was correct. Do not quote large passages. Respond with only the explanation sentence.

BEFORE:
%s

AFTER:
%s`, kind, before, after)

	params := responses.ResponseNewParams{
		Model:            archivalOpenAIModel,
		SafetyIdentifier: openai.String(userID.String()),
		MaxOutputTokens:  openai.Int(checkpointExplanationMaxTokens),
		ServiceTier:      responses.ResponseNewParamsServiceTierFlex,
		Instructions:     openai.String("You explain checkpoint audit changes concisely and literally."),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{
				responses.ResponseInputItemParamOfMessage(prompt, provider.RoleUser),
			},
		},
	}

	resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathScratchpad), params)
	if err != nil {
		return "", err
	}
	explanation := strings.TrimSpace(resp.OutputText())
	if explanation == "" {
		return "", fmt.Errorf("checkpoint explanation returned empty content")
	}
	return explanation, nil
}
