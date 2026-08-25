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
	resp, err := oaiClient.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: openai.String(input),
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

	var emb openAIEmbedding

	err = json.Unmarshal([]byte(resp.Data[0].RawJSON()), &emb)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal embedding: %w", err)
	}

	return emb.Embedding, nil
}
