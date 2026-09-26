package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

type openAIEmbedding struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// CreateEmbedding calls the OpenAI Embeddings API to generate a vector for the given input text.
// Uses text-embedding-3-small model with 1536 dimensions.
func CreateEmbedding(ctx context.Context, oaiClient *openai.Client, input string) ([]float32, error) {
	embeddings, err := CreateEmbeddings(ctx, oaiClient, []string{input})
	if err != nil {
		return nil, err
	}
	return embeddings[0], nil
}

// CreateEmbeddings calls the OpenAI Embeddings API once for a batch of input text.
// The returned vectors preserve the input order, regardless of the API response order.
func CreateEmbeddings(ctx context.Context, oaiClient *openai.Client, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return []([]float32){}, nil
	}

	params := openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: inputs,
		},
		Model:          openai.EmbeddingModelTextEmbedding3Small,
		Dimensions:     openai.Int(1536),
		EncodingFormat: "float",
	}
	start := time.Now()
	resp, err := oaiClient.Embeddings.New(ctx, params)
	recordEmbeddingMetrics(ctx, string(params.Model), time.Since(start), resp, err)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("failed to generate embedding: empty embedding response")
	}

	embeddings := make([][]float32, len(inputs))
	seen := make([]bool, len(inputs))
	for _, data := range resp.Data {
		var emb openAIEmbedding
		if err := json.Unmarshal([]byte(data.RawJSON()), &emb); err != nil {
			return nil, fmt.Errorf("failed to unmarshal embedding: %w", err)
		}
		if emb.Index < 0 || emb.Index >= len(inputs) {
			return nil, fmt.Errorf("failed to generate embedding: response index %d outside input range", emb.Index)
		}
		if seen[emb.Index] {
			return nil, fmt.Errorf("failed to generate embedding: duplicate response index %d", emb.Index)
		}
		seen[emb.Index] = true
		embeddings[emb.Index] = emb.Embedding
	}

	for i := range embeddings {
		if !seen[i] {
			return nil, fmt.Errorf("failed to generate embedding: missing response for input index %d", i)
		}
	}
	return embeddings, nil
}

// recordEmbeddingMetrics records one Embeddings API call on the process-wide recorder: its
// duration (with error.type on failure) and, on success, its input tokens.
func recordEmbeddingMetrics(ctx context.Context, model string, d time.Duration, resp *openai.CreateEmbeddingResponse, err error) {
	m := telemetry.Global()
	provider := telemetry.AttrGenAIProvider.String(telemetry.DependencyOpenAI)
	modelAttr := telemetry.AttrGenAIModel.String(model)
	callPath := telemetry.AttrCallPath.String(string(telemetry.CallPathFromContext(ctx)))
	attrs := append([]attribute.KeyValue{provider, modelAttr, telemetry.AttrGenAIOperation.String("embeddings"), callPath},
		telemetry.ErrorAttrs(err)...)
	m.RecordDuration(ctx, telemetry.GenAIOperationDuration, d, attrs...)
	if err != nil || resp == nil || resp.Usage.PromptTokens <= 0 {
		return
	}
	tokenType := telemetry.AttrGenAITokenType.String(telemetry.TokenTypeInput)
	m.Record(ctx, telemetry.GenAITokenUsage, float64(resp.Usage.PromptTokens), provider, tokenType, callPath)
	m.Add(ctx, telemetry.GenAITokens, resp.Usage.PromptTokens, provider, modelAttr, tokenType, callPath)
}
