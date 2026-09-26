package provider

import (
	"context"
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
)

// These tests pin the terminal-event handling of the Responses streaming loop.
// Before the fix, only "response.completed" was captured, so a stream that ended
// with "response.incomplete", "response.failed", or "error" produced the opaque
// "stream finished without response.completed event" (issue #132).

func TestResponsesNewStreaming_IncompleteIsNotAnError(t *testing.T) {
	respJSON := `{"id":"resp_inc","object":"response","created_at":1,"model":"test",` +
		`"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},` +
		`"output":[{"type":"message","id":"m1","role":"assistant","status":"incomplete",` +
		`"content":[{"type":"output_text","text":"partial"}]}],` +
		`"usage":{"input_tokens":5,"output_tokens":8,"total_tokens":13}}`
	frames := []string{
		`{"type":"response.output_text.delta","delta":"partial","sequence_number":1}`,
		`{"type":"response.incomplete","sequence_number":2,"response":` + respJSON + `}`,
	}
	srv := sseServer(t, frames)
	defer srv.Close()

	var deltas []string
	resp, deltaEmitted, err := newTestOpenAIProvider(srv.URL).responsesNewStreaming(
		context.Background(),
		responses.ResponseNewParams{Model: "test"},
		func(d string) { deltas = append(deltas, d) },
	)
	require.NoError(t, err)
	require.True(t, deltaEmitted)
	require.NotNil(t, resp)
	require.Equal(t, "resp_inc", resp.ID)
	// The reason survives so GenerateResponse.StopReason can carry it, exactly as
	// the non-streaming path already does.
	require.Equal(t, "max_output_tokens", openAIStopReason(resp))
	require.Equal(t, []string{"partial"}, deltas)
	require.Equal(t, "partial", ProcessResponseOutput(resp))
}

func TestResponsesNewStreaming_FailedEventReturnsDescriptiveError(t *testing.T) {
	respJSON := `{"id":"resp_fail","object":"response","created_at":1,"model":"test",` +
		`"status":"failed","error":{"code":"server_error","message":"the model exploded"},` +
		`"output":[],"usage":{"input_tokens":1,"output_tokens":0,"total_tokens":1}}`
	frames := []string{
		`{"type":"response.failed","sequence_number":1,"response":` + respJSON + `}`,
	}
	srv := sseServer(t, frames)
	defer srv.Close()

	resp, _, err := newTestOpenAIProvider(srv.URL).responsesNewStreaming(
		context.Background(),
		responses.ResponseNewParams{Model: "test"},
		nil,
	)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "stream failed")
	require.Contains(t, err.Error(), "the model exploded")
	require.Contains(t, err.Error(), "server_error")
	require.Contains(t, err.Error(), "resp_fail")
}

func TestResponsesNewStreaming_FailedAfterDeltasKeepsDeltaEmitted(t *testing.T) {
	// A failure that lands after visible text must still surface a descriptive
	// error, and must report deltaEmitted=true so the retry guard in
	// callWithRetry does not re-issue a call whose output the user already saw.
	respJSON := `{"id":"resp_fail2","object":"response","created_at":1,"model":"test",` +
		`"status":"failed","error":{"code":"server_error","message":"died mid-stream"},` +
		`"output":[],"usage":{"input_tokens":1,"output_tokens":3,"total_tokens":4}}`
	frames := []string{
		`{"type":"response.output_text.delta","delta":"half an ","sequence_number":1}`,
		`{"type":"response.output_text.delta","delta":"answer","sequence_number":2}`,
		`{"type":"response.failed","sequence_number":3,"response":` + respJSON + `}`,
	}
	srv := sseServer(t, frames)
	defer srv.Close()

	var deltas []string
	resp, deltaEmitted, err := newTestOpenAIProvider(srv.URL).responsesNewStreaming(
		context.Background(),
		responses.ResponseNewParams{Model: "test"},
		func(d string) { deltas = append(deltas, d) },
	)
	require.Error(t, err)
	require.Nil(t, resp)
	require.True(t, deltaEmitted)
	require.Equal(t, []string{"half an ", "answer"}, deltas)
	require.Contains(t, err.Error(), "died mid-stream")
}

func TestResponsesNewStreaming_ErrorEventReturnsDescriptiveError(t *testing.T) {
	frames := []string{
		`{"type":"error","sequence_number":1,"code":"rate_limit_exceeded","message":"slow down","param":"model"}`,
	}
	srv := sseServer(t, frames)
	defer srv.Close()

	resp, _, err := newTestOpenAIProvider(srv.URL).responsesNewStreaming(
		context.Background(),
		responses.ResponseNewParams{Model: "test"},
		nil,
	)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "stream error")
	require.Contains(t, err.Error(), "slow down")
	require.Contains(t, err.Error(), "rate_limit_exceeded")
}

func TestResponsesNewStreaming_NoTerminalEventIsTruncationError(t *testing.T) {
	// A stream that ends cleanly but never delivers a terminal event: a truncated
	// or dropped stream. Still an error, but now distinguishable from a provider
	// outcome and it reports whether any delta was seen.
	frames := []string{
		`{"type":"response.output_text.delta","delta":"hi","sequence_number":1}`,
	}
	srv := sseServer(t, frames)
	defer srv.Close()

	resp, deltaEmitted, err := newTestOpenAIProvider(srv.URL).responsesNewStreaming(
		context.Background(),
		responses.ResponseNewParams{Model: "test"},
		nil,
	)
	require.Error(t, err)
	require.Nil(t, resp)
	require.True(t, deltaEmitted)
	require.Contains(t, err.Error(), "without a terminal event")
	require.Contains(t, err.Error(), "delta_emitted=true")
}

func TestResponsesNewStreaming_CompletedStillWorks(t *testing.T) {
	frames := []string{
		`{"type":"response.output_text.delta","delta":"Hi there","sequence_number":1}`,
		`{"type":"response.completed","sequence_number":2,"response":` +
			responseTextJSON("resp_ok", "Hi there") + `}`,
	}
	srv := sseServer(t, frames)
	defer srv.Close()

	resp, deltaEmitted, err := newTestOpenAIProvider(srv.URL).responsesNewStreaming(
		context.Background(),
		responses.ResponseNewParams{Model: "test"},
		nil,
	)
	require.NoError(t, err)
	require.True(t, deltaEmitted)
	require.NotNil(t, resp)
	require.Equal(t, "resp_ok", resp.ID)
	require.Equal(t, "Hi there", ProcessResponseOutput(resp))
}
