# embeddings-eval

Replays memory retrieval from an account export with the production embedding model (OpenAI
`text-embedding-3-small`) and one or more candidate models, and reports where their picks differ.
It answers one question: would a small local model pull memories from the *same neighborhood* on
real traffic? Ordering changes are fine; baseline picks that go missing entirely are not.

## What it does

1. Reads the export ZIP (`POST /account/export`): user turns from `conversations.json`, the memory
   corpus from the nested `memories.zip`.
2. Samples `-n` distinct user turns (seeded, so reruns pick the same ones).
3. Builds each turn's memory query:
   - `-query llm` (default): the production memory-query prompt and schema on the utility model,
     exactly like `Agent.getMemoryQuery`. One cheap LLM call per sampled turn, cached. Turns the
     model says need no memories are skipped, as on a live turn.
   - `-query raw`: embeds the user message itself. No LLM calls, but not what production embeds.
4. Embeds every memory any sampled turn could see, plus the queries, with the baseline and each
   candidate. Only those memories, and each text only once: vectors are cached under `-out/cache`.
5. Runs the `GetRelatedMemories` selection in memory: the chat's own Chat-scoped memories plus
   unpinned or same-personality User-scoped ones, L2 cutoff, top 5. With `-time-travel` (default)
   only memories created before the turn are visible.

Both models see the same corpus snapshot, so the comparison is model against model.

## Running it

Start a local OpenAI-compatible embeddings server for the candidates. Any of these work:

```bash
# Hugging Face text-embeddings-inference (CPU), closest to an ONNX/sidecar deployment
docker run --rm -p 8081:80 ghcr.io/huggingface/text-embeddings-inference:cpu-1.9 --model-id Snowflake/snowflake-arctic-embed-s --auto-truncate
```

```bash
# Ollama
ollama pull snowflake-arctic-embed:33m
```

```bash
# LM Studio (the GGUF model must already be downloaded)
lms server start && lms load nomic-ai/nomic-embed-text-v1.5-GGUF
```

Run a second TEI on `-p 8082:80 ... --model-id BAAI/bge-small-en-v1.5` for the bge entry. Copy
`candidates.example.json`, keep the entries your servers serve, then:

```bash
OPENAI_API_KEY=sk-... go run ./cmd/embeddings-eval -export ~/Downloads/account-export.zip -candidates candidates.json -n 200 -out ../embeval-out
```

The output directory holds real conversation and memory text. Keep it outside the repository
(`embeval-out/` is gitignored as a backstop).

Cost of a 200-turn run with `-query llm`: 200 utility-model calls, plus one OpenAI embedding pass
over the visible memories (~7k memories ≈ 0.3M tokens ≈ $0.01). Reruns hit the caches.

## Candidates file

A JSON list. Every field but `name` and `model` is optional.

| field | meaning |
|---|---|
| `name` | label in the report; also names the cache file |
| `base_url` | OpenAI-compatible root, e.g. `http://localhost:8081/v1` (empty = OpenAI) |
| `model` | model id the server expects |
| `api_key_env` | env var holding a key (local servers need none) |
| `query_prefix`, `doc_prefix` | instruction prefixes the model was trained with |
| `dimensions` | truncate to the first N components and renormalize (Matryoshka models only) |
| `threshold` | L2 cutoff to apply; omit to calibrate one so mean picks/turn match the baseline's |
| `batch_size` | inputs per request (default 64) |

The production cutoff (`datastore.MemoryRelevanceThreshold`, L2 1.2 ≈ cosine 0.28) was tuned for
OpenAI. Similarity distributions differ by model, so a candidate needs its own; the calibrated
value in `summary.md` is a starting point for that constant.

## Reading the output

- `summary.md`: one row per candidate. *baseline picks kept* is plain top-5 overlap; *in cand
  top 10 / 20* is the same-neighborhood bar; *missed* counts baseline picks the candidate ranked
  past 20. *baseline-model cos of picks* scores each side's picks in the baseline's geometry: a
  candidate near the baseline there is choosing memories OpenAI also considers close.
- `disagreements.md`: the worst turns per candidate, with both pick lists and each memory's rank
  in the other model's ordering. Eyeball these for "reordered" versus "missed".
- `turns.jsonl`: ids, distances and ranks per turn for your own analysis.

`-baseline-url` / `-baseline-model` swap the baseline for another model (useful for smoke tests
without an OpenAI key, or for comparing two local candidates head to head).
