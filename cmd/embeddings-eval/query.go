package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// queryRecord is one cached memory-query result, keyed by the turn text and the model that
// wrote it.
type queryRecord struct {
	Key          string `json:"key"`
	Model        string `json:"model"`
	ShouldEnrich bool   `json:"should_enrich"`
	Query        string `json:"query"`
}

// queryWriter turns a user message into the semantic search query, exactly as
// Agent.getMemoryQuery does on a live turn: the production prompt, schema and parameters, so the
// replayed query is the one the embedding model would really have been asked to embed.
type queryWriter struct {
	client openai.Client
	model  string
	schema map[string]any

	mu        sync.Mutex
	cache     map[string]queryRecord
	cacheFile *os.File
}

func newQueryWriter(model, cachePath string) (*queryWriter, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, errors.New("query generation needs OPENAI_API_KEY (or use -query raw)")
	}
	w := &queryWriter{
		client: openai.NewClient(option.WithAPIKey(key), option.WithMaxRetries(3)),
		model:  model,
		schema: provider.GenerateSchema[models.MemoryQuery](),
		cache:  map[string]queryRecord{},
	}
	if f, err := os.Open(cachePath); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var r queryRecord
			if json.Unmarshal(sc.Bytes(), &r) == nil {
				w.cache[r.Key] = r
			}
		}
		f.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(cachePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	w.cacheFile = f
	return w, nil
}

func (w *queryWriter) Close() error { return w.cacheFile.Close() }

func (w *queryWriter) key(text string) string {
	h := sha256.Sum256([]byte(w.model + "\x00" + agent.MemoryQueryPrompt + "\x00" + text))
	return hex.EncodeToString(h[:])
}

// write returns the memory query for one user message, from the cache when this model already
// answered it.
func (w *queryWriter) write(ctx context.Context, text string) (models.MemoryQuery, error) {
	k := w.key(text)
	w.mu.Lock()
	r, ok := w.cache[k]
	w.mu.Unlock()
	if ok {
		return models.MemoryQuery{Query: r.Query, ShouldEnrich: r.ShouldEnrich}, nil
	}

	resp, err := w.client.Responses.New(ctx, responses.ResponseNewParams{
		Model:           w.model,
		Temperature:     openai.Float(provider.DefaultTemperature),
		MaxOutputTokens: openai.Int(provider.DefaultMaxContentLength),
		Instructions:    openai.String(agent.MemoryQueryPrompt),
		Input:           responses.ResponseNewParamsInputUnion{OfString: openai.String(text)},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:        "MemoryQuery",
					Schema:      w.schema,
					Strict:      openai.Bool(true),
					Description: openai.String("Memory Query JSON"),
					Type:        "json_schema",
				},
			},
		},
	})
	if err != nil {
		return models.MemoryQuery{}, err
	}
	var q models.MemoryQuery
	if err := json.Unmarshal([]byte(resp.OutputText()), &q); err != nil {
		return models.MemoryQuery{}, fmt.Errorf("decoding memory query: %w", err)
	}

	rec := queryRecord{Key: k, Model: w.model, ShouldEnrich: q.ShouldEnrich, Query: q.Query}
	line, _ := json.Marshal(rec)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cache[k] = rec
	if _, err := w.cacheFile.Write(append(line, '\n')); err != nil {
		return q, err
	}
	return q, nil
}
