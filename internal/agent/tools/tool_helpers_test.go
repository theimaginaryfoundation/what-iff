package tools

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for marshaling tests
type testResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

type testArgs struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestMarshalToolResult(t *testing.T) {
	tests := []struct {
		name        string
		result      interface{}
		toolName    string
		expectError bool
		validate    func(t *testing.T, output string)
	}{
		{
			name: "successful marshaling of simple struct",
			result: testResult{
				Success: true,
				Message: "operation completed",
			},
			toolName:    "test_tool",
			expectError: false,
			validate: func(t *testing.T, output string) {
				var parsed testResult
				err := json.Unmarshal([]byte(output), &parsed)
				require.NoError(t, err)
				assert.True(t, parsed.Success)
				assert.Equal(t, "operation completed", parsed.Message)
			},
		},
		{
			name: "marshaling with error field",
			result: testResult{
				Success: false,
				Error:   "something went wrong",
			},
			toolName:    "test_tool",
			expectError: false,
			validate: func(t *testing.T, output string) {
				var parsed testResult
				err := json.Unmarshal([]byte(output), &parsed)
				require.NoError(t, err)
				assert.False(t, parsed.Success)
				assert.Equal(t, "something went wrong", parsed.Error)
			},
		},
		{
			name: "marshaling map",
			result: map[string]interface{}{
				"key1": "value1",
				"key2": 123,
			},
			toolName:    "map_tool",
			expectError: false,
			validate: func(t *testing.T, output string) {
				var parsed map[string]interface{}
				err := json.Unmarshal([]byte(output), &parsed)
				require.NoError(t, err)
				assert.Equal(t, "value1", parsed["key1"])
				assert.Equal(t, float64(123), parsed["key2"]) // JSON numbers are float64
			},
		},
		{
			name:        "marshaling empty struct",
			result:      testResult{},
			toolName:    "empty_tool",
			expectError: false,
			validate: func(t *testing.T, output string) {
				assert.Contains(t, output, `"success":false`)
			},
		},
		{
			name: "marshaling with special characters",
			result: testResult{
				Message: `Test "quotes" and 'apostrophes' and \backslashes\`,
			},
			toolName:    "special_tool",
			expectError: false,
			validate: func(t *testing.T, output string) {
				var parsed testResult
				err := json.Unmarshal([]byte(output), &parsed)
				require.NoError(t, err)
				assert.Equal(t, `Test "quotes" and 'apostrophes' and \backslashes\`, parsed.Message)
			},
		},
		{
			name: "marshaling with unicode",
			result: testResult{
				Message: "Hello 世界 🌍",
			},
			toolName:    "unicode_tool",
			expectError: false,
			validate: func(t *testing.T, output string) {
				var parsed testResult
				err := json.Unmarshal([]byte(output), &parsed)
				require.NoError(t, err)
				assert.Equal(t, "Hello 世界 🌍", parsed.Message)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := marshalToolResult(tt.result, tt.toolName)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.toolName)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, output)
				if tt.validate != nil {
					tt.validate(t, output)
				}
			}
		})
	}
}

func TestUnmarshalToolArgs(t *testing.T) {
	tests := []struct {
		name           string
		args           []byte
		expectSuccess  bool
		validateResult func(t *testing.T, result testArgs)
		validateError  func(t *testing.T, errorResponse string, err error)
	}{
		{
			name:          "valid JSON args",
			args:          []byte(`{"name":"test","value":42}`),
			expectSuccess: true,
			validateResult: func(t *testing.T, result testArgs) {
				assert.Equal(t, "test", result.Name)
				assert.Equal(t, 42, result.Value)
			},
		},
		{
			name:          "valid JSON with extra fields",
			args:          []byte(`{"name":"test","value":42,"extra":"ignored"}`),
			expectSuccess: true,
			validateResult: func(t *testing.T, result testArgs) {
				assert.Equal(t, "test", result.Name)
				assert.Equal(t, 42, result.Value)
			},
		},
		{
			name:          "empty JSON object",
			args:          []byte(`{}`),
			expectSuccess: true,
			validateResult: func(t *testing.T, result testArgs) {
				assert.Equal(t, "", result.Name)
				assert.Equal(t, 0, result.Value)
			},
		},
		{
			name:          "invalid JSON",
			args:          []byte(`{"name":"test",invalid}`),
			expectSuccess: false,
			validateError: func(t *testing.T, errorResponse string, err error) {
				assert.NotEmpty(t, errorResponse)
				assert.NoError(t, err) // Should return error response, not Go error
				assert.Contains(t, errorResponse, "invalid arguments")
			},
		},
		{
			name:          "malformed JSON",
			args:          []byte(`not json at all`),
			expectSuccess: false,
			validateError: func(t *testing.T, errorResponse string, err error) {
				assert.NotEmpty(t, errorResponse)
				assert.NoError(t, err)
			},
		},
		{
			name:          "empty byte slice",
			args:          []byte{},
			expectSuccess: false,
			validateError: func(t *testing.T, errorResponse string, err error) {
				assert.NotEmpty(t, errorResponse)
				assert.NoError(t, err)
			},
		},
		{
			name:          "null JSON",
			args:          []byte(`null`),
			expectSuccess: true, // null unmarshals to zero value
			validateResult: func(t *testing.T, result testArgs) {
				assert.Equal(t, "", result.Name)
				assert.Equal(t, 0, result.Value)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dest testArgs
			errorResult := testResult{
				Success: false,
				Error:   "invalid arguments: parsing error",
			}

			errorResponse, err := unmarshalToolArgs(tt.args, &dest, errorResult, "test_tool")

			if tt.expectSuccess {
				assert.Empty(t, errorResponse)
				assert.NoError(t, err)
				if tt.validateResult != nil {
					tt.validateResult(t, dest)
				}
			} else {
				if tt.validateError != nil {
					tt.validateError(t, errorResponse, err)
				}
			}
		})
	}
}

func TestValidateNonEmptyString(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedValid bool
		expectedTrim  string
	}{
		{
			name:          "non-empty string",
			input:         "hello world",
			expectedValid: true,
			expectedTrim:  "hello world",
		},
		{
			name:          "string with leading whitespace",
			input:         "  hello",
			expectedValid: true,
			expectedTrim:  "hello",
		},
		{
			name:          "string with trailing whitespace",
			input:         "hello  ",
			expectedValid: true,
			expectedTrim:  "hello",
		},
		{
			name:          "string with leading and trailing whitespace",
			input:         "  hello  ",
			expectedValid: true,
			expectedTrim:  "hello",
		},
		{
			name:          "string with tabs and spaces",
			input:         "\t  hello world  \t",
			expectedValid: true,
			expectedTrim:  "hello world",
		},
		{
			name:          "string with newlines",
			input:         "\nhello\n",
			expectedValid: true,
			expectedTrim:  "hello",
		},
		{
			name:          "empty string",
			input:         "",
			expectedValid: false,
			expectedTrim:  "",
		},
		{
			name:          "only spaces",
			input:         "   ",
			expectedValid: false,
			expectedTrim:  "",
		},
		{
			name:          "only tabs",
			input:         "\t\t\t",
			expectedValid: false,
			expectedTrim:  "",
		},
		{
			name:          "only newlines",
			input:         "\n\n\n",
			expectedValid: false,
			expectedTrim:  "",
		},
		{
			name:          "mixed whitespace only",
			input:         " \t\n\r ",
			expectedValid: false,
			expectedTrim:  "",
		},
		{
			name:          "unicode spaces",
			input:         "\u00A0\u00A0",
			expectedValid: false, // Non-breaking spaces are trimmed by TrimSpace in Go 1.18+
			expectedTrim:  "",
		},
		{
			name:          "string with internal whitespace preserved",
			input:         "  hello   world  ",
			expectedValid: true,
			expectedTrim:  "hello   world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trimmed, valid := validateNonEmptyString(tt.input)
			assert.Equal(t, tt.expectedValid, valid, "validity mismatch")
			assert.Equal(t, tt.expectedTrim, trimmed, "trimmed value mismatch")
		})
	}
}

func TestFormatEmbeddingError(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		err      error
		expected string
	}{
		{
			name:     "simple error",
			content:  "test content",
			err:      errors.New("connection failed"),
			expected: "failed to create embedding: connection failed",
		},
		{
			name:     "wrapped error",
			content:  "some text",
			err:      errors.New("API error: rate limit exceeded"),
			expected: "failed to create embedding: API error: rate limit exceeded",
		},
		{
			name:     "empty content",
			content:  "",
			err:      errors.New("empty input"),
			expected: "failed to create embedding: empty input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatEmbeddingError(tt.content, tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTruncateForLog(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLen   int
		expected string
	}{
		{
			name:     "content shorter than max",
			content:  "short",
			maxLen:   10,
			expected: "short",
		},
		{
			name:     "content equal to max",
			content:  "exactly10!",
			maxLen:   10,
			expected: "exactly10!",
		},
		{
			name:     "content longer than max",
			content:  "this is a very long content that should be truncated",
			maxLen:   20,
			expected: "this is a very long ...",
		},
		{
			name:     "empty content",
			content:  "",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "max length zero",
			content:  "content",
			maxLen:   0,
			expected: "...",
		},
		{
			name:     "max length one",
			content:  "content",
			maxLen:   1,
			expected: "c...",
		},
		{
			name:    "unicode content truncation",
			content: "Hello 世界, this is a test",
			maxLen:  10,
			// Note: truncation may split multi-byte UTF-8 characters
			// This is acceptable for logging purposes
			expected: "", // Will validate length instead
		},
		{
			name:     "sensitive data preview",
			content:  "password123456789secret",
			maxLen:   8,
			expected: "password...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateForLog(tt.content, tt.maxLen)
			if tt.expected != "" {
				assert.Equal(t, tt.expected, result)
			}
			if len(tt.content) > tt.maxLen {
				assert.LessOrEqual(t, len(result), tt.maxLen+3) // +3 for "..."
				assert.Contains(t, result, "...")               // Should have ellipsis
			}
		})
	}
}

// Benchmark tests for performance validation
func BenchmarkMarshalToolResult(b *testing.B) {
	result := testResult{
		Success: true,
		Message: "test message with some content",
		Error:   "",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = marshalToolResult(result, "test_tool")
	}
}

func BenchmarkValidateNonEmptyString(b *testing.B) {
	testString := "  test string with whitespace  "

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = validateNonEmptyString(testString)
	}
}

func BenchmarkTruncateForLog(b *testing.B) {
	longString := "This is a very long string that needs to be truncated for logging purposes to avoid excessive log output"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = truncateForLog(longString, 50)
	}
}
