package embedding

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
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

	resp, err := oaiClient.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: inputs,
		},
		Model:          openai.EmbeddingModelTextEmbedding3Small,
		Dimensions:     openai.Int(1536),
		EncodingFormat: "float",
	})
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
