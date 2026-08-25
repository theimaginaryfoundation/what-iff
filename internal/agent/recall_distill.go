package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// recallDistillPrompt instructs the small archival model to answer a question strictly from the
// retrieved material and cite the numbered sources it used. Kept terse: the goal is to spend fewer
// context tokens than dumping raw chunks would.
const recallDistillPrompt = `You answer a question using ONLY the provided archival material (numbered memories and file/thread excerpts).

Rules:
- Answer the question directly and concisely. No preamble.
- Use only facts present in the material. If the material does not answer the question, say so plainly.
- Cite the source numbers you relied on inline, like [1] or [2][4].
- Do not invent details, dates, or names that are not in the material.`

// recallDistillMaxOutputTokens bounds the distilled answer; investigate answers should be short.
const recallDistillMaxOutputTokens = 500

// recallDistiller implements tools.RecallDistiller by calling the fixed archival OpenAI model.
// It closes over the Agent so it can reuse the configured OpenAIProvider.
type recallDistiller struct {
	a *Agent
}

func newRecallDistiller(a *Agent) *recallDistiller {
	return &recallDistiller{a: a}
}

func (d *recallDistiller) Distill(ctx context.Context, question, material string) (string, error) {
	if d == nil || d.a == nil || d.a.OpenAIProvider == nil {
		return "", fmt.Errorf("recall distiller: OpenAIProvider unavailable")
	}

	userContent := fmt.Sprintf("Question:\n%s\n\nMaterial:\n%s", strings.TrimSpace(question), strings.TrimSpace(material))
	params := responses.ResponseNewParams{
		Model:           archivalOpenAIModel,
		MaxOutputTokens: openai.Int(recallDistillMaxOutputTokens),
		ServiceTier:     responses.ResponseNewParamsServiceTierFlex,
		Instructions:    openai.String(recallDistillPrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{
				responses.ResponseInputItemParamOfMessage(userContent, provider.RoleUser),
			},
		},
	}

	resp, err := d.a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathMemory), params)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.OutputText()), nil
}
