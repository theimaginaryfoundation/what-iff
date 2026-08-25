package server

import (
	"os"
	"testing"
	"time"
)

func TestNewConfig_AllowedEmails(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		expected    []string
		expectEmpty bool
	}{
		{
			name:        "empty env var",
			envValue:    "",
			expected:    []string{},
			expectEmpty: true,
		},
		{
			name:        "single email",
			envValue:    "test@example.com",
			expected:    []string{"test@example.com"},
			expectEmpty: false,
		},
		{
			name:        "multiple emails",
			envValue:    "test@example.com,user@test.com,admin@site.org",
			expected:    []string{"test@example.com", "user@test.com", "admin@site.org"},
			expectEmpty: false,
		},
		{
			name:        "emails with spaces",
			envValue:    "test@example.com, user@test.com , admin@site.org",
			expected:    []string{"test@example.com", "user@test.com", "admin@site.org"},
			expectEmpty: false,
		},
		{
			name:        "emails with mixed case - normalized to lowercase",
			envValue:    "Test@Example.COM,USER@test.com",
			expected:    []string{"test@example.com", "user@test.com"},
			expectEmpty: false,
		},
		{
			name:        "emails with extra commas",
			envValue:    "test@example.com,,user@test.com,",
			expected:    []string{"test@example.com", "user@test.com"},
			expectEmpty: false,
		},
		{
			name:        "emails with leading/trailing spaces",
			envValue:    "  test@example.com  ,  user@test.com  ",
			expected:    []string{"test@example.com", "user@test.com"},
			expectEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env var
			if tt.envValue != "" {
				os.Setenv("ALLOWED_EMAILS", tt.envValue)
			} else {
				os.Unsetenv("ALLOWED_EMAILS")
			}
			defer os.Unsetenv("ALLOWED_EMAILS")

			// Create config
			config := NewConfig()

			// Check empty vs non-empty
			if tt.expectEmpty {
				if len(config.AllowedEmails) != 0 {
					t.Errorf("expected empty AllowedEmails, got %v", config.AllowedEmails)
				}
				return
			}

			// Check length
			if len(config.AllowedEmails) != len(tt.expected) {
				t.Errorf("expected %d emails, got %d: %v", len(tt.expected), len(config.AllowedEmails), config.AllowedEmails)
				return
			}

			// Check each email
			for i, expectedEmail := range tt.expected {
				if config.AllowedEmails[i] != expectedEmail {
					t.Errorf("email[%d]: expected %q, got %q", i, expectedEmail, config.AllowedEmails[i])
				}
			}
		})
	}
}

func TestNewConfig_Environment(t *testing.T) {
	t.Run("uses ENV when set", func(t *testing.T) {
		t.Setenv("ENV", "Prod")

		config := NewConfig()
		if config.Environment != "prod" {
			t.Fatalf("expected environment %q, got %q", "prod", config.Environment)
		}
	})

	t.Run("ignores legacy ENVIRONMENT variable", func(t *testing.T) {
		t.Setenv("ENV", "")
		t.Setenv("ENVIRONMENT", "Production")

		config := NewConfig()
		if config.Environment != "development" {
			t.Fatalf("expected environment %q, got %q", "development", config.Environment)
		}
	})

	t.Run("defaults to development", func(t *testing.T) {
		t.Setenv("ENV", "")

		config := NewConfig()
		if config.Environment != "development" {
			t.Fatalf("expected environment %q, got %q", "development", config.Environment)
		}
	})
}

func TestNewConfig_ExplicitEnvGating(t *testing.T) {
	tests := []struct {
		name              string
		env               map[string]string
		wantExplicit      bool
		wantConflict      bool
		wantExplicitLocal bool
		wantLLMBackend    string
	}{
		{
			name:              "unset ENV defaults to development but is not explicit",
			env:               map[string]string{"ENV": "", "ENVIRONMENT": ""},
			wantExplicit:      false,
			wantExplicitLocal: false,
			wantLLMBackend:    "vendor",
		},
		{
			name:              "explicit development is local",
			env:               map[string]string{"ENV": "development", "ENVIRONMENT": ""},
			wantExplicit:      true,
			wantExplicitLocal: true,
			wantLLMBackend:    "vendor",
		},
		{
			name:              "explicit test is local",
			env:               map[string]string{"ENV": "test", "ENVIRONMENT": ""},
			wantExplicit:      true,
			wantExplicitLocal: true,
			wantLLMBackend:    "vendor",
		},
		{
			name:              "explicit local is local",
			env:               map[string]string{"ENV": "local", "ENVIRONMENT": ""},
			wantExplicit:      true,
			wantExplicitLocal: true,
			wantLLMBackend:    "vendor",
		},
		{
			name:              "explicit production is not local",
			env:               map[string]string{"ENV": "production", "ENVIRONMENT": ""},
			wantExplicit:      true,
			wantExplicitLocal: false,
			wantLLMBackend:    "vendor",
		},
		{
			name:              "unknown explicit value is treated as non-local",
			env:               map[string]string{"ENV": "staging-oops", "ENVIRONMENT": ""},
			wantExplicit:      true,
			wantExplicitLocal: false,
			wantLLMBackend:    "vendor",
		},
		{
			name:              "legacy ENVIRONMENT alias counts as explicit",
			env:               map[string]string{"ENV": "", "ENVIRONMENT": "test"},
			wantExplicit:      true,
			wantExplicitLocal: true,
			wantLLMBackend:    "vendor",
		},
		{
			name:              "conflicting ENV and ENVIRONMENT flags conflict and disables local",
			env:               map[string]string{"ENV": "production", "ENVIRONMENT": "development"},
			wantExplicit:      true,
			wantConflict:      true,
			wantExplicitLocal: false,
			wantLLMBackend:    "vendor",
		},
		{
			name:              "agreeing ENV and ENVIRONMENT are not a conflict",
			env:               map[string]string{"ENV": "test", "ENVIRONMENT": "test"},
			wantExplicit:      true,
			wantConflict:      false,
			wantExplicitLocal: true,
			wantLLMBackend:    "vendor",
		},
		{
			name:              "LLM_BACKEND parsed independently of gating",
			env:               map[string]string{"ENV": "test", "ENVIRONMENT": "", "LLM_BACKEND": "mock"},
			wantExplicit:      true,
			wantExplicitLocal: true,
			wantLLMBackend:    "mock",
		},
		{
			name:              "unset LLM_BACKEND defaults to vendor",
			env:               map[string]string{"ENV": "test", "ENVIRONMENT": ""},
			wantExplicit:      true,
			wantExplicitLocal: true,
			wantLLMBackend:    "vendor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LLM_BACKEND", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			cfg := NewConfig()
			if cfg.EnvironmentExplicit != tt.wantExplicit {
				t.Errorf("EnvironmentExplicit = %v, want %v", cfg.EnvironmentExplicit, tt.wantExplicit)
			}
			if cfg.EnvironmentConflict != tt.wantConflict {
				t.Errorf("EnvironmentConflict = %v, want %v", cfg.EnvironmentConflict, tt.wantConflict)
			}
			if cfg.IsExplicitLocalEnv() != tt.wantExplicitLocal {
				t.Errorf("IsExplicitLocalEnv() = %v, want %v", cfg.IsExplicitLocalEnv(), tt.wantExplicitLocal)
			}
			if cfg.LLMBackend != tt.wantLLMBackend {
				t.Errorf("LLMBackend = %v, want %v", cfg.LLMBackend, tt.wantLLMBackend)
			}
		})
	}
}

func TestNewConfig_MockLLMOptions(t *testing.T) {
	t.Setenv("ENV", "test")
	t.Setenv("LLM_BACKEND", "mock")
	t.Setenv("MOCK_LLM_MODE", "Fixed")
	t.Setenv("MOCK_LLM_FIXED_RESPONSES", "first reply| second reply |")
	t.Setenv("MOCK_LLM_STREAM_DELAY_MS", "25")

	cfg := NewConfig()
	if cfg.MockLLMMode != "fixed" {
		t.Errorf("MockLLMMode = %q, want %q", cfg.MockLLMMode, "fixed")
	}
	if len(cfg.MockLLMFixedResponses) != 2 || cfg.MockLLMFixedResponses[0] != "first reply" || cfg.MockLLMFixedResponses[1] != "second reply" {
		t.Errorf("MockLLMFixedResponses = %v", cfg.MockLLMFixedResponses)
	}
	if cfg.MockLLMStreamDelay != 25*time.Millisecond {
		t.Errorf("MockLLMStreamDelay = %v", cfg.MockLLMStreamDelay)
	}
}
