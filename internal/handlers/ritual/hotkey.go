package ritual

import (
	"errors"
	"fmt"
	"strings"
)

const MaxHotkeyLength = 100

var (
	ErrInvalidHotkey = errors.New("invalid hotkey format")
)

// ValidateHotkey applies server-side validation for ritual hotkey bindings.
//
// Rules:
//   - Empty string is valid (used when clearing a binding).
//   - Must have at least two parts separated by "+" (e.g. "Ctrl+A").
//   - Must include at least one primary modifier: ctrl, control, alt, meta, cmd, or command.
//     Shift alone is not sufficient — "Shift+A" is invalid; "Ctrl+Shift+A" is valid.
//   - Must include at least one non-modifier key (the actual key, e.g. A, 1, Enter).
//   - Maximum length MaxHotkeyLength characters.
func ValidateHotkey(hotkey string) error {
	hotkey = strings.TrimSpace(hotkey)
	if hotkey == "" {
		return nil
	}

	parts := strings.Split(hotkey, "+")
	if len(parts) < 2 {
		return fmt.Errorf("%w: must have at least two parts separated by + (e.g. Ctrl+A)", ErrInvalidHotkey)
	}

	hasPrimaryModifier := false
	nonModifierCount := 0
	for _, p := range parts {
		k := strings.TrimSpace(strings.ToLower(p))
		if k == "" {
			return fmt.Errorf("%w: part cannot be empty", ErrInvalidHotkey)
		}
		switch k {
		case "ctrl", "control", "alt", "meta", "cmd", "command":
			hasPrimaryModifier = true
		case "shift":
			// Shift is a modifier but does not count as primary.
			// Shift+A is invalid; Ctrl+Shift+A is valid.
		default:
			nonModifierCount++
		}
	}

	if !hasPrimaryModifier {
		return fmt.Errorf("%w: must include a primary modifier (Ctrl, Alt, Meta, Cmd, or Command); Shift alone is not sufficient", ErrInvalidHotkey)
	}
	if nonModifierCount == 0 {
		return fmt.Errorf("%w: must include a non-modifier key (e.g. A, 1, Enter)", ErrInvalidHotkey)
	}
	if len(hotkey) > MaxHotkeyLength {
		return fmt.Errorf("%w: exceeds maximum length of %d characters", ErrInvalidHotkey, MaxHotkeyLength)
	}
	return nil
}
