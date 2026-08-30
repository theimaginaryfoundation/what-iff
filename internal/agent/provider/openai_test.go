package provider

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// testProcessResponseOutput adapts raw text into the real OpenAI Response shape and then calls the
// production ProcessResponseOutput path. It intentionally contains no copy of the deduplication
// algorithm, so these tests fail when production response processing regresses.
func testProcessResponseOutput(rawOutput string) string {
	quoted, err := json.Marshal(rawOutput)
	if err != nil {
		panic(err)
	}
	raw := `{"id":"resp_test","object":"response","created_at":1,"model":"test","status":"completed",` +
		`"output":[{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":` + string(quoted) + `}]}]}`
	var resp responses.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		panic(err)
	}
	return ProcessResponseOutput(&resp)
}

func TestProcessResponseOutput_NilResponse(t *testing.T) {
	t.Parallel()
	require.Empty(t, ProcessResponseOutput(nil))
}

func TestProcessResponseOutput_EmptyString(t *testing.T) {
	result := testProcessResponseOutput("")
	assert.Empty(t, result, "Empty string should return empty string")
}

func TestProcessResponseOutput_ShortContent(t *testing.T) {
	shortText := "This is a short response."
	result := testProcessResponseOutput(shortText)
	assert.Equal(t, shortText, result, "Short content should pass through unchanged")
}

func TestProcessResponseOutput_ShortContentAtBoundary(t *testing.T) {
	// Create content exactly 100 characters
	shortText := "This is exactly one hundred characters long for testing the boundary condition of the short check!!!"
	assert.Equal(t, 100, len(shortText), "Test content should be exactly 100 chars")

	result := testProcessResponseOutput(shortText)
	assert.Equal(t, shortText, result, "Content at 100 char boundary should pass through unchanged")
}

func TestProcessResponseOutput_NoduplicateContent(t *testing.T) {
	content := `This is the first paragraph with unique content about AI research.

This is the second paragraph with different information about machine learning.

Here's a third paragraph discussing natural language processing and its applications.`

	result := testProcessResponseOutput(content)
	assert.Equal(t, content, result, "Content without duplicates should remain unchanged")
}

func TestProcessResponseOutput_ExactDuplicates(t *testing.T) {
	content := `This is a unique paragraph about artificial intelligence.

This is a duplicate paragraph that appears twice.

This is a unique paragraph about artificial intelligence.

This is a duplicate paragraph that appears twice.

This is another unique paragraph about machine learning.`

	expectedResult := `This is a unique paragraph about artificial intelligence.

This is a duplicate paragraph that appears twice.

This is another unique paragraph about machine learning.`

	result := testProcessResponseOutput(content)
	assert.Equal(t, expectedResult, result, "Exact duplicate paragraphs should be removed")
}

func TestProcessResponseOutput_WhitespaceDifferences(t *testing.T) {
	content := `This is a paragraph with normal spacing.

This   is   a   paragraph   with   extra   spaces.

This is a paragraph with normal spacing.

This is a paragraph with normal spacing.`

	// The production pipeline deduplicates paragraphs and then StripOpenAIFileLinks normalizes
	// repeated spaces/tabs, so the surviving unique paragraph uses single internal spaces.
	expectedResult := `This is a paragraph with normal spacing.

This is a paragraph with extra spaces.`

	result := testProcessResponseOutput(content)
	assert.Equal(t, expectedResult, result, "Duplicate paragraphs should be deduplicated")
}

func TestProcessResponseOutput_MultipleInstancesOfSameContent(t *testing.T) {
	content := `Original content here.

Duplicate content appears here.

Some other unique content.

Duplicate content appears here.

More unique content follows.

Duplicate content appears here.

Final unique paragraph.`

	expectedResult := `Original content here.

Duplicate content appears here.

Some other unique content.

More unique content follows.

Final unique paragraph.`

	result := testProcessResponseOutput(content)
	assert.Equal(t, expectedResult, result, "Multiple instances of duplicate content should be reduced to one")
}

func TestProcessResponseOutput_EmptyParagraphs(t *testing.T) {
	content := `First paragraph with content.



Second paragraph after empty lines.

Third paragraph.



Fourth paragraph after more empty lines.`

	// Empty paragraphs should now be preserved for proper markdown formatting
	expectedResult := content

	result := testProcessResponseOutput(content)
	assert.Equal(t, expectedResult, result, "Empty paragraphs should be preserved for markdown formatting")
}

func TestProcessResponseOutput_OnlyWhitespace(t *testing.T) {
	// Short whitespace content passes through unchanged (< 100 chars)
	content := "   \n\n   \t\t\t   \n\n   "
	result := testProcessResponseOutput(content)
	assert.Equal(t, content, result, "Short whitespace content should pass through unchanged")
}

func TestProcessResponseOutput_LongWhitespaceContent(t *testing.T) {
	// Test that long whitespace-heavy content is handled gracefully
	content := strings.Repeat("   \n\n   \n\n", 20) // Makes it > 100 chars
	result := testProcessResponseOutput(content)

	// Main requirement: function should not panic and should return a string
	assert.NotNil(t, result, "Function should return a valid string")
	assert.IsType(t, "", result, "Result should be a string")

	// The exact whitespace handling behavior may vary, but it should be consistent
	// This test primarily ensures the function handles edge cases gracefully
}

func TestProcessResponseOutput_WhitespaceNormalization(t *testing.T) {
	// Test that minor whitespace differences are properly normalized and deduplicated
	content := `This has normal spacing between words and should be deduplicated properly when repeated.

Some other unique content that should remain in the output.

This has  normal  spacing  between  words  and  should  be  deduplicated  properly  when  repeated.

This has normal spacing between words and should be deduplicated properly when repeated.`

	expectedResult := `This has normal spacing between words and should be deduplicated properly when repeated.

Some other unique content that should remain in the output.`

	result := testProcessResponseOutput(content)
	assert.Equal(t, expectedResult, result, "Content with minor whitespace differences should be deduplicated")
}

func TestProcessResponseOutput_ParallelToolCallSimulation(t *testing.T) {
	// Simulate content that might come from parallel tool calls with duplicates
	content := `Based on my research, artificial intelligence is rapidly evolving.

Here are the key findings from web search:
- AI applications are growing
- Machine learning is advancing
- Natural language processing is improving

Based on my research, artificial intelligence is rapidly evolving.

Additional information from another source:
- Deep learning is revolutionizing AI
- Computer vision has many applications
- Robotics is integrating AI technologies

Here are the key findings from web search:
- AI applications are growing
- Machine learning is advancing
- Natural language processing is improving`

	expectedResult := `Based on my research, artificial intelligence is rapidly evolving.

Here are the key findings from web search:
- AI applications are growing
- Machine learning is advancing
- Natural language processing is improving

Additional information from another source:
- Deep learning is revolutionizing AI
- Computer vision has many applications
- Robotics is integrating AI technologies`

	result := testProcessResponseOutput(content)
	assert.Equal(t, expectedResult, result, "Simulated parallel tool call duplicates should be removed")
}

func TestProcessResponseOutput_SpecialCharacters(t *testing.T) {
	content := `Here's content with special characters: @#$%^&*()!

This content has émojis and unicodé: 🤖 🔬 ✨

Here's content with special characters: @#$%^&*()!

Different content with symbols: <>&"'{}[]

This content has émojis and unicodé: 🤖 🔬 ✨`

	expectedResult := `Here's content with special characters: @#$%^&*()!

This content has émojis and unicodé: 🤖 🔬 ✨

Different content with symbols: <>&"'{}[]`

	result := testProcessResponseOutput(content)
	assert.Equal(t, expectedResult, result, "Content with special characters should be deduplicated correctly")
}

func TestProcessResponseOutput_VeryLongContent(t *testing.T) {
	// Create a long paragraph to test performance and correctness
	longParagraph := "This is a very long paragraph that repeats the same information over and over again. " +
		"It contains detailed information about artificial intelligence, machine learning, and natural language processing. " +
		"The purpose is to test how the deduplication function handles longer content blocks that exceed the 100 character threshold. " +
		"This content should be processed for deduplication rather than passed through unchanged."

	content := longParagraph + "\n\n" + "Unique middle content goes here." + "\n\n" + longParagraph

	expectedResult := longParagraph + "\n\n" + "Unique middle content goes here."

	result := testProcessResponseOutput(content)
	assert.Equal(t, expectedResult, result, "Long content with duplicates should be properly deduplicated")
}

func TestProcessResponseOutput_SingleParagraphDuplication(t *testing.T) {
	// Edge case: single paragraph repeated
	paragraph := "This is a single paragraph that gets repeated multiple times without any line breaks between repetitions."
	content := paragraph + "\n\n" + paragraph + "\n\n" + paragraph

	result := testProcessResponseOutput(content)
	assert.Equal(t, paragraph, result, "Single paragraph repeated should be reduced to one instance")
}

func TestProcessResponseOutput_MarkdownTables(t *testing.T) {
	// Test that markdown tables are properly preserved with their spacing
	content := `Here are the results:

| Name | Age | City |
|------|-----|------|
| John | 25  | NYC  |
| Jane | 30  | LA   |

Some additional text after the table.`

	result := testProcessResponseOutput(content)
	assert.Equal(t, content, result, "Markdown tables should be preserved with proper spacing")
}

func TestProcessResponseOutput_MarkdownTableWithDuplicates(t *testing.T) {
	// Test deduplication still works with markdown tables
	content := `Here are the results:

| Name | Age |
|------|-----|
| John | 25  |

Here are the results:

| Name | Age |
|------|-----|
| John | 25  |

Final paragraph.`

	expectedResult := `Here are the results:

| Name | Age |
|------|-----|
| John | 25  |

Final paragraph.`

	result := testProcessResponseOutput(content)
	assert.Equal(t, expectedResult, result, "Duplicate markdown tables should be removed while preserving formatting")
}

// Benchmark test to ensure performance is acceptable
func BenchmarkProcessResponseOutput(b *testing.B) {
	// Create a realistic content scenario with some duplicates.
	content := `This is a research summary about artificial intelligence and its applications.

Key findings from multiple sources indicate significant growth in AI adoption across industries.

Machine learning algorithms are becoming more sophisticated and efficient each year.

This is a research summary about artificial intelligence and its applications.

Deep learning neural networks are revolutionizing computer vision and natural language processing.

Key findings from multiple sources indicate significant growth in AI adoption across industries.

Robotics integration with AI is creating new opportunities in manufacturing and service sectors.

The future of AI development looks promising with continued investment and research breakthroughs.`

	// Build the SDK response fixture once. The benchmark is intended to measure
	// ProcessResponseOutput, not JSON marshaling/unmarshaling in the test adapter.
	quoted, err := json.Marshal(content)
	if err != nil {
		b.Fatal(err)
	}
	raw := `{"id":"resp_bench","object":"response","created_at":1,"model":"test","status":"completed",` +
		`"output":[{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":` + string(quoted) + `}]}]}`
	var resp responses.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ProcessResponseOutput(&resp)
	}
}

// responseWithOutputText builds a *responses.Response whose OutputText() returns text, so
// ProcessResponseOutput (and therefore dedupeParagraphs) can be exercised directly.
func responseWithOutputText(t *testing.T, text string) *responses.Response {
	t.Helper()
	quoted, err := json.Marshal(text)
	require.NoError(t, err)
	raw := `{"id":"resp_1","object":"response","created_at":1,"model":"test","status":"completed",` +
		`"output":[{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":` + string(quoted) + `}]}]}`
	var resp responses.Response
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	return &resp
}

func TestProcessResponseOutput_RealResponse_Dedupes(t *testing.T) {
	t.Parallel()
	content := "This is a unique paragraph about artificial intelligence, long enough to pass the short-message threshold.\n\n" +
		"This is a unique paragraph about artificial intelligence, long enough to pass the short-message threshold.\n\n" +
		"This is a second, different paragraph that is also long enough to matter for the test."
	resp := responseWithOutputText(t, content)

	got := ProcessResponseOutput(resp)
	require.Equal(t, 1, strings.Count(got, "artificial intelligence"), "exact duplicate paragraph must be folded to one copy")
	require.Contains(t, got, "second, different paragraph")
}

func TestProcessResponseOutput_RealResponse_ShortContentPassesThrough(t *testing.T) {
	t.Parallel()
	resp := responseWithOutputText(t, "short")
	require.Equal(t, "short", ProcessResponseOutput(resp))
}

func TestDedupeParagraphs_PreservesEmptyParagraphsAndDedupes(t *testing.T) {
	t.Parallel()
	in := []string{"Same content here.", "", "Same   content  here.", "Different."}
	out := dedupeParagraphs(in)
	require.Equal(t, []string{"Same content here.", "", "Different."}, out)
}

func TestDedupeParagraphs_Empty(t *testing.T) {
	t.Parallel()
	require.Nil(t, dedupeParagraphs(nil))
}

func TestOpenAIProvider_SelectCarryOverTurns(t *testing.T) {
	t.Parallel()
	c := &OpenAIProvider{tokenCounter: NewTokenCounter()}
	now := time.Now()
	recent := []*models.ChatMessage{
		{Origin: models.MessageOriginAssistant, Message: "reply", SentAt: now},
		{Origin: models.MessageOriginUser, Message: "question", SentAt: now.Add(-time.Second)},
	}
	turns := c.SelectCarryOverTurns(recent, 5, 1000)
	require.Len(t, turns, 1)
	require.Equal(t, "question", turns[0][0].Message)
	require.Equal(t, "reply", turns[0][1].Message)
}
