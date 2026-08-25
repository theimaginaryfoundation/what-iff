package chat

import (
	"testing"
)

// TestSanitizeFilename tests the filename sanitization function
func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal filename",
			input:    "My Chat",
			expected: "My Chat",
		},
		{
			name:     "filename with slashes",
			input:    "My/Chat\\Name",
			expected: "My-Chat-Name",
		},
		{
			name:     "filename with special characters",
			input:    "Chat:Name*With?Special<Chars>",
			expected: "Chat-Name-With-Special-Chars",
		},
		{
			name:     "filename with pipes and quotes",
			input:    `Chat|Name"With"Quotes`,
			expected: "Chat-Name-With-Quotes",
		},
		{
			name:     "filename with whitespace",
			input:    "  Chat   With   Spaces  ",
			expected: "Chat With Spaces",
		},
		{
			name:     "filename with newlines and tabs",
			input:    "Chat\nWith\rLine\tBreaks",
			expected: "Chat With Line Breaks",
		},
		{
			name:     "empty filename",
			input:    "",
			expected: "chat-export",
		},
		{
			name:     "filename with only special chars",
			input:    "///***???",
			expected: "chat-export",
		},
		{
			name:     "filename with only whitespace",
			input:    "   \t\n\r   ",
			expected: "chat-export",
		},
		{
			name:     "very long filename",
			input:    "This is an extremely long chat name that goes on and on and on and should be truncated to avoid filesystem limits and other issues that might occur when dealing with very long filenames in various operating systems that have different length limitations",
			expected: "", // We'll check length separately
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeFilename(tt.input)
			// For very long filenames, we just check that it's truncated and not empty
			if tt.expected == "" && tt.name == "very long filename" {
				if len(result) > 200 {
					t.Errorf("sanitizeFilename returned a filename longer than 200 characters: got %d", len(result))
				}
				if result == "" {
					t.Error("sanitizeFilename returned an empty string for long input")
				}
			} else if result != tt.expected {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestSanitizeFilename_Length verifies that sanitized filenames don't exceed 200 characters
func TestSanitizeFilename_Length(t *testing.T) {
	// Create a very long input (500 characters of repeating a-z)
	longInputBytes := make([]byte, 500)
	for i := range longInputBytes {
		longInputBytes[i] = byte('a' + (i % 26))
	}

	result := sanitizeFilename(string(longInputBytes))

	if len(result) > 200 {
		t.Errorf("sanitizeFilename returned a filename longer than 200 characters: got %d", len(result))
	}
}

// TestSanitizeFilename_NoUnsafeChars verifies that result contains no unsafe filesystem characters
func TestSanitizeFilename_NoUnsafeChars(t *testing.T) {
	unsafeInputs := []string{
		"Chat/Name",
		"Chat\\Name",
		"Chat:Name",
		"Chat*Name",
		"Chat?Name",
		"Chat\"Name",
		"Chat<Name",
		"Chat>Name",
		"Chat|Name",
	}

	unsafeChars := []rune{'/', '\\', ':', '*', '?', '"', '<', '>', '|'}

	for _, input := range unsafeInputs {
		result := sanitizeFilename(input)

		for _, unsafeChar := range unsafeChars {
			for _, resultChar := range result {
				if resultChar == unsafeChar {
					t.Errorf("sanitizeFilename(%q) = %q contains unsafe character %q", input, result, unsafeChar)
				}
			}
		}
	}
}
