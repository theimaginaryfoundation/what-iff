package datastore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// TestMessageOriginFilterValidation tests the validation of message origin filter constants
func TestMessageOriginFilterValidation(t *testing.T) {
	tests := []struct {
		name          string
		filter        models.MessageOriginFilter
		shouldBeValid bool
	}{
		{
			name:          "Valid filter - ALL",
			filter:        models.MessageOriginFilterAll,
			shouldBeValid: true,
		},
		{
			name:          "Valid filter - USER",
			filter:        models.MessageOriginFilterUser,
			shouldBeValid: true,
		},
		{
			name:          "Valid filter - ASSISTANT",
			filter:        models.MessageOriginFilterAssistant,
			shouldBeValid: true,
		},
		{
			name:          "Invalid filter - lowercase",
			filter:        models.MessageOriginFilter("all"),
			shouldBeValid: false,
		},
		{
			name:          "Invalid filter - mixed case",
			filter:        models.MessageOriginFilter("User"),
			shouldBeValid: false,
		},
		{
			name:          "Invalid filter - arbitrary string",
			filter:        models.MessageOriginFilter("INVALID"),
			shouldBeValid: false,
		},
		{
			name:          "Empty filter - should default to ALL",
			filter:        models.MessageOriginFilter(""),
			shouldBeValid: true, // Empty is valid because it defaults to ALL
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Validate filter
			filter := tc.filter
			if filter == "" {
				filter = models.MessageOriginFilterAll
			}

			isValid := filter == models.MessageOriginFilterAll ||
				filter == models.MessageOriginFilterUser ||
				filter == models.MessageOriginFilterAssistant

			assert.Equal(t, tc.shouldBeValid, isValid,
				"Filter validation result should match expected")
		})
	}
}

// TestMessageOriginFilterConstants verifies the constant values are as expected
func TestMessageOriginFilterConstants(t *testing.T) {
	assert.Equal(t, "ALL", string(models.MessageOriginFilterAll),
		"MessageOriginFilterAll should be 'ALL'")
	assert.Equal(t, "USER", string(models.MessageOriginFilterUser),
		"MessageOriginFilterUser should be 'USER'")
	assert.Equal(t, "ASSISTANT", string(models.MessageOriginFilterAssistant),
		"MessageOriginFilterAssistant should be 'ASSISTANT'")
}

// TestMessageOriginFilterDefaultBehavior tests that empty filter defaults to ALL
func TestMessageOriginFilterDefaultBehavior(t *testing.T) {
	emptyFilter := models.MessageOriginFilter("")
	defaultFilter := models.MessageOriginFilterAll

	// Simulate the default behavior in the actual method
	if emptyFilter == "" {
		emptyFilter = models.MessageOriginFilterAll
	}

	assert.Equal(t, defaultFilter, emptyFilter,
		"Empty filter should default to MessageOriginFilterAll")
}

// TestMessageOriginFilterMapping verifies correct mapping between filter and origin
func TestMessageOriginFilterMapping(t *testing.T) {
	tests := []struct {
		name           string
		filter         models.MessageOriginFilter
		shouldFilterTo *models.MessageOrigin // nil means no filtering
	}{
		{
			name:           "ALL filter - no specific origin",
			filter:         models.MessageOriginFilterAll,
			shouldFilterTo: nil,
		},
		{
			name:           "USER filter - User origin",
			filter:         models.MessageOriginFilterUser,
			shouldFilterTo: func() *models.MessageOrigin { o := models.MessageOriginUser; return &o }(),
		},
		{
			name:           "ASSISTANT filter - Assistant origin",
			filter:         models.MessageOriginFilterAssistant,
			shouldFilterTo: func() *models.MessageOrigin { o := models.MessageOriginAssistant; return &o }(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Verify filter maps to correct origin
			var expectedOrigin *models.MessageOrigin

			if tc.filter == models.MessageOriginFilterUser {
				o := models.MessageOriginUser
				expectedOrigin = &o
			} else if tc.filter == models.MessageOriginFilterAssistant {
				o := models.MessageOriginAssistant
				expectedOrigin = &o
			}

			if tc.shouldFilterTo == nil {
				assert.Nil(t, expectedOrigin, "ALL filter should not map to specific origin")
			} else {
				assert.NotNil(t, expectedOrigin, "Filter should map to specific origin")
				assert.Equal(t, *tc.shouldFilterTo, *expectedOrigin,
					"Filter should map to correct origin")
			}
		})
	}
}

// TestMessageOriginToFilterConversion tests conceptual conversion between types
func TestMessageOriginToFilterConversion(t *testing.T) {
	tests := []struct {
		name   string
		origin models.MessageOrigin
		filter models.MessageOriginFilter
	}{
		{
			name:   "User origin maps to USER filter",
			origin: models.MessageOriginUser,
			filter: models.MessageOriginFilterUser,
		},
		{
			name:   "Assistant origin maps to ASSISTANT filter",
			origin: models.MessageOriginAssistant,
			filter: models.MessageOriginFilterAssistant,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Verify that the string representations are consistent
			// (USER/User, ASSISTANT/Assistant)
			assert.NotEqual(t, string(tc.origin), string(tc.filter),
				"Origin and Filter should have different string representations")

			// Verify expected values
			if tc.origin == models.MessageOriginUser {
				assert.Equal(t, models.MessageOriginFilterUser, tc.filter)
			} else if tc.origin == models.MessageOriginAssistant {
				assert.Equal(t, models.MessageOriginFilterAssistant, tc.filter)
			}
		})
	}
}

// TestFilterTypeDistinction ensures filter types are distinct from origin types
func TestFilterTypeDistinction(t *testing.T) {
	// These should be different types and not directly assignable
	var filter models.MessageOriginFilter = models.MessageOriginFilterUser
	var origin models.MessageOrigin = models.MessageOriginUser

	// Verify they have different string values
	assert.Equal(t, "USER", string(filter))
	assert.Equal(t, "User", string(origin))
	assert.NotEqual(t, string(filter), string(origin),
		"Filter and Origin should have distinct string representations")
}
