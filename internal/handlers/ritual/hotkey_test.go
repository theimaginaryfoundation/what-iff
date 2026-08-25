package ritual

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateHotkey(t *testing.T) {
	tests := []struct {
		name    string
		hotkey  string
		wantErr bool
	}{
		// Empty / clearing binding
		{"empty string valid", "", false},
		{"whitespace only valid", "   ", false},

		// Valid: primary modifier + non-modifier
		{"Ctrl+A valid", "Ctrl+A", false},
		{"ctrl+a lowercase valid", "ctrl+a", false},
		{"Control+B valid", "Control+B", false},
		{"Alt+F4 valid", "Alt+F4", false},
		{"Meta+K valid", "Meta+K", false},
		{"Cmd+S valid", "Cmd+S", false},
		{"Command+Enter valid", "Command+Enter", false},

		// Valid: primary + shift + non-modifier
		{"Ctrl+Shift+A valid", "Ctrl+Shift+A", false},
		{"Alt+Shift+Tab valid", "Alt+Shift+Tab", false},

		// Valid: multiple non-modifier keys (sequence)
		{"Ctrl+K then Ctrl+C valid", "Ctrl+K+C", false},

		// Invalid: Shift alone (no primary modifier)
		{"Shift+A invalid - no primary modifier", "Shift+A", true},
		{"Shift+Enter invalid", "Shift+Enter", true},

		// Invalid: single part
		{"single part A invalid", "A", true},
		{"single part Ctrl invalid", "Ctrl", true},

		// Invalid: no non-modifier key
		{"Ctrl+Shift only invalid", "Ctrl+Shift", true},
		{"ctrl+alt invalid", "ctrl+alt", true},

		// Invalid: empty part
		{"empty part invalid", "Ctrl+ ", true},
		{"empty part middle invalid", "Ctrl++A", true},

		// Invalid: too long (limit MaxHotkeyLength chars)
		{"too long invalid", "Ctrl+" + strings.Repeat("a", MaxHotkeyLength-4), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHotkey(tt.hotkey)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHotkey(%q) error = %v, wantErr %v", tt.hotkey, err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidHotkey) {
				t.Errorf("ValidateHotkey(%q) error = %v, want ErrInvalidHotkey", tt.hotkey, err)
			}
		})
	}
}
