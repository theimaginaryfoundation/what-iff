package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripOpenAIFileLinks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single sentence with sandbox link",
			input:    "You can also [download the current merged CSV here](sandbox:/mnt/data/merged_sorted.csv).",
			expected: "",
		},
		{
			name:     "multiple sentences with one sandbox link",
			input:    "Here is your analysis. You can also [download the current merged CSV here](sandbox:/mnt/data/merged_sorted.csv). The data shows interesting patterns.",
			expected: "Here is your analysis. The data shows interesting patterns.",
		},
		{
			name:     "multiple sentences with multiple sandbox links",
			input:    "Here is your analysis. You can [download the CSV here](sandbox:/mnt/data/data.csv). Also check out [this file](sandbox:/mnt/data/results.txt). The data shows patterns.",
			expected: "Here is your analysis. The data shows patterns.",
		},
		{
			name:     "sentence with sandbox link at the beginning",
			input:    "[Download the file](sandbox:/mnt/data/test.csv) to see the results. Here is the analysis.",
			expected: "Here is the analysis.",
		},
		{
			name:     "sentence with sandbox link at the end",
			input:    "Here is the analysis. Please [download the results](sandbox:/mnt/data/output.csv).",
			expected: "Here is the analysis.",
		},
		{
			name:     "sentence with sandbox link in the middle",
			input:    "Here is the analysis. You can [download the file](sandbox:/mnt/data/data.csv) to verify the results.",
			expected: "Here is the analysis.",
		},
		{
			name:     "no sandbox links - should remain unchanged",
			input:    "Here is your analysis. The data shows interesting patterns. Please review the results.",
			expected: "Here is your analysis. The data shows interesting patterns. Please review the results.",
		},
		{
			name:     "regular markdown links should not be removed",
			input:    "Here is your analysis. You can [download the file](https://example.com/file.csv) to see the results.",
			expected: "Here is your analysis. You can [download the file](https://example.com/file.csv) to see the results.",
		},
		{
			name:     "different file extensions",
			input:    "Here is the data. [Download CSV](sandbox:/mnt/data/data.csv). Also check [this text file](sandbox:/mnt/data/notes.txt).",
			expected: "Here is the data.",
		},
		{
			name:     "nested paths in sandbox links",
			input:    "Here is the analysis. [Download the processed file](sandbox:/mnt/data/processed/results/final.csv).",
			expected: "Here is the analysis.",
		},
		{
			name:     "sandbox link with special characters in filename",
			input:    "Here is the data. [Download file](sandbox:/mnt/data/data-2024_01_15 (final).csv).",
			expected: "Here is the data.",
		},
		{
			name:     "multiple punctuation marks",
			input:    "Here is your analysis!!! You can also [download the file](sandbox:/mnt/data/data.csv)??? The data shows patterns...",
			expected: "Here is your analysis!!! The data shows patterns...",
		},
		{
			name:     "question and exclamation marks",
			input:    "What do you think? You can [download the results](sandbox:/mnt/data/results.csv)! Here are the findings.",
			expected: "What do you think? Here are the findings.",
		},
		{
			name:     "only sandbox links - should return empty",
			input:    "[Download CSV](sandbox:/mnt/data/data.csv). [Download results](sandbox:/mnt/data/results.txt).",
			expected: "",
		},
		{
			name:     "whitespace handling",
			input:    "  Here is your analysis.   You can [download the file](sandbox:/mnt/data/data.csv).   The data shows patterns.  ",
			expected: "Here is your analysis. The data shows patterns.",
		},
		{
			name:     "newlines and tabs",
			input:    "Here is your analysis.\nYou can [download the file](sandbox:/mnt/data/data.csv).\tThe data shows patterns.",
			expected: "Here is your analysis.\nThe data shows patterns.",
		},
		{
			name:     "link text with special characters",
			input:    "Here is the analysis. You can [download the 'processed' data file](sandbox:/mnt/data/data.csv) to verify.",
			expected: "Here is the analysis.",
		},
		{
			name:     "case sensitivity - uppercase SANDBOX",
			input:    "Here is the analysis. [Download file](SANDBOX:/mnt/data/data.csv).",
			expected: "Here is the analysis. [Download file](SANDBOX:/mnt/data/data.csv).",
		},
		{
			name:     "partial sandbox path match",
			input:    "Here is the analysis. [Download file](sandbox:/other/path/data.csv).",
			expected: "Here is the analysis. [Download file](sandbox:/other/path/data.csv).",
		},
		{
			name:     "empty link text",
			input:    "Here is the analysis. [](sandbox:/mnt/data/data.csv). The data shows patterns.",
			expected: "Here is the analysis. The data shows patterns.",
		},
		{
			name:     "malformed markdown link",
			input:    "Here is the analysis. [Download file](sandbox:/mnt/data/data.csv without closing paren.",
			expected: "Here is the analysis. [Download file](sandbox:/mnt/data/data.csv without closing paren.",
		},
		{
			name:     "very long sentence with sandbox link",
			input:    "This is a very long sentence that contains a lot of text and explanations about the data analysis process and results, and you can [download the comprehensive report](sandbox:/mnt/data/comprehensive_report.pdf) to see all the details and findings from our extensive research and analysis work.",
			expected: "",
		},
		{
			name:     "sentence without ending punctuation",
			input:    "Here is your analysis You can [download the file](sandbox:/mnt/data/data.csv) The data shows patterns",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripOpenAIFileLinks(tt.input)
			assert.Equal(t, tt.expected, result, "Input: %q", tt.input)
		})
	}
}

func TestStripOpenAIFileLinks_EdgeCases(t *testing.T) {
	t.Run("nil input handling", func(t *testing.T) {
		// This test ensures the function handles nil-like inputs gracefully
		// Since we're using string, nil isn't possible, but empty string is
		result := StripOpenAIFileLinks("")
		assert.Equal(t, "", result)
	})

	t.Run("only whitespace", func(t *testing.T) {
		result := StripOpenAIFileLinks("   \n\t   ")
		assert.Equal(t, "   \n\t   ", result)
	})

	t.Run("only punctuation", func(t *testing.T) {
		result := StripOpenAIFileLinks("...!!!???")
		assert.Equal(t, "...!!!???", result)
	})

	t.Run("mixed punctuation and sandbox links", func(t *testing.T) {
		input := "...You can [download](sandbox:/mnt/data/file.csv)...!!!"
		result := StripOpenAIFileLinks(input)
		assert.Equal(t, "", result)
	})

	t.Run("regex special characters in sandbox path", func(t *testing.T) {
		input := "Here is data. [Download](sandbox:/mnt/data/file[1].csv). More text."
		result := StripOpenAIFileLinks(input)
		assert.Equal(t, "Here is data. More text.", result)
	})

	t.Run("multiple consecutive sentences with sandbox links", func(t *testing.T) {
		input := "First sentence. [Download A](sandbox:/mnt/data/a.csv). [Download B](sandbox:/mnt/data/b.csv). Final sentence."
		result := StripOpenAIFileLinks(input)
		assert.Equal(t, "First sentence. Final sentence.", result)
	})
}

func TestStripOpenAIFileLinks_Performance(t *testing.T) {
	// Test with a very long text to ensure reasonable performance
	longText := "Here is your analysis. " +
		"[Download file 1](sandbox:/mnt/data/file1.csv). " +
		"This is some regular text without any links. " +
		"[Download file 2](sandbox:/mnt/data/file2.csv). " +
		"More analysis text here. " +
		"Final sentence without links."

	// Repeat the pattern many times
	for i := 0; i < 100; i++ {
		longText += " Here is your analysis. [Download file](sandbox:/mnt/data/file.csv). More text."
	}

	// This should complete quickly and return only non-sandbox sentences
	result := StripOpenAIFileLinks(longText)
	require.NotEmpty(t, result)

	// Verify no sandbox links remain
	require.NotContains(t, result, "sandbox:/mnt/data/")
}

func TestStripOpenAIFileLinks_RegexPatterns(t *testing.T) {
	t.Run("various link text patterns", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected string
		}{
			{
				input:    "Text. [Download](sandbox:/mnt/data/file.csv). More.",
				expected: "Text. More.",
			},
			{
				input:    "Text. [download the file](sandbox:/mnt/data/file.csv). More.",
				expected: "Text. More.",
			},
			{
				input:    "Text. [Download the CSV file here](sandbox:/mnt/data/file.csv). More.",
				expected: "Text. More.",
			},
			{
				input:    "Text. [Click here to download](sandbox:/mnt/data/file.csv). More.",
				expected: "Text. More.",
			},
			{
				input:    "Text. [Download: file.csv](sandbox:/mnt/data/file.csv). More.",
				expected: "Text. More.",
			},
		}

		for _, tc := range testCases {
			result := StripOpenAIFileLinks(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})

	t.Run("various file path patterns", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected string
		}{
			{
				input:    "Text. [Download](sandbox:/mnt/data/file.csv). More.",
				expected: "Text. More.",
			},
			{
				input:    "Text. [Download](sandbox:/mnt/data/subfolder/file.csv). More.",
				expected: "Text. More.",
			},
			{
				input:    "Text. [Download](sandbox:/mnt/data/processed/2024/results.csv). More.",
				expected: "Text. More.",
			},
			{
				input:    "Text. [Download](sandbox:/mnt/data/file-with-dashes.csv). More.",
				expected: "Text. More.",
			},
			{
				input:    "Text. [Download](sandbox:/mnt/data/file_with_underscores.csv). More.",
				expected: "Text. More.",
			},
		}

		for _, tc := range testCases {
			result := StripOpenAIFileLinks(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})
}

func TestStripOpenAIFileLinks_MarkdownFormatting(t *testing.T) {
	t.Run("markdown lists and headings preserved with newlines", func(t *testing.T) {
		input := `# Heading 1

Here's a list:
- Item 1
- Item 2
- Item 3

## Heading 2

More content here.`

		// No sandbox links, so everything should be preserved exactly
		expected := input

		result := StripOpenAIFileLinks(input)
		assert.Equal(t, expected, result, "Markdown formatting without sandbox links should be preserved")
	})

	t.Run("markdown table preserved with newlines", func(t *testing.T) {
		input := `Here are the results:

| Name | Value |
|------|-------|
| A | 123 |
| B | 456 |

End of report.`

		// No sandbox links, so everything should be preserved exactly
		expected := input

		result := StripOpenAIFileLinks(input)
		assert.Equal(t, expected, result, "Markdown tables should be preserved with proper newlines")
	})

	t.Run("numbered lists with sandbox link sentence removed", func(t *testing.T) {
		input := `Steps to follow:

1. First step.
2. Second step.
3. Third step.

You can [download instructions](sandbox:/mnt/data/instructions.pdf).

That's all!`

		// The sentence with the sandbox link should be removed
		expected := `Steps to follow:

1. First step.
2. Second step.
3. Third step.

That's all!`

		result := StripOpenAIFileLinks(input)
		assert.Equal(t, expected, result, "Numbered lists should be preserved and sentences with sandbox links removed")
	})

	t.Run("complex markdown with multiple newlines", func(t *testing.T) {
		input := `# Analysis Report

## Summary

The data shows interesting patterns.

Here's a breakdown:

- **Point 1**: First observation
- **Point 2**: Second observation
- **Point 3**: Third observation

### Detailed Results

| Metric | Value |
|--------|-------|
| A | 100 |
| B | 200 |

This completes the analysis.`

		// No sandbox links, everything preserved (note: trailing spaces are normalized)
		expected := input

		result := StripOpenAIFileLinks(input)
		assert.Equal(t, expected, result, "Complex markdown with multiple newlines should be fully preserved")
	})
}

func TestNormalizeUploadFileNameExtension(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"photo.JPG":       "photo.jpg",
		"photo.jpg":       "photo.jpg",
		"doc.PNG":         "doc.png",
		"archive.tar.GZ":  "archive.tar.gz",
		"NoExtension":     "NoExtension",
		"UPPER.name.WEBP": "UPPER.name.webp",
	}
	for in, want := range cases {
		require.Equal(t, want, normalizeUploadFileNameExtension(in), "input %q", in)
	}
}
