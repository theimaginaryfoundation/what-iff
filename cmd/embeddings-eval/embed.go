package main

import (
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// embedderSpec describes one embedding model reachable over an OpenAI-compatible /v1/embeddings
// endpoint: OpenAI itself for the baseline, or a local server (Ollama, Hugging Face TEI,
// infinity, ...) for a candidate. Going over HTTP keeps this tool inside the
// ADR 0x018 C2 rule (no local model runtime linked into the module).
type embedderSpec struct {
	// Name labels the model in the report; it also names its cache file.
	Name string `json:"name"`
	// BaseURL is the OpenAI-compatible API root, e.g. http://localhost:11434/v1. Empty means OpenAI.
	BaseURL string `json:"base_url,omitempty"`
	// Model is the model id the server expects.
	Model string `json:"model"`
	// APIKeyEnv names the environment variable holding the key. Empty uses OPENAI_API_KEY for
	// OpenAI and a placeholder for local servers (which ignore it).
	APIKeyEnv string `json:"api_key_env,omitempty"`
	// QueryPrefix and DocPrefix are the instruction prefixes some models are trained with
	// (arctic-embed / bge query instruction, nomic "search_query: " / "search_document: ").
	QueryPrefix string `json:"query_prefix,omitempty"`
	DocPrefix   string `json:"doc_prefix,omitempty"`
	// Dimensions, when set, truncates each vector to its first N components and renormalizes
	// (Matryoshka models such as nomic-embed-text-v1.5 or EmbeddingGemma).
	Dimensions int `json:"dimensions,omitempty"`
	// Threshold is the L2-distance cutoff to apply, like datastore.MemoryRelevanceThreshold for
	// OpenAI. Zero means calibrate one from the baseline (see calibrateThreshold).
	Threshold float64 `json:"threshold,omitempty"`
	// BatchSize bounds inputs per request (default 64).
	BatchSize int `json:"batch_size,omitempty"`
}

func loadCandidates(path string) ([]embedderSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var specs []embedderSpec
	if err := json.Unmarshal(raw, &specs); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("%s lists no candidates", path)
	}
	seen := map[string]bool{}
	for i, s := range specs {
		if s.Name == "" || s.Model == "" {
			return nil, fmt.Errorf("candidate %d: name and model are required", i)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("candidate %q listed twice", s.Name)
		}
		seen[s.Name] = true
	}
	return specs, nil
}

// embedder embeds texts for one spec, reading and filling an on-disk cache so a rerun (another
// sample size, another candidate) never pays for the same text twice.
type embedder struct {
	spec      embedderSpec
	client    openai.Client
	cache     map[string][]float32
	cachePath string
	dirty     int
}

func newEmbedder(spec embedderSpec, cacheDir string) (*embedder, error) {
	opts := []option.RequestOption{option.WithMaxRetries(3)}
	key := ""
	switch {
	case spec.APIKeyEnv != "":
		key = os.Getenv(spec.APIKeyEnv)
	case spec.BaseURL == "":
		key = os.Getenv("OPENAI_API_KEY")
	default:
		key = "unused" // local servers ignore the key, but the SDK insists on one
	}
	if key == "" {
		return nil, fmt.Errorf("%s: no API key (set %s)", spec.Name, firstNonEmpty(spec.APIKeyEnv, "OPENAI_API_KEY"))
	}
	opts = append(opts, option.WithAPIKey(key))
	if spec.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(strings.TrimRight(spec.BaseURL, "/")+"/"))
	}
	if spec.BatchSize <= 0 {
		spec.BatchSize = 64
	}
	e := &embedder{
		spec:      spec,
		client:    openai.NewClient(opts...),
		cache:     map[string][]float32{},
		cachePath: filepath.Join(cacheDir, slug(spec.Name)+".gob"),
	}
	if f, err := os.Open(e.cachePath); err == nil {
		defer f.Close()
		if err := gob.NewDecoder(f).Decode(&e.cache); err != nil {
			return nil, fmt.Errorf("%s: reading cache %s: %w", spec.Name, e.cachePath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return e, nil
}

type embedRole int

const (
	roleDoc embedRole = iota
	roleQuery
)

func (e *embedder) input(text string, role embedRole) string {
	if role == roleQuery {
		return e.spec.QueryPrefix + text
	}
	return e.spec.DocPrefix + text
}

// cacheKey covers everything that changes the vector: model, server, truncation and the exact
// (prefixed) input.
func (e *embedder) cacheKey(input string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", e.spec.BaseURL, e.spec.Model, e.spec.Dimensions, input)))
	return hex.EncodeToString(h[:])
}

// embedAll returns one unit-length vector per text, in order, calling the API only for texts not
// already cached. progress, when non-nil, is called after each request.
func (e *embedder) embedAll(ctx context.Context, texts []string, role embedRole, progress func(done, total int)) ([][]float32, error) {
	out := make([][]float32, len(texts))
	var missIdx []int
	for i, t := range texts {
		if v, ok := e.cache[e.cacheKey(e.input(t, role))]; ok {
			out[i] = v
		} else {
			missIdx = append(missIdx, i)
		}
	}
	for start := 0; start < len(missIdx); start += e.spec.BatchSize {
		end := min(start+e.spec.BatchSize, len(missIdx))
		batch := missIdx[start:end]
		inputs := make([]string, len(batch))
		for j, i := range batch {
			inputs[j] = e.input(texts[i], role)
		}
		vecs, err := e.request(ctx, inputs)
		if err != nil {
			_ = e.save() // keep what was already paid for
			return nil, fmt.Errorf("%s: %w", e.spec.Name, err)
		}
		for j, i := range batch {
			out[i] = vecs[j]
			e.cache[e.cacheKey(inputs[j])] = vecs[j]
		}
		e.dirty += len(batch)
		if e.dirty >= 1000 {
			if err := e.save(); err != nil {
				return nil, err
			}
		}
		if progress != nil {
			progress(end, len(missIdx))
		}
	}
	return out, e.save()
}

func (e *embedder) request(ctx context.Context, inputs []string) ([][]float32, error) {
	resp, err := e.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input:          openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: inputs},
		Model:          openai.EmbeddingModel(e.spec.Model),
		EncodingFormat: openai.EmbeddingNewParamsEncodingFormatFloat,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Data) != len(inputs) {
		return nil, fmt.Errorf("got %d vectors for %d inputs", len(resp.Data), len(inputs))
	}
	vecs := make([][]float32, len(inputs))
	for _, d := range resp.Data {
		if d.Index < 0 || int(d.Index) >= len(inputs) || vecs[d.Index] != nil {
			return nil, fmt.Errorf("bad response index %d", d.Index)
		}
		v := make([]float32, len(d.Embedding))
		for k, x := range d.Embedding {
			v[k] = float32(x)
		}
		if e.spec.Dimensions > 0 && e.spec.Dimensions < len(v) {
			v = v[:e.spec.Dimensions]
		}
		vecs[d.Index] = normalize(v)
	}
	return vecs, nil
}

func (e *embedder) save() error {
	if e.dirty == 0 {
		return nil
	}
	tmp := e.cachePath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(e.cache); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	e.dirty = 0
	return os.Rename(tmp, e.cachePath)
}

// normalize scales v to unit length. OpenAI vectors already are; local servers differ, and
// the L2 thresholds compared here only mean anything between unit vectors.
func normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
	return v
}

// l2 is pgvector's <-> operator.
func l2(a, b []float32) float64 {
	var sum float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	return math.Sqrt(sum)
}

var slugRE = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func slug(s string) string { return slugRE.ReplaceAllString(s, "_") }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
