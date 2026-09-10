package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// TestRecoverAsyncMessageJob_ContainsPanic locks in the pod-crash guard: a panic in the
// async chat-message goroutine must be contained to its job, not propagated (which would
// take down the process). It must stay contained even when recording the failed status
// fails — here the datastore call errors because no expectation is registered.
func TestRecoverAsyncMessageJob_ContainsPanic(t *testing.T) {
	ds, _, cleanup := newTestDatastore(t)
	defer cleanup()
	a := newTestAgent(ds)

	require.NotPanics(t, func() {
		defer a.recoverAsyncMessageJob(context.Background(), uuid.New(), uuid.New(), uuid.New())
		panic("boom in post-inference checkpoint")
	})
}

// TestRecoverAsyncMessageJob_NoPanicIsNoop verifies the guard is inert on the happy path:
// with no panic in flight it must not touch the datastore (no sqlmock expectations set).
func TestRecoverAsyncMessageJob_NoPanicIsNoop(t *testing.T) {
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	a := newTestAgent(ds)

	require.NotPanics(t, func() {
		defer a.recoverAsyncMessageJob(context.Background(), uuid.New(), uuid.New(), uuid.New())
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

// Test buildAttachmentLabels function
func TestBuildAttachmentLabels_EmptyAttachments(t *testing.T) {
	attachments := []*models.FileAttachment{}
	labels := buildAttachmentLabels(attachments)
	assert.Empty(t, labels, "Empty attachments should return empty labels")
}

func TestCancelledInputTokensForBilling_UsesProviderUsage(t *testing.T) {
	t.Parallel()

	cause := provider.WrapCanceledWithUsage(context.Canceled, provider.CancelUsage{
		InputTokens:  321,
		OutputTokens: 87,
		Available:    true,
		Source:       "claude_stream_usage",
	})
	inputTokens, source, outputTokens, outputKnown, providerUsageAvailable := cancelledInputTokensForBilling(
		cause,
		&chatContext{modelProvider: "anthropic", model: "claude-sonnet"},
		nil,
		"",
		provider.NewTokenCounter(),
	)
	assert.Equal(t, int64(321), inputTokens)
	assert.Equal(t, "claude_stream_usage", source)
	assert.Equal(t, int64(87), outputTokens)
	assert.True(t, outputKnown)
	assert.True(t, providerUsageAvailable)
}

func TestCancelledInputTokensForBilling_FallsBackToMinimumOne(t *testing.T) {
	t.Parallel()

	inputTokens, source, outputTokens, outputKnown, providerUsageAvailable := cancelledInputTokensForBilling(
		errors.New("no usage"),
		nil,
		nil,
		"",
		provider.NewTokenCounter(),
	)
	assert.Equal(t, int64(1), inputTokens)
	assert.Equal(t, "fallback_min_1", source)
	assert.Equal(t, int64(0), outputTokens)
	assert.False(t, outputKnown)
	assert.False(t, providerUsageAvailable)
}

func TestCancelledInputTokensForBilling_FallbackWhenProviderInputMissing(t *testing.T) {
	t.Parallel()

	cause := provider.WrapCanceledWithUsage(context.Canceled, provider.CancelUsage{
		InputTokens:  0,
		OutputTokens: 12,
		Available:    false,
		Source:       "openai_stream_usage_unavailable",
	})
	modelCtx := &provider.ModelContext{}
	modelCtx.Append(provider.SegmentKindSystemPrompt, provider.RoleDeveloper, "You are a helpful assistant with concise answers.", true)
	modelCtx.Append(provider.SegmentKindUserMessage, provider.RoleUser, "Please draft a cancellation billing explanation with examples.", false)
	inputTokens, source, outputTokens, outputKnown, providerUsageAvailable := cancelledInputTokensForBilling(
		cause,
		&chatContext{modelProvider: "openai", model: "gpt-4o"},
		modelCtx,
		"Partial assistant output that was generated before cancellation.",
		provider.NewTokenCounter(),
	)
	assert.Greater(t, inputTokens, int64(1))
	assert.Equal(t, "openai_local_estimate", source)
	assert.Greater(t, outputTokens, int64(0))
	assert.True(t, outputKnown)
	assert.False(t, providerUsageAvailable)
}

func TestCancelledInputTokensForBilling_OpenAIEstimateFailureFallsBackToOne(t *testing.T) {
	t.Parallel()

	cause := provider.WrapCanceledWithUsage(context.Canceled, provider.CancelUsage{
		Available: false,
		Source:    "openai_stream_usage_unavailable",
	})
	inputTokens, source, outputTokens, outputKnown, providerUsageAvailable := cancelledInputTokensForBilling(
		cause,
		&chatContext{modelProvider: "openai", model: "gpt-4o"},
		nil,
		"",
		nil,
	)
	assert.Equal(t, int64(1), inputTokens)
	assert.Equal(t, "openai_stream_usage_unavailable", source)
	assert.Equal(t, int64(0), outputTokens)
	assert.False(t, outputKnown)
	assert.False(t, providerUsageAvailable)
}

// A provider call can succeed at the transport level and still carry no assistant text.
// Persisting that as a normal assistant message produced a turn that looked answered but
// was blank, with no error and no retry offered, so the turn must fail instead.
func TestAssertGenerationProducedOutput(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	chatCtx := &chatContext{model: "claude-sonnet-4-6", modelProvider: "anthropic"}

	t.Run("empty text with no attachments fails the turn", func(t *testing.T) {
		err := a.assertGenerationProducedOutput("anthropic", chatCtx,
			&provider.GenerateResponse{ID: "msg_1", StopReason: "max_tokens", OutputTokens: 4096}, nil)

		require.Error(t, err)
		// The message has to carry the two fields that separate "generated text we failed
		// to extract" from "nothing came back", since that is the whole diagnostic value.
		require.Contains(t, err.Error(), "max_tokens")
		require.Contains(t, err.Error(), "4096")
	})

	t.Run("unreported stop reason is still named", func(t *testing.T) {
		err := a.assertGenerationProducedOutput("zai", chatCtx, &provider.GenerateResponse{ID: "msg_2"}, nil)

		require.Error(t, err)
		require.Contains(t, err.Error(), "unreported")
	})

	t.Run("whitespace-only text is empty", func(t *testing.T) {
		err := a.assertGenerationProducedOutput("anthropic", chatCtx,
			&provider.GenerateResponse{ID: "msg_3", Text: "  \n\t "}, nil)

		require.Error(t, err)
	})

	t.Run("text passes", func(t *testing.T) {
		err := a.assertGenerationProducedOutput("anthropic", chatCtx,
			&provider.GenerateResponse{ID: "msg_4", Text: "a real reply"}, nil)

		require.NoError(t, err)
	})

	// An image ritual legitimately answers with an attachment and no prose.
	t.Run("attachment-only turn passes", func(t *testing.T) {
		err := a.assertGenerationProducedOutput("openai", chatCtx,
			&provider.GenerateResponse{ID: "msg_5"},
			[]*models.FileAttachment{{Name: "image.png", FileType: "image/png"}})

		require.NoError(t, err)
	})

	t.Run("nil attachment entries do not count as output", func(t *testing.T) {
		err := a.assertGenerationProducedOutput("openai", chatCtx,
			&provider.GenerateResponse{ID: "msg_6"}, []*models.FileAttachment{nil})

		require.Error(t, err)
	})
}

// Pins that runGeneration actually consults the empty-response guard. The unit test
// above covers the predicate; this covers the wiring, which is the part a refactor can
// silently drop — and dropping it restores the original defect (a blank assistant row
// saved as a normal turn) with every test still green.
//
// The mock is driven through the real adapter interface and returns empty text, so the
// turn must fail before saveAgentResponse. That early return is also why this needs no
// datastore: newJobDraftDeltaBuffer degrades to an inert buffer without one, and nothing
// past the guard is reached.
func TestRunGeneration_FailsTurnOnEmptyModelResponse(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	chatCtx := &chatContext{
		chat:          &models.Chat{ID: uuid.New(), UserID: uuid.New()},
		model:         "mock-model",
		modelProvider: "mock",
	}
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{
		Mode:           provider.MockModeFixed,
		FixedResponses: []string{""},
	})

	agentMessage, result, err := a.runGeneration(
		context.Background(),
		chatCtx.chat.UserID,
		nil, // no job: the draft buffer is inert, and the turn fails before persistence
		&models.ChatMessage{ID: uuid.New(), ChatID: chatCtx.chat.ID},
		chatCtx,
		adapter,
		generationOptions{provider: "mock"},
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "empty response")
	require.Nil(t, agentMessage, "no assistant message may be persisted for an empty turn")
	require.Nil(t, result)
}

func TestBackfillSummaryMemoriesSkipsWithoutMemoryTool(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}

	stats := a.BackfillSummaryMemories(context.Background(), 10)

	assert.Equal(t, 0, stats.Processed)
	assert.Equal(t, 1, stats.Skipped)
	assert.Equal(t, 0, stats.Failed)
}

func TestBuildAttachmentLabels_AttachmentsWithoutFileID(t *testing.T) {
	attachments := []*models.FileAttachment{
		{Name: "document.pdf", FileID: nil},
		{Name: "image.png", FileID: nil},
	}
	labels := buildAttachmentLabels(attachments)
	assert.Empty(t, labels, "Attachments without FileID should not generate labels")
}

func TestBuildAttachmentLabels_AttachmentsWithFileID(t *testing.T) {
	fileID1 := "file-123"
	fileID2 := "file-456"
	attachments := []*models.FileAttachment{
		{Name: "document.pdf", FileID: &fileID1},
		{Name: "image.png", FileID: &fileID2},
	}

	labels := buildAttachmentLabels(attachments)

	assert.Len(t, labels, 2, "Should generate 2 labels")
	assert.Equal(t, "document.pdf (file_id: file-123)", labels[0])
	assert.Equal(t, "image.png (file_id: file-456)", labels[1])
}

func TestBuildAttachmentLabels_MixedAttachments(t *testing.T) {
	fileID1 := "file-123"
	attachments := []*models.FileAttachment{
		{Name: "document.pdf", FileID: &fileID1},
		{Name: "image.png", FileID: nil},
		{Name: "data.csv", FileID: nil},
	}

	labels := buildAttachmentLabels(attachments)

	assert.Len(t, labels, 1, "Should only generate label for attachment with FileID")
	assert.Equal(t, "document.pdf (file_id: file-123)", labels[0])
}

// Test appendMemoryMessages function
func TestAppendMemoryMessages_NoMemories(t *testing.T) {
	var messages []responses.ResponseInputItemUnionParam
	memories := []string{}

	result := appendMemoryMessages(messages, memories)

	assert.Empty(t, result, "Should return empty when no memories provided")
}

func TestAppendMemoryMessages_SingleMemory(t *testing.T) {
	var messages []responses.ResponseInputItemUnionParam
	memories := []string{"User prefers dark mood"}

	result := appendMemoryMessages(messages, memories)

	assert.Len(t, result, 1, "Should add one message for memories")
	// Note: We can't easily assert the content without accessing internal fields
	// but we can verify the count is correct
}

func TestAppendMemoryMessages_MultipleMemories(t *testing.T) {
	var messages []responses.ResponseInputItemUnionParam
	memories := []string{
		"User prefers dark mood",
		"User is a software engineer",
		"User lives in San Francisco",
	}

	result := appendMemoryMessages(messages, memories)

	assert.Len(t, result, 1, "Should add one message containing all memories")
}

func TestAppendMemoryMessages_PreservesExistingMessages(t *testing.T) {
	existingMessages := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("Existing message", provider.RoleUser),
	}
	memories := []string{"User prefers dark mood"}

	result := appendMemoryMessages(existingMessages, memories)

	assert.Len(t, result, 2, "Should preserve existing messages and add memory message")
}

// Test appendAttachmentMessages function
func TestAppendAttachmentMessages_NoAttachments(t *testing.T) {
	var messages []responses.ResponseInputItemUnionParam
	attachments := []*models.FileAttachment{}

	result := appendAttachmentMessages(messages, attachments)

	assert.Empty(t, result, "Should return empty when no attachments with FileID")
}

func TestAppendAttachmentMessages_AttachmentsWithoutFileID(t *testing.T) {
	var messages []responses.ResponseInputItemUnionParam
	attachments := []*models.FileAttachment{
		{Name: "document.pdf", FileID: nil},
	}

	result := appendAttachmentMessages(messages, attachments)

	assert.Empty(t, result, "Should not add message when attachments lack FileID")
}

func TestAppendAttachmentMessages_AttachmentsWithFileID(t *testing.T) {
	var messages []responses.ResponseInputItemUnionParam
	fileID1 := "file-123"
	fileID2 := "file-456"
	attachments := []*models.FileAttachment{
		{Name: "document.pdf", FileID: &fileID1},
		{Name: "image.png", FileID: &fileID2},
	}

	result := appendAttachmentMessages(messages, attachments)

	assert.Len(t, result, 1, "Should add one message for attachments")
}

func TestAppendAttachmentMessages_PreservesExistingMessages(t *testing.T) {
	existingMessages := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("Existing message", provider.RoleUser),
	}
	fileID := "file-123"
	attachments := []*models.FileAttachment{
		{Name: "document.pdf", FileID: &fileID},
	}

	result := appendAttachmentMessages(existingMessages, attachments)

	assert.Len(t, result, 2, "Should preserve existing messages and add attachment message")
}

func TestMemoryToolCallsForChatContext_MemoryEnrichmentFailed(t *testing.T) {
	t.Parallel()

	chatCtx := &chatContext{
		memories:               []string{"should be ignored"},
		memoryEnrichmentFailed: true,
	}
	toolCalls := memoryToolCallsForChatContext(chatCtx)

	if assert.Len(t, toolCalls, 1) {
		assert.Equal(t, memoryEnrichmentToolCallName, toolCalls[0].ToolName)
		assert.Equal(t, memoryEnrichmentFailureMessage, toolCalls[0].ToolError)
		assert.Empty(t, toolCalls[0].ToolOutput)
	}
}

func TestMemoryToolCallsForChatContext_MemoriesPresent(t *testing.T) {
	t.Parallel()

	chatCtx := &chatContext{
		memories: []string{"m1", "m2"},
	}
	toolCalls := memoryToolCallsForChatContext(chatCtx)

	if assert.Len(t, toolCalls, 1) {
		assert.Equal(t, "Load Memory", toolCalls[0].ToolName)
		assert.Empty(t, toolCalls[0].ToolError)
		assert.Contains(t, toolCalls[0].ToolOutput, "Retrieved memories:")
	}
}

func TestMemoryToolCallsForChatContext_NoMemoriesNoFailure(t *testing.T) {
	t.Parallel()

	chatCtx := &chatContext{}
	toolCalls := memoryToolCallsForChatContext(chatCtx)
	assert.Empty(t, toolCalls)
}

func TestGetMemoriesBestEffort_DegradesOnError(t *testing.T) {
	t.Parallel()

	a := &Agent{
		logger: zap.NewNop(),
		testHooks: agentTestHooks{
			GetMemoriesOverride: func(ctx context.Context, userID uuid.UUID, chatID uuid.UUID, personalityID uuid.UUID, userMessage string) ([]string, error) {
				return nil, fmt.Errorf("embeddings api down")
			},
		},
	}

	memories, liveMemories, failed := a.getMemoriesBestEffort(context.Background(), uuid.New(), uuid.New(), uuid.New(), "hi")
	assert.True(t, failed)
	assert.Equal(t, []string{}, memories)
	assert.Nil(t, liveMemories)
}

func TestGetMemoriesBestEffort_PassesThroughOnSuccess(t *testing.T) {
	t.Parallel()

	a := &Agent{
		logger: zap.NewNop(),
		testHooks: agentTestHooks{
			GetMemoriesOverride: func(ctx context.Context, userID uuid.UUID, chatID uuid.UUID, personalityID uuid.UUID, userMessage string) ([]string, error) {
				return []string{"m1"}, nil
			},
		},
	}

	memories, liveMemories, failed := a.getMemoriesBestEffort(context.Background(), uuid.New(), uuid.New(), uuid.New(), "hi")
	assert.False(t, failed)
	assert.Equal(t, []string{"m1"}, memories)
	assert.Nil(t, liveMemories)
}
