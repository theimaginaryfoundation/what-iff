package auth

import "testing"

func TestValidateSecret(t *testing.T) {
	tests := []struct {
		name    string
		secret  string
		wantErr bool
	}{
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"too short", "short-secret", true},
		{"known placeholder devsecret", "devsecret", true},
		{"known placeholder devrefresh", "devrefresh", true},
		{"known placeholder changeme", "changeme", true},
		{"known placeholder is case-insensitive", "DevSecret", true},
		{"env.example JWT placeholder", "your_super_secret_key_change_this_in_production", true},
		{"env.example token placeholder", "change_this_secret_to_a_long_random_value", true},
		{"valid long random secret", "kx8f2m9qz3vw7n4jr6bt1yc5hd0lp8se", false},
		{"exactly the minimum length", "0123456789012345678901234567890a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSecret("TEST_SECRET", tt.secret)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSecret(%q) error = %v, wantErr %v", tt.secret, err, tt.wantErr)
			}
		})
	}
}
