package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// marshalToolResult marshals a tool result to JSON, returning an error if marshaling fails.
// This helper reduces boilerplate in tool implementations.
func marshalToolResult(result interface{}, toolName string) (string, error) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal %s response: %w", toolName, err)
	}
	return string(resultJSON), nil
}

// unmarshalToolArgs unmarshals JSON arguments into the provided destination.
// Returns a marshaled error response if unmarshaling fails, or empty string on success.
// The errorResult parameter should be a struct that can be marshaled to JSON for error responses.
func unmarshalToolArgs(args []byte, dest interface{}, errorResult interface{}, toolName string) (errorResponse string, err error) {
	if err := json.Unmarshal(args, dest); err != nil {
		// Try to marshal the error result
		resultJSON, marshalErr := json.Marshal(errorResult)
		if marshalErr != nil {
			// If we can't even marshal the error, return a hard error
			return "", fmt.Errorf("failed to marshal %s error response: %w", toolName, marshalErr)
		}
		// Return the marshaled error response
		return string(resultJSON), nil
	}
	// Success - no error response needed
	return "", nil
}

// validateNonEmptyString validates that a string is not empty after trimming whitespace.
// Returns the trimmed string and a boolean indicating validity.
func validateNonEmptyString(input string) (trimmed string, valid bool) {
	trimmed = strings.TrimSpace(input)
	return trimmed, trimmed != ""
}

// createEmbeddingErrorResult creates a standardized error result for embedding failures.
// This helper provides consistent error messaging for tools that use embeddings.
type embeddingError struct {
	content string
	err     error
}

// formatEmbeddingError formats an embedding error with content preview for error messages
func formatEmbeddingError(content string, err error) string {
	return fmt.Sprintf("failed to create embedding: %v", err)
}

// truncateForLog truncates sensitive content for logging purposes
// This is a shared utility that multiple tools can use
func truncateForLog(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "..."
}
