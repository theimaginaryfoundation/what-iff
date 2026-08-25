package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type SafetyViolation struct {
	Provider        models.SafetyViolationProvider
	ViolationType   string
	ProviderCode    string
	ProviderMessage string
	RawError        string
}

type SafetyViolationError struct {
	Violation *SafetyViolation
}

func (e *SafetyViolationError) Error() string {
	if e == nil || e.Violation == nil {
		return "safety violation"
	}
	if msg := strings.TrimSpace(e.Violation.ProviderMessage); msg != "" {
		return msg
	}
	return "safety violation"
}

func IsSafetyViolationError(err error) (*SafetyViolation, bool) {
	var sve *SafetyViolationError
	if errors.As(err, &sve) && sve != nil && sve.Violation != nil {
		return sve.Violation, true
	}
	violation, ok := ParseSafetyViolationError(err)
	return violation, ok
}

// ParseSafetyViolationError inspects an error's text only. Use it where the
// provider genuinely is not known at the call site; prefer
// ParseSafetyViolationErrorFor, which does not have to guess.
func ParseSafetyViolationError(err error) (*SafetyViolation, bool) {
	return parseSafetyViolation(err, "")
}

// ParseSafetyViolationErrorFor is ParseSafetyViolationError with the provider
// supplied by the caller. The adapter raising the error always knows which
// provider it called, and a caller that says so cannot be misattributed.
func ParseSafetyViolationErrorFor(p models.SafetyViolationProvider, err error) (*SafetyViolation, bool) {
	return parseSafetyViolation(err, p)
}

func parseSafetyViolation(err error, p models.SafetyViolationProvider) (*SafetyViolation, bool) {
	if err == nil {
		return nil, false
	}
	raw := err.Error()
	lower := strings.ToLower(raw)
	if !looksLikeSafetyViolation(raw, lower) {
		return nil, false
	}

	// Fall back to sniffing the message only when the caller did not say.
	provider := p
	if provider == "" {
		provider = detectProvider(lower)
	}

	violation := &SafetyViolation{
		Provider:        provider,
		ProviderMessage: extractMessage(raw),
		RawError:        raw,
	}
	violation.ProviderCode = extractCode(raw, lower)
	violation.ViolationType = extractViolationType(raw, lower)
	return violation, true
}

// WrapSafetyViolationError marks err as a content-policy refusal by p, when it
// looks like one; anything else is returned unchanged. p is required rather than
// inferred: the error text is not a reliable signal, and requiring it makes a
// missing provider a compile error.
func WrapSafetyViolationError(p models.SafetyViolationProvider, err error) error {
	if err == nil {
		return nil
	}
	v, ok := ParseSafetyViolationErrorFor(p, err)
	if !ok {
		return err
	}
	return &SafetyViolationError{Violation: v}
}

// WrapProviderCallError annotates a provider API failure. Only errors that look
// like content-policy blocks are wrapped as SafetyViolationError; transport,
// auth, and validation failures pass through with context only.
func WrapProviderCallError(p models.SafetyViolationProvider, context string, err error) error {
	if err == nil {
		return nil
	}
	if sve := WrapSafetyViolationError(p, err); sve != err {
		return sve
	}
	return fmt.Errorf("%s: %w", context, err)
}

func looksLikeSafetyViolation(raw, lower string) bool {
	strongNeedles := []string{
		"moderation_blocked",
		"rejected by the safety system",
		"safety_violations=",
		"content filtering policy",
		"request was blocked by content filtering policy",
		`"code":"moderation_blocked"`,
	}
	for _, n := range strongNeedles {
		if strings.Contains(lower, n) {
			return true
		}
	}

	// Fallback to structured payload markers when available.
	code := strings.ToLower(extractJSONField(raw, "code"))
	if code == "moderation_blocked" {
		return true
	}
	msg := strings.ToLower(extractJSONField(raw, "message"))
	if strings.Contains(msg, "rejected by the safety system") {
		return true
	}
	if strings.Contains(msg, "content filtering policy") && (strings.Contains(lower, "anthropic") || strings.Contains(lower, "claude")) {
		return true
	}

	return false
}

// detectProvider guesses the provider from the error text, matching the adapters'
// own wrap prefixes. Only reached via ParseSafetyViolationError, where the caller
// cannot say; OpenAI is the fallback because that path parses raw OpenAI errors.
func detectProvider(lower string) models.SafetyViolationProvider {
	switch {
	case strings.Contains(lower, "anthropic"), strings.Contains(lower, "claude"):
		return models.SafetyViolationProviderAnthropic
	case strings.Contains(lower, "gemini"), strings.Contains(lower, "google"):
		return models.SafetyViolationProviderGoogle
	case strings.Contains(lower, "deepseek"):
		return models.SafetyViolationProviderDeepSeek
	case strings.Contains(lower, "mistral"):
		return models.SafetyViolationProviderMistral
	case strings.Contains(lower, "xiaomi"):
		return models.SafetyViolationProviderXiaomi
	case strings.Contains(lower, "qwen"):
		return models.SafetyViolationProviderQwen
	case strings.Contains(lower, "zai"), strings.Contains(lower, "glm"):
		return models.SafetyViolationProviderZAI
	case strings.Contains(lower, "local model"):
		return models.SafetyViolationProviderLocal
	default:
		return models.SafetyViolationProviderOpenAI
	}
}

func extractMessage(raw string) string {
	if m := extractJSONField(raw, "message"); m != "" {
		return m
	}
	return raw
}

func extractCode(raw, lower string) string {
	if c := extractJSONField(raw, "code"); c != "" {
		return c
	}
	if strings.Contains(lower, "moderation_blocked") {
		return "moderation_blocked"
	}
	return ""
}

func extractViolationType(raw, lower string) string {
	if idx := strings.Index(lower, "safety_violations=["); idx >= 0 {
		start := idx + len("safety_violations=[")
		end := strings.Index(lower[start:], "]")
		if end > 0 {
			return strings.TrimSpace(lower[start : start+end])
		}
	}
	if t := extractJSONField(raw, "type"); t != "" {
		return t
	}
	return ""
}

func extractJSONField(raw, fieldName string) string {
	start := strings.Index(raw, "{")
	if start == -1 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw[start:]), &payload); err != nil {
		return ""
	}
	if v, ok := payload[fieldName].(string); ok {
		return strings.TrimSpace(v)
	}
	if nested, ok := payload["error"].(map[string]any); ok {
		if v, ok := nested[fieldName].(string); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
