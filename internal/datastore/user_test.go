package datastore

import "testing"

func TestValidateIANATimezone(t *testing.T) {
	tests := []struct {
		name    string
		tz      string
		wantErr bool
	}{
		{"empty is valid (no-op)", "", false},
		{"whitespace only is valid", "  ", false},
		{"America/New_York", "America/New_York", false},
		{"UTC", "UTC", false},
		{"Europe/London", "Europe/London", false},
		{"with trim", "  America/Los_Angeles  ", false},
		{"invalid name", "Not/A_Real_Timezone", true},
		{"garbage", "xyz", true},
		{"empty after trim", "  ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIANATimezone(tt.tz)
			if tt.wantErr {
				if err != ErrInvalidTimezone {
					t.Fatalf("validateIANATimezone() err = %v, want ErrInvalidTimezone", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateIANATimezone() err = %v, want nil", err)
			}
		})
	}
}
