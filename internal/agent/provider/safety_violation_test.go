package provider

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestParseSafetyViolationErrorOpenAI(t *testing.T) {
	err := errors.New(`POST "https://api.openai.com/v1/images/generations": 400 Bad Request {"message":"Your request was rejected by the safety system. safety_violations=[sexual].","type":"image_generation_user_error","code":"moderation_blocked"}`)

	v, ok := ParseSafetyViolationError(err)
	require.True(t, ok)
	require.NotNil(t, v)
	require.Equal(t, models.SafetyViolationProviderOpenAI, v.Provider)
	require.Equal(t, "moderation_blocked", v.ProviderCode)
	require.Contains(t, v.ProviderMessage, "safety system")
	require.Equal(t, "sexual", v.ViolationType)
}

func TestParseSafetyViolationErrorAnthropic(t *testing.T) {
	err := errors.New(`Anthropic API call failed: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Request blocked by content filtering policy"}}`)

	v, ok := ParseSafetyViolationError(err)
	require.True(t, ok)
	require.NotNil(t, v)
	require.Equal(t, models.SafetyViolationProviderAnthropic, v.Provider)
	require.Contains(t, v.ProviderMessage, "content filtering policy")
}

func TestParseSafetyViolationErrorNonSafety(t *testing.T) {
	err := errors.New("dial tcp timeout")
	v, ok := ParseSafetyViolationError(err)
	require.False(t, ok)
	require.Nil(t, v)
}

func TestParseSafetyViolationErrorDoesNotMatchGenericPolicyText(t *testing.T) {
	err := errors.New("request failed due to account policy mismatch")
	v, ok := ParseSafetyViolationError(err)
	require.False(t, ok)
	require.Nil(t, v)
}

func TestParseSafetyViolationErrorDoesNotMatchGenericSafetyWord(t *testing.T) {
	err := errors.New("safety check endpoint unavailable")
	v, ok := ParseSafetyViolationError(err)
	require.False(t, ok)
	require.Nil(t, v)
}

func TestWrapProviderCallError(t *testing.T) {
	t.Parallel()

	// Note the OpenAI-shaped upstream URL on a Gemini call: the Gemini adapter
	// speaks the OpenAI-compatible API, so the error text names api.openai.com.
	// Attribution therefore cannot come from the message, and this case is why —
	// text sniffing recorded it as an OpenAI refusal.
	safetyErr := errors.New(`POST "https://api.openai.com/v1/chat/completions": 400 {"message":"Your request was rejected by the safety system.","code":"moderation_blocked"}`)
	wrapped := WrapProviderCallError(models.SafetyViolationProviderGoogle, "Gemini API call failed", safetyErr)
	v, ok := IsSafetyViolationError(wrapped)
	require.True(t, ok, "safety-like errors should be wrapped as SafetyViolationError")
	require.Equal(t, models.SafetyViolationProviderGoogle, v.Provider,
		"the caller's provider must win over anything inferred from the message")

	transportErr := errors.New("dial tcp: connection refused")
	wrapped = WrapProviderCallError(models.SafetyViolationProviderGoogle, "Gemini API call failed", transportErr)
	_, ok = IsSafetyViolationError(wrapped)
	require.False(t, ok, "transport errors must not be classified as safety violations")
	require.ErrorContains(t, wrapped, "Gemini API call failed")
	require.ErrorIs(t, wrapped, transportErr)
}

// Every adapter that can raise a safety violation must record its own provider.
// Before the provider became an explicit argument, detectProvider recognised only
// Anthropic and defaulted the rest to OpenAI, so all five of the newer providers
// were logged as OpenAI refusals.
func TestWrapSafetyViolationErrorAttributesTheCallersProvider(t *testing.T) {
	t.Parallel()

	// Shaped like each adapter's real wrap site, with a needle
	// looksLikeSafetyViolation recognises.
	const blocked = `{"code":"moderation_blocked","message":"request was blocked"}`

	cases := []struct {
		name     string
		provider models.SafetyViolationProvider
		wrapText string
	}{
		{"openai", models.SafetyViolationProviderOpenAI, "OpenAI API call failed: " + blocked},
		{"anthropic", models.SafetyViolationProviderAnthropic, "Anthropic API call failed: " + blocked},
		{"google", models.SafetyViolationProviderGoogle, "Gemini API call failed: " + blocked},
		{"mistral", models.SafetyViolationProviderMistral, "Mistral API call failed: " + blocked},
		{"deepseek", models.SafetyViolationProviderDeepSeek, "DeepSeek API call failed: " + blocked},
		{"qwen", models.SafetyViolationProviderQwen, "Qwen API call failed: " + blocked},
		{"xiaomi", models.SafetyViolationProviderXiaomi, "Xiaomi API call failed: " + blocked},
		{"local", models.SafetyViolationProviderLocal, "local model API call failed: " + blocked},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wrapped := WrapSafetyViolationError(tc.provider, errors.New(tc.wrapText))
			v, ok := IsSafetyViolationError(wrapped)
			require.True(t, ok)
			require.Equal(t, tc.provider, v.Provider)
		})
	}
}

// The text-only path still exists for callers that genuinely cannot say which
// provider refused (the image-generation path parses a raw provider error).
// It should at least not attribute an obviously-named provider to OpenAI.
func TestParseSafetyViolationErrorFallsBackToTheMessage(t *testing.T) {
	t.Parallel()

	const blocked = `{"code":"moderation_blocked","message":"request was blocked"}`
	for _, tc := range []struct {
		text string
		want models.SafetyViolationProvider
	}{
		{"Anthropic API call failed: " + blocked, models.SafetyViolationProviderAnthropic},
		{"Mistral API call failed: " + blocked, models.SafetyViolationProviderMistral},
		{"Qwen API call failed: " + blocked, models.SafetyViolationProviderQwen},
		{"local model API call failed: " + blocked, models.SafetyViolationProviderLocal},
		{"something unattributable: " + blocked, models.SafetyViolationProviderOpenAI},
	} {
		v, ok := ParseSafetyViolationError(errors.New(tc.text))
		require.True(t, ok, tc.text)
		require.Equal(t, tc.want, v.Provider, tc.text)
	}
}
