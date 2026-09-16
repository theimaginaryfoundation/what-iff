package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
)

// Comparison harness for choosing the personality-generation model.
//
// TEMPORARY. This makes real, billed API calls, so it is skipped unless
// PERSONALITY_COMPARE=1 is set — it must never run in CI. Delete it once the
// model is chosen.
//
//	OPENAI_API_KEY=sk-... PERSONALITY_COMPARE=1 \
//	  go test ./internal/agent -run TestComparePersonalityModels -v
//
// Options:
//
//	PERSONALITY_COMPARE_MODELS  comma-separated (default "gpt-5.1,gpt-5.6-luna")
//	PERSONALITY_COMPARE_RUNS    generations per model (default 3)
//	PERSONALITY_COMPARE_OUT     output file (default ./personality-compare.md)
//
// Every model sees the identical prompt, schema and answers, so differences in
// the output are the model's and not the harness's. Runs are numbered rather
// than labelled with the model in the body, so the results can be read blind
// before checking which column produced what.
const compareEnv = "PERSONALITY_COMPARE"

// sampleAnswers stands in for the onboarding questionnaire. Deliberately
// specific: a vague prompt produces vague output from any model and would make
// the two look more alike than they are.
var sampleAnswers = map[string]string{
	"What should this personality help you with?": "Thinking through systems design problems out loud, and catching when I am about to over-engineer something.",
	"What tone do you want?":                      "Dry, direct, a bit funny. Willing to tell me I am wrong without softening it into mush.",
	"Anything it should avoid?":                   "Flattery, hedging, and restating my question back at me before answering.",
	"Any themes or aesthetics you like?":          "Old workshops, hand tools, astronomy. Things that reward patience.",
}

func TestComparePersonalityModels(t *testing.T) {
	if os.Getenv(compareEnv) != "1" {
		t.Skipf("set %s=1 to run this; it makes real, billed API calls", compareEnv)
	}
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if key == "" {
		t.Fatal("OPENAI_API_KEY is required")
	}

	models := strings.Split(envOr("PERSONALITY_COMPARE_MODELS", "gpt-5.1,gpt-5.6-luna"), ",")
	runs, err := strconv.Atoi(envOr("PERSONALITY_COMPARE_RUNS", "3"))
	if err != nil || runs < 1 {
		t.Fatalf("PERSONALITY_COMPARE_RUNS must be a positive integer")
	}
	out := envOr("PERSONALITY_COMPARE_OUT", "./personality-compare.md")

	client := openai.NewClient()
	var doc strings.Builder
	doc.WriteString("# Personality generation: model comparison\n\n")
	doc.WriteString(fmt.Sprintf("Same prompt, schema and answers for every run. %d runs per model.\n\n", runs))

	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		doc.WriteString(fmt.Sprintf("## %s\n\n", model))
		for i := 1; i <= runs; i++ {
			started := time.Now()
			result, err := generatePersonalityWith(context.Background(), &client, model)
			elapsed := time.Since(started).Round(time.Millisecond)
			if err != nil {
				doc.WriteString(fmt.Sprintf("### Run %d — FAILED after %s\n\n```\n%v\n```\n\n", i, elapsed, err))
				t.Logf("%s run %d failed: %v", model, i, err)
				continue
			}
			doc.WriteString(fmt.Sprintf("### Run %d — %s\n\n", i, elapsed))
			doc.WriteString(fmt.Sprintf("**Names:** %s\n\n", strings.Join(result.Names, ", ")))
			doc.WriteString(fmt.Sprintf("**About me** (%d chars)\n\n%s\n\n", len(result.AboutMe), result.AboutMe))
			doc.WriteString(fmt.Sprintf("**System prompt** (%d chars)\n\n%s\n\n---\n\n",
				len(result.SystemPrompt), result.SystemPrompt))
		}
	}

	if err := os.WriteFile(out, []byte(doc.String()), 0o600); err != nil {
		t.Fatalf("could not write %s: %v", out, err)
	}
	t.Logf("wrote %s", out)
}

// generatePersonalityWith mirrors GeneratePersonality's request exactly, with
// the model as the only variable. It does not go through Agent because that
// would drag in a datastore and telemetry this comparison has no use for.
func generatePersonalityWith(ctx context.Context, client *openai.Client, model string) (*GeneratePersonalityResult, error) {
	var sb strings.Builder
	sb.WriteString("Here are the user's answers to the personality-building questionnaire:\n\n")
	for key, val := range sampleAnswers {
		sb.WriteString(fmt.Sprintf("**%s**: %s\n", key, val))
	}

	resp, err := client.Responses.New(ctx, responses.ResponseNewParams{
		Model:           model,
		MaxOutputTokens: openai.Int(provider.DefaultMaxContentLength),
		Instructions:    openai.String(generatePersonalityPrompt),
		Input:           responses.ResponseNewParamsInputUnion{OfString: openai.String(sb.String())},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:        "GeneratePersonality",
					Schema:      generatePersonalitySchema,
					Strict:      openai.Bool(true),
					Description: openai.String("Generate Personality JSON"),
					Type:        "json_schema",
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	var result GeneratePersonalityResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp.OutputText())), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
