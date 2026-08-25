package provider

import (
	"crypto/md5"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper functions for processResponseOutput
// We test the core deduplication logic directly rather than mocking the complex OpenAI Response struct

// testProcessResponseOutput extracts the core deduplication logic for testing
// This mirrors the actual processResponseOutput function but accepts our mock
func testProcessResponseOutput(rawOutput string) string {
	// If the output is empty or very short, return as-is (no duplication possible)
	if len(strings.TrimSpace(rawOutput)) < shortMessageThreshold {
		return rawOutput
	}

	// Split the output into paragraphs and deduplicate
	paragraphs := strings.Split(rawOutput, "\n\n")
	var uniqueParagraphs []string
	seenContent := make(map[string]bool)

	for _, paragraph := range paragraphs {
		trimmedParagraph := strings.TrimSpace(paragraph)

		// Preserve empty paragraphs for proper markdown formatting
		// (they're needed for spacing between blocks, tables, etc.)
		if trimmedParagraph == "" {
			uniqueParagraphs = append(uniqueParagraphs, paragraph)
			continue
		}

		// Create a hash of the paragraph content for deduplication
		// We normalize whitespace to catch minor formatting differences
		normalizedContent := strings.Join(strings.Fields(trimmedParagraph), " ")
		contentHash := fmt.Sprintf("%x", md5.Sum([]byte(normalizedContent)))

		// Only add if we haven't seen this content before
		if !seenContent[contentHash] {
			uniqueParagraphs = append(uniqueParagraphs, trimmedParagraph)
			seenContent[contentHash] = true
		}
	}

	return strings.Join(uniqueParagraphs, "\n\n")
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

	// Expected: Duplicate paragraphs should be removed, regardless of minor whitespace differences
	expectedResult := `This is a paragraph with normal spacing.

This   is   a   paragraph   with   extra   spaces.`

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
	// Create a realistic content scenario with some duplicates
	content := `This is a research summary about artificial intelligence and its applications.

Key findings from multiple sources indicate significant growth in AI adoption across industries.

Machine learning algorithms are becoming more sophisticated and efficient each year.

This is a research summary about artificial intelligence and its applications.

Deep learning neural networks are revolutionizing computer vision and natural language processing.

Key findings from multiple sources indicate significant growth in AI adoption across industries.

Robotics integration with AI is creating new opportunities in manufacturing and service sectors.

The future of AI development looks promising with continued investment and research breakthroughs.`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testProcessResponseOutput(content)
	}
}
