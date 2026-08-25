package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

const (
	// JobTypeThreadRehydration is the Job.job_type for lazy summarization of an imported thread
	// triggered when the user unarchives (restores) it.
	JobTypeThreadRehydration = "thread_rehydration"

	// rehydrationKeepTurns is how many trailing user turns stay live (loaded verbatim) after
	// rehydration; everything before is captured in the checkpoint summary. Per product design the
	// restored thread keeps a few turns of real context so the next reply feels continuous.
	rehydrationKeepTurns = 5

	// rehydrationSinglePassChars is the transcript size (characters) below which we summarize in one
	// pass. Above it we chunk + roll up to stay within model/context and cost budgets on very long
	// (100+ turn) imported threads. ~200k chars is roughly 50k tokens.
	rehydrationSinglePassChars = 200_000
	// rehydrationChunkChars bounds each map-reduce chunk for long transcripts.
	rehydrationChunkChars = 120_000

	rehydrationSummaryMaxTokens = 1200
	rehydrationChunkMaxTokens   = 800

	// rehydrationWaitTimeout bounds how long an inbound inference turn stalls waiting for an
	// in-flight rehydration summary before proceeding without it (graceful degrade).
	rehydrationWaitTimeout  = 120 * time.Second
	rehydrationPollInterval = 1 * time.Second

	// importMemoryMaxPerThread caps how many seeded memories we keep per imported thread so one long
	// conversation can't flood the user's memory store. Set generously: extraction is intentionally
	// eager (see importMemoryExtractionInstructions), so this is a safety ceiling, not a target.
	importMemoryMaxPerThread = 30
	// importMemoryExtractMaxTokens bounds the JSON memory-extraction response per chunk; sized for the
	// eager target (~15-25 memories) plus headroom.
	importMemoryExtractMaxTokens = 3000
)

// WaitForThreadRehydration stalls until an in-flight rehydration for the chat reaches a terminal
// state (ready/failed), or the timeout/ctx elapses. It is a no-op for threads that are not pending
// (native threads, already-ready imports). This is the "stall the inference job" gate: it lets a user
// send a message immediately after restoring a thread while the summary finishes, instead of blocking
// the UI. On timeout it returns and the caller proceeds with whatever context exists (degraded).
func (a *Agent) WaitForThreadRehydration(ctx context.Context, userID, chatID uuid.UUID) {
	state, err := a.ds.GetChatRehydrationState(ctx, userID, chatID)
	if err != nil {
		a.logger.Warn("rehydration gate: failed to read state", zap.String("chat_id", chatID.String()), zap.Error(err))
		return
	}
	if !isRehydrationInFlight(state) {
		return
	}

	a.logger.Info("rehydration gate: stalling turn until summary ready", zap.String("chat_id", chatID.String()))
	deadline := time.Now().Add(rehydrationWaitTimeout)
	ticker := time.NewTicker(rehydrationPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state, err = a.ds.GetChatRehydrationState(ctx, userID, chatID)
			if err != nil {
				// A missing/unauthorized chat will never settle — stop instead of polling to timeout.
				if errors.Is(err, datastore.ErrChatNotFound) {
					a.logger.Warn("rehydration gate: chat not found; proceeding", zap.String("chat_id", chatID.String()))
					return
				}
				a.logger.Warn("rehydration gate: poll failed", zap.String("chat_id", chatID.String()), zap.Error(err))
				continue
			}
			if !isRehydrationInFlight(state) {
				a.logger.Info("rehydration gate: summary settled, proceeding",
					zap.String("chat_id", chatID.String()), zap.String("state", state))
				return
			}
			if time.Now().After(deadline) {
				a.logger.Warn("rehydration gate: timed out waiting for summary; proceeding degraded",
					zap.String("chat_id", chatID.String()))
				return
			}
		}
	}
}

func isRehydrationInFlight(state string) bool {
	return state == models.RehydrationStatePending || state == models.RehydrationStateProcessing
}

// EnqueueThreadRehydration marks an imported thread as pending rehydration and runs summarization in
// the background. It is safe to call on the request goroutine: it detaches the context so the work
// outlives the HTTP response. Callers should only invoke this for imported threads that have not yet
// been rehydrated (source set, empty checkpoint summary).
func (a *Agent) EnqueueThreadRehydration(ctx context.Context, userID, chatID uuid.UUID) {
	detachedCtx, ok := middleware.CopyUserToIDContext(ctx, context.Background())
	if !ok {
		a.logger.Error("thread rehydration: missing user in context", zap.String("chat_id", chatID.String()))
		return
	}

	if err := a.ds.SetChatRehydrationState(detachedCtx, userID, chatID, models.RehydrationStatePending); err != nil {
		a.logger.Error("thread rehydration: failed to mark pending", zap.String("chat_id", chatID.String()), zap.Error(err))
		return
	}

	job, err := a.ds.CreateJob(detachedCtx, userID, models.Job{
		JobType:   JobTypeThreadRehydration,
		Reference: chatID.String(),
		Status:    models.JobStatusPending,
	})
	if err != nil {
		a.logger.Error("thread rehydration: failed to create job", zap.String("chat_id", chatID.String()), zap.Error(err))
		// Leave state pending; the gate falls back gracefully after timeout.
		return
	}

	go a.runThreadRehydration(detachedCtx, userID, chatID, job.ID)
}

// runThreadRehydration executes the summarization and persists the checkpoint + window pointer.
// On any failure it marks the chat rehydration_state=failed so the inference gate stops waiting.
func (a *Agent) runThreadRehydration(ctx context.Context, userID, chatID, jobID uuid.UUID) {
	defer func() {
		if v := recover(); v != nil {
			a.logger.Error("thread rehydration: panic",
				zap.String("chat_id", chatID.String()),
				zap.Any("panic", v),
				zap.ByteString("stack", debug.Stack()))
			a.failRehydration(ctx, userID, chatID, jobID, "panic during rehydration")
		}
	}()

	if _, err := a.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusProcessing, ""); err != nil {
		a.logger.Warn("thread rehydration: failed to mark job processing", zap.String("job_id", jobID.String()), zap.Error(err))
	}
	if err := a.ds.SetChatRehydrationState(ctx, userID, chatID, models.RehydrationStateProcessing); err != nil {
		a.logger.Warn("thread rehydration: failed to mark processing", zap.String("chat_id", chatID.String()), zap.Error(err))
	}

	msgs, err := a.ds.GetChatMessagesForSummary(ctx, userID, chatID)
	if err != nil {
		a.failRehydration(ctx, userID, chatID, jobID, fmt.Sprintf("load messages: %v", err))
		return
	}

	summarizeMsgs, windowStartIdx, needs := splitForRehydration(msgs, rehydrationKeepTurns)
	if !needs {
		// Thread is short enough to load in full; no summary needed. Mark ready without a checkpoint
		// so the entire (small) history is used as context.
		if err := a.ds.SetChatRehydrationState(ctx, userID, chatID, models.RehydrationStateReady); err != nil {
			a.failRehydration(ctx, userID, chatID, jobID, fmt.Sprintf("mark ready: %v", err))
			return
		}
		// Seed long-term memories from the (short) thread too — even a few turns can carry durable
		// facts. Runs after the gate is released; best-effort so it never fails the job.
		a.extractAndStoreImportedMemories(ctx, userID, chatID, msgs)
		if _, err := a.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusComplete, ""); err != nil {
			a.logger.Warn("thread rehydration: failed to complete short-thread job", zap.String("job_id", jobID.String()), zap.Error(err))
		}
		a.logger.Info("thread rehydration: thread short enough, no summary needed",
			zap.String("chat_id", chatID.String()), zap.Int("messages", len(msgs)))
		return
	}

	var summary string
	if a.nonVendorLLM() {
		// Mock/local mode: summarization is a provider call. Use a deterministic
		// fake so the rehydration state machine still completes and imported
		// threads remain chattable (WaitForThreadRehydration gates every turn on them).
		summary = fmt.Sprintf("Mock rehydration summary of %d imported messages.", len(summarizeMsgs))
		a.logger.Debug("mock/local mode: using deterministic rehydration summary", zap.String("chat_id", chatID.String()))
	} else {
		var err error
		summary, err = a.summarizeImportedThread(ctx, userID, summarizeMsgs)
		if err != nil {
			a.failRehydration(ctx, userID, chatID, jobID, fmt.Sprintf("summarize: %v", err))
			return
		}
	}

	windowStart := msgs[windowStartIdx].SentAt
	assistantCount := countAssistantMessages(summarizeMsgs)

	if err := a.ds.SetImportedThreadRehydrated(ctx, userID, chatID, summary, assistantCount, windowStart); err != nil {
		a.failRehydration(ctx, userID, chatID, jobID, fmt.Sprintf("persist: %v", err))
		return
	}

	// Best-effort: index the summary as a searchable Summary-scope memory, mirroring live checkpoints.
	// Skipped in mock/local mode: embeddings are a provider call.
	if a.memoryTool != nil && !a.nonVendorLLM() {
		if vec, embErr := a.memoryTool.CreateEmbedding(ctx, summary); embErr != nil {
			a.logger.Warn("thread rehydration: summary embedding failed", zap.String("chat_id", chatID.String()), zap.Error(embErr))
		} else if upErr := a.ds.UpsertChatSummaryMemory(ctx, userID, chatID, summary, vec); upErr != nil {
			a.logger.Warn("thread rehydration: summary memory upsert failed", zap.String("chat_id", chatID.String()), zap.Error(upErr))
		}
	}

	// Seed long-term memories from the full transcript. The state is already ready (gate released),
	// so this extra LLM work delays only job completion, not the user's next turn. Best-effort.
	a.extractAndStoreImportedMemories(ctx, userID, chatID, msgs)

	if _, err := a.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusComplete, ""); err != nil {
		a.logger.Warn("thread rehydration: failed to complete job", zap.String("job_id", jobID.String()), zap.Error(err))
	}

	a.logger.Info("thread rehydration complete",
		zap.String("chat_id", chatID.String()),
		zap.Int("summarized_messages", len(summarizeMsgs)),
		zap.Int("kept_turns", rehydrationKeepTurns),
		zap.Time("window_start", windowStart))
}

func (a *Agent) failRehydration(ctx context.Context, userID, chatID, jobID uuid.UUID, reason string) {
	a.logger.Error("thread rehydration failed", zap.String("chat_id", chatID.String()), zap.String("reason", reason))
	if err := a.ds.SetChatRehydrationState(ctx, userID, chatID, models.RehydrationStateFailed); err != nil {
		a.logger.Error("thread rehydration: failed to mark failed", zap.String("chat_id", chatID.String()), zap.Error(err))
	}
	if _, err := a.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusFailed, reason); err != nil {
		a.logger.Error("thread rehydration: failed to mark job failed", zap.String("job_id", jobID.String()), zap.Error(err))
	}
}

// splitForRehydration partitions a chronological message list into the prefix to summarize and the
// trailing window to keep live. needs is false when there are not more than keepTurns user turns,
// meaning the thread is short enough to load fully without a summary.
func splitForRehydration(msgs []models.ChatMessage, keepTurns int) (summarize []models.ChatMessage, windowStartIdx int, needs bool) {
	if keepTurns < 1 {
		keepTurns = 1
	}

	var userIdxs []int
	for i, m := range msgs {
		if m.Origin == models.MessageOriginUser {
			userIdxs = append(userIdxs, i)
		}
	}

	if len(userIdxs) <= keepTurns {
		return nil, 0, false
	}

	windowStartIdx = userIdxs[len(userIdxs)-keepTurns]
	return msgs[:windowStartIdx], windowStartIdx, true
}

func countAssistantMessages(msgs []models.ChatMessage) int {
	n := 0
	for _, m := range msgs {
		if m.Origin == models.MessageOriginAssistant {
			n++
		}
	}
	return n
}

// buildTranscript renders messages as a role-tagged plain-text transcript for summarization.
func buildTranscript(msgs []models.ChatMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		role := "User"
		if m.Origin == models.MessageOriginAssistant {
			role = "Assistant"
		}
		text := strings.TrimSpace(m.Message)
		if text == "" {
			continue
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	return b.String()
}

// summarizeImportedThread produces a single compact summary of the pre-window transcript. For long
// threads it map-reduces: summarize message chunks, then roll the partial summaries up into one.
func (a *Agent) summarizeImportedThread(ctx context.Context, userID uuid.UUID, msgs []models.ChatMessage) (string, error) {
	transcript := buildTranscript(msgs)
	if strings.TrimSpace(transcript) == "" {
		return "", fmt.Errorf("empty transcript")
	}

	if len(transcript) <= rehydrationSinglePassChars {
		return a.summarizeText(ctx, userID, checkpointConversationSummaryInstructions, transcript, rehydrationSummaryMaxTokens)
	}

	// Map: chunk by message boundaries within a character budget and summarize each chunk.
	chunks := chunkMessagesByChars(msgs, rehydrationChunkChars)
	partials := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		part, err := a.summarizeText(ctx, userID, rehydrationChunkInstructions, buildTranscript(chunk), rehydrationChunkMaxTokens)
		if err != nil {
			return "", fmt.Errorf("summarizing chunk %d/%d: %w", i+1, len(chunks), err)
		}
		partials = append(partials, fmt.Sprintf("Section %d:\n%s", i+1, part))
	}

	// Reduce: roll the section summaries up into one cohesive summary.
	rollupInput := fmt.Sprintf("The following are ordered section summaries of one long conversation. Produce a single cohesive summary as instructed.\n\n%s", strings.Join(partials, "\n\n"))
	return a.summarizeText(ctx, userID, checkpointConversationSummaryInstructions, rollupInput, rehydrationSummaryMaxTokens)
}

// chunkMessagesByChars groups consecutive messages so each chunk's transcript stays under maxChars.
// A single oversized message becomes its own chunk (it will be truncated by the model's own limits).
func chunkMessagesByChars(msgs []models.ChatMessage, maxChars int) [][]models.ChatMessage {
	if maxChars < 1 {
		maxChars = rehydrationChunkChars
	}
	var (
		chunks  [][]models.ChatMessage
		current []models.ChatMessage
		size    int
	)
	for _, m := range msgs {
		mlen := len(m.Message) + 16 // role label + separators
		if size+mlen > maxChars && len(current) > 0 {
			chunks = append(chunks, current)
			current = nil
			size = 0
		}
		current = append(current, m)
		size += mlen
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

// extractAndStoreImportedMemories mines durable long-term memories from a freshly rehydrated imported
// thread and persists them, mirroring the live checkpoint memory-extraction pipeline minus the
// scratchpad delta (imported threads have no scratchpad). It chunks long transcripts, accumulates up
// to importMemoryMaxPerThread, and stores each as an embedded Memory tied to the chat. Best-effort:
// every failure is logged and never fails the rehydration job. Per product design, this seeds the
// user's memory store so an import leaves them with ~10-20 memories per rehydrated thread.
func (a *Agent) extractAndStoreImportedMemories(ctx context.Context, userID, chatID uuid.UUID, msgs []models.ChatMessage) {
	if a.OpenAIProvider == nil || a.memoryTool == nil {
		return
	}
	// Mock/local mode: memory mining is inference + embeddings — deliberately skipped.
	if a.nonVendorLLM() {
		a.logger.Debug("mock/local mode: skipping imported-memory extraction", zap.String("chat_id", chatID.String()))
		return
	}
	if strings.TrimSpace(buildTranscript(msgs)) == "" {
		return
	}

	// Chunk long transcripts so each extraction call stays within budget; accumulate across chunks up
	// to the per-thread cap.
	chunks := chunkMessagesByChars(msgs, rehydrationChunkChars)
	var extracted []models.ExtractedMemory
	for _, chunk := range chunks {
		if len(extracted) >= importMemoryMaxPerThread {
			break
		}
		mems, err := a.extractMemoriesFromTranscript(ctx, userID, buildTranscript(chunk))
		if err != nil {
			a.logger.Warn("imported memory extraction: chunk failed",
				zap.String("chat_id", chatID.String()), zap.Error(err))
			continue
		}
		extracted = append(extracted, mems...)
	}

	memories := memoryutil.NormalizeExtractedMemories(extracted, importMemoryMaxPerThread)

	stored := 0
	for _, mem := range memories {
		embedding, err := a.memoryTool.CreateEmbedding(ctx, mem.Content)
		if err != nil {
			a.logger.Warn("imported memory extraction: embedding failed",
				zap.String("chat_id", chatID.String()), zap.Error(err))
			continue
		}
		// Imported threads have no personality; pass uuid.Nil so no auto-pin is attempted.
		if _, err := a.ds.CreateMemory(ctx, userID, models.Memory{
			ChatID:     chatID,
			Content:    mem.Content,
			Scope:      mem.Scope,
			Confidence: mem.Confidence.Float(),
			Status:     models.MemoryStatusActive,
		}, embedding, uuid.Nil); err != nil {
			a.logger.Warn("imported memory extraction: create memory failed",
				zap.String("chat_id", chatID.String()), zap.Error(err))
			continue
		}
		stored++
	}

	a.logger.Info("imported memory extraction complete",
		zap.String("chat_id", chatID.String()),
		zap.Int("candidates", len(memories)),
		zap.Int("stored", stored))
}

// extractMemoriesFromTranscript runs one structured-JSON memory-extraction call over a plain
// role-tagged transcript using the fixed archival model. Unlike the live path it does not chain off a
// PreviousResponseID; the transcript is the direct input.
func (a *Agent) extractMemoriesFromTranscript(ctx context.Context, userID uuid.UUID, transcript string) ([]models.ExtractedMemory, error) {
	if strings.TrimSpace(transcript) == "" {
		return nil, nil
	}
	params := responses.ResponseNewParams{
		Model:            archivalOpenAIModel,
		SafetyIdentifier: openai.String(userID.String()),
		MaxOutputTokens:  openai.Int(importMemoryExtractMaxTokens),
		ServiceTier:      responses.ResponseNewParamsServiceTierFlex,
		Instructions:     openai.String(importMemoryExtractionInstructions),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(transcript),
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:        "MemoryExtraction",
					Schema:      memoryExtractionSchema,
					Strict:      openai.Bool(true),
					Description: openai.String("Memory Extraction JSON"),
					Type:        "json_schema",
				},
			},
		},
	}
	resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathMemory), params)
	if err != nil {
		return nil, fmt.Errorf("openai memory extraction: %w", err)
	}
	var out models.ExtractedMemoryResponse
	if err := json.Unmarshal([]byte(resp.OutputText()), &out); err != nil {
		return nil, fmt.Errorf("unmarshal memory extraction: %w", err)
	}
	return out.Memories, nil
}

// summarizeText runs a one-shot summarization with the given instructions over input text using the
// fixed archival OpenAI model (the app's primary provider). Returns an error if no OpenAI provider is
// configured; there is no secondary provider for archival summarization.
func (a *Agent) summarizeText(ctx context.Context, userID uuid.UUID, instructions, input string, maxTokens int) (string, error) {
	pathCtx := telemetry.WithCallPath(ctx, telemetry.CallPathConversationSummary)

	if a.OpenAIProvider != nil {
		params := responses.ResponseNewParams{
			Model:            archivalOpenAIModel,
			SafetyIdentifier: openai.String(userID.String()),
			MaxOutputTokens:  openai.Int(int64(maxTokens)),
			ServiceTier:      responses.ResponseNewParamsServiceTierFlex,
			Instructions:     openai.String(instructions),
			Input: responses.ResponseNewParamsInputUnion{
				OfString: openai.String(input),
			},
		}
		resp, err := a.OpenAIProvider.CallWithRetry(pathCtx, params)
		if err != nil {
			return "", fmt.Errorf("openai summarize: %w", err)
		}
		summary := strings.TrimSpace(resp.OutputText())
		if summary == "" {
			return "", fmt.Errorf("openai summarize returned empty summary")
		}
		return summary, nil
	}

	return "", fmt.Errorf("no summarization provider configured")
}
