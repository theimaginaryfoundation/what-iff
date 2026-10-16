// Command embeddings-eval replays memory retrieval from an account export with two embedding
// models and reports where their picks differ, to judge whether a small local model can replace
// OpenAI text-embedding-3-small for memory fetch (the "same neighborhood" bar: ordering changes
// are fine, baseline picks missing entirely are not).
//
// For a random sample of the export's user turns it rebuilds the turn's memory query with the
// production prompt (or uses the raw message with -query raw, which costs no LLM calls), embeds
// the query and every memory that turn could see with each model, and runs the
// GetRelatedMemories selection (visibility rules, L2 cutoff, top 5) in memory. Both models see
// the same corpus snapshot, so the comparison is model against model, not against whatever the
// corpus looked like when the turn was live.
//
//	OPENAI_API_KEY=... go run ./cmd/embeddings-eval \
//	  -export ~/Downloads/account-export.zip \
//	  -candidates cmd/embeddings-eval/candidates.example.json \
//	  -n 200 -out ../embeval-out
//
// Candidates are served by any OpenAI-compatible /v1/embeddings server (Ollama, Hugging Face
// TEI, infinity, ...). Embeddings and queries are cached under -out, so reruns are cheap. The
// output contains real conversation and memory text: keep -out outside the repository.
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// rawQueryMaxRunes bounds -query raw inputs: a pasted wall of text would be truncated (or
// rejected) differently by every server, and live turns never embed the raw message anyway.
const rawQueryMaxRunes = 2000

type options struct {
	exportPath, candidatesPath, outDir string
	n                                  int
	seed                               int64
	since                              string
	minChars                           int
	queryMode, queryModel              string
	concurrency                        int
	timeTravel, includeNoEnrich        bool
	worst                              int
	baselineURL, baselineModel         string
}

func main() {
	var o options
	flag.StringVar(&o.exportPath, "export", "", "account export ZIP (required)")
	flag.StringVar(&o.candidatesPath, "candidates", "", "JSON list of candidate embedders (required; see candidates.example.json)")
	flag.StringVar(&o.outDir, "out", "embeval-out", "output and cache directory (holds private text; keep it out of the repo)")
	flag.IntVar(&o.n, "n", 200, "user turns to sample")
	flag.Int64Var(&o.seed, "seed", 1, "sampling seed (same seed + export = same turns)")
	flag.StringVar(&o.since, "since", "", "only sample turns on or after this date (YYYY-MM-DD)")
	flag.IntVar(&o.minChars, "min-chars", 1, "skip user messages shorter than this")
	flag.StringVar(&o.queryMode, "query", "llm", "llm: rebuild each memory query with the production prompt; raw: embed the message itself (no LLM calls)")
	flag.StringVar(&o.queryModel, "query-model", models.UtilityModelName, "model for -query llm")
	flag.IntVar(&o.concurrency, "concurrency", 8, "parallel memory-query requests")
	flag.BoolVar(&o.timeTravel, "time-travel", true, "only consider memories created before each turn")
	flag.BoolVar(&o.includeNoEnrich, "include-no-enrich", false, "also compare turns the query model said need no memories")
	flag.IntVar(&o.worst, "worst", 25, "disagreements to list per candidate")
	flag.StringVar(&o.baselineURL, "baseline-url", "", "OpenAI-compatible root for the baseline (default OpenAI); for comparing against something other than production")
	flag.StringVar(&o.baselineModel, "baseline-model", "", "baseline model id (default "+baselineSpec.Model+")")
	flag.Parse()
	if o.exportPath == "" || o.candidatesPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(context.Background(), o); err != nil {
		fmt.Fprintln(os.Stderr, "embeddings-eval:", err)
		os.Exit(1)
	}
}

// baselineSpec is production retrieval: OpenAI text-embedding-3-small with the live cutoff.
var baselineSpec = embedderSpec{
	Name:      "openai-text-embedding-3-small",
	Model:     "text-embedding-3-small",
	Threshold: datastore.MemoryRelevanceThreshold,
	BatchSize: 256,
}

// sampledTurn is a turn that made it into the comparison, with its query.
type sampledTurn struct {
	evalTurn
	Query   string
	Visible []int
}

func run(ctx context.Context, o options) error {
	candidates, err := loadCandidates(o.candidatesPath)
	if err != nil {
		return err
	}
	if o.baselineURL != "" || o.baselineModel != "" {
		baselineSpec.BaseURL = o.baselineURL
		baselineSpec.Model = firstNonEmpty(o.baselineModel, baselineSpec.Model)
		baselineSpec.Name = "baseline-" + baselineSpec.Model
	}
	cacheDir := filepath.Join(o.outDir, "cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return err
	}

	logf("loading %s", o.exportPath)
	exp, err := loadExport(o.exportPath)
	if err != nil {
		return err
	}
	logf("export: %d user turns, %d memories", len(exp.Turns), len(exp.Memories))

	pool, err := eligibleTurns(exp.Turns, o)
	if err != nil {
		return err
	}
	sample := sampleTurns(pool, o.n, o.seed)
	logf("sampled %d of %d eligible turns (seed %d)", len(sample), len(pool), o.seed)

	turns, noEnrich, err := buildQueries(ctx, sample, o, filepath.Join(cacheDir, "queries.jsonl"))
	if err != nil {
		return err
	}
	for i := range turns {
		turns[i].Visible = visibleMemories(turns[i].evalTurn, exp.Memories, o.timeTravel)
	}
	needed := neededMemories(turns)
	logf("%d turns to compare (%d skipped: query model said no memories needed); %d memories to embed", len(turns), noEnrich, len(needed))

	specs := append([]embedderSpec{baselineSpec}, candidates...)
	vecs := make([][][]float32, len(specs))  // spec → memory index → vector (nil when not needed)
	qvecs := make([][][]float32, len(specs)) // spec → turn → query vector
	for s, spec := range specs {
		e, err := newEmbedder(spec, cacheDir)
		if err != nil {
			return err
		}
		texts := make([]string, len(needed))
		for i, m := range needed {
			texts[i] = exp.Memories[m].Content
		}
		docVecs, err := e.embedAll(ctx, texts, roleDoc, progressLogger(spec.Name+" memories"))
		if err != nil {
			return err
		}
		vecs[s] = make([][]float32, len(exp.Memories))
		for i, m := range needed {
			vecs[s][m] = docVecs[i]
		}
		queries := make([]string, len(turns))
		for i, t := range turns {
			queries[i] = t.Query
		}
		if qvecs[s], err = e.embedAll(ctx, queries, roleQuery, progressLogger(spec.Name+" queries")); err != nil {
			return err
		}
	}

	baseOrders := make([][]ranked, len(turns))
	var baseCount int
	for i, t := range turns {
		baseOrders[i] = rankAll(qvecs[0][i], t.Visible, vecs[0])
		baseCount += len(picks(baseOrders[i], baselineSpec.Threshold))
	}
	baseMean := float64(baseCount) / float64(max(1, len(turns)))

	results := make([]candidateResult, len(candidates))
	for c, spec := range candidates {
		orders := make([][]ranked, len(turns))
		for i, t := range turns {
			orders[i] = rankAll(qvecs[c+1][i], t.Visible, vecs[c+1])
		}
		threshold, calibrated := spec.Threshold, false
		if threshold == 0 {
			threshold, calibrated = calibrateThreshold(orders, baseMean), true
		}
		r := candidateResult{Spec: spec, Threshold: threshold, Calibrated: calibrated}
		for i := range turns {
			r.Turns = append(r.Turns, compareTurn(baseOrders[i], orders[i], baselineSpec.Threshold, threshold))
		}
		results[c] = r
	}

	rep := report{
		Options: o, Export: exp, Turns: turns, NoEnrich: noEnrich, Eligible: len(pool),
		BaseMeanPicks: baseMean, Results: results, Generated: time.Now(),
	}
	if err := rep.write(o.outDir); err != nil {
		return err
	}
	logf("wrote %s", filepath.Join(o.outDir, "summary.md"))
	return nil
}

// eligibleTurns applies -since and -min-chars and drops repeated texts ("continue", "lol"), which
// would otherwise be sampled in proportion to how often they are typed.
func eligibleTurns(all []evalTurn, o options) ([]evalTurn, error) {
	var since time.Time
	if o.since != "" {
		var err error
		if since, err = time.Parse("2006-01-02", o.since); err != nil {
			return nil, fmt.Errorf("-since: %w", err)
		}
	}
	seen := map[string]bool{}
	var out []evalTurn
	for _, t := range all {
		text := strings.TrimSpace(t.Text)
		if len([]rune(text)) < o.minChars || t.CreatedAt.Before(since) || seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, t)
	}
	return out, nil
}

func sampleTurns(pool []evalTurn, n int, seed int64) []evalTurn {
	idx := rand.New(rand.NewSource(seed)).Perm(len(pool))
	if n < len(idx) {
		idx = idx[:n]
	}
	out := make([]evalTurn, len(idx))
	for i, j := range idx {
		out[i] = pool[j]
	}
	return out
}

// buildQueries produces each sampled turn's memory query and drops the turns a live request would
// not have searched for, unless -include-no-enrich.
func buildQueries(ctx context.Context, sample []evalTurn, o options, cachePath string) ([]sampledTurn, int, error) {
	if o.queryMode == "raw" {
		out := make([]sampledTurn, len(sample))
		for i, t := range sample {
			q := []rune(strings.TrimSpace(t.Text))
			if len(q) > rawQueryMaxRunes {
				q = q[:rawQueryMaxRunes]
			}
			out[i] = sampledTurn{evalTurn: t, Query: string(q)}
		}
		return out, 0, nil
	}
	if o.queryMode != "llm" {
		return nil, 0, fmt.Errorf("-query must be llm or raw")
	}
	w, err := newQueryWriter(o.queryModel, cachePath)
	if err != nil {
		return nil, 0, err
	}
	defer w.Close()

	qs := make([]models.MemoryQuery, len(sample))
	errs := make([]error, len(sample))
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(1, o.concurrency))
	var mu sync.Mutex
	done := 0
	for i, t := range sample {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			qs[i], errs[i] = w.write(ctx, t.Text)
			mu.Lock()
			done++
			if done%25 == 0 || done == len(sample) {
				logf("memory queries: %d/%d", done, len(sample))
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	var out []sampledTurn
	noEnrich, failed := 0, 0
	for i, t := range sample {
		switch {
		case errs[i] != nil:
			failed++
			logf("memory query failed (turn skipped): %v", errs[i])
		case !qs[i].ShouldEnrich && !o.includeNoEnrich, strings.TrimSpace(qs[i].Query) == "":
			noEnrich++
		default:
			out = append(out, sampledTurn{evalTurn: t, Query: qs[i].Query})
		}
	}
	if failed > len(sample)/2 {
		return nil, 0, fmt.Errorf("%d of %d memory queries failed", failed, len(sample))
	}
	return out, noEnrich, nil
}

// neededMemories is the union of every compared turn's visible set, so only memories some turn
// could retrieve get embedded.
func neededMemories(turns []sampledTurn) []int {
	seen := map[int]bool{}
	var out []int
	for _, t := range turns {
		for _, m := range t.Visible {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

func progressLogger(label string) func(done, total int) {
	last := time.Time{}
	return func(done, total int) {
		if done == total || time.Since(last) > 5*time.Second {
			logf("%s: embedded %d/%d new", label, done, total)
			last = time.Now()
		}
	}
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05")}, args...)...)
}
