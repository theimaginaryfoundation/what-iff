package accountexport

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/exporter"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

var (
	errMemoryImportUnavailable = errors.New("memory import unavailable: OpenAI API key is not configured")
	// errDuplicateMemoryArchiveEntry marks malformed nested memory ZIPs with repeated entry names.
	errDuplicateMemoryArchiveEntry = errors.New("duplicate memory archive entry")

	// Selection-validation sentinels, mapped to HTTP status codes by the handler.
	errSelectionTooLarge     = errors.New("selection payload too large")
	errSelectionInvalidJSON  = errors.New("selection is not valid JSON")
	errSelectionTooManyItems = errors.New("selection has too many items")
)

// parseImportSelection parses and bounds the optional multipart `selection` field. It returns
// (nil, nil) when absent (import everything), and a sentinel error otherwise so the caller can map
// it to the right status code. Bounding happens before the archive is validated, so an oversized or
// high-cardinality selection is rejected cheaply.
func parseImportSelection(raw string) (*models.AccountImportSelection, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > maxSelectionBytes {
		return nil, errSelectionTooLarge
	}
	var sel models.AccountImportSelection
	if err := json.Unmarshal([]byte(raw), &sel); err != nil {
		return nil, errSelectionInvalidJSON
	}
	if len(sel.PersonalityIDs) > maxSelectionIDs || len(sel.ConversationIDs) > maxSelectionIDs {
		return nil, errSelectionTooManyItems
	}
	return &sel, nil
}

// ImportAccount stages an export ZIP and enqueues its additive restore. The job outlives the HTTP
// request and records a user-safe AccountImportProgress snapshot for polling clients.
func (h *Handler) ImportAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Space out imports before consuming the upload, so a user can't tie up disk
	// and worker slots with many concurrent uploads. Mirrors the export cooldown.
	recent, err := h.ds.HasRecentAccountImport(r.Context(), userID, time.Now().Add(-importCooldown))
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to check recent import requests", err)
		return
	}
	if recent || !h.importLimiter.allow(userID) {
		handlerutils.RespondWithError(w, h.logger, http.StatusTooManyRequests, handlerutils.CodeNotSet, "An import was requested recently; please wait before requesting another.", nil)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	if err := r.ParseMultipartForm(importMultipartMemory); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, handlerutils.CodeNotSet, "Import file too large (max 100MB)", nil)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid multipart form (expected multipart/form-data with a 'file' field)", err)
		return
	}

	// Optional selection ledger: which personalities/conversations to restore, and whether to
	// include memories. Absent ⇒ import everything (backward-compatible). Parsed before staging so
	// a malformed selection fails fast without consuming disk.
	selection, selErr := parseImportSelection(r.FormValue("selection"))
	if selErr != nil {
		status, msg := http.StatusBadRequest, "Invalid selection (expected JSON)"
		switch {
		case errors.Is(selErr, errSelectionTooLarge):
			status, msg = http.StatusRequestEntityTooLarge, "Selection is too large"
		case errors.Is(selErr, errSelectionTooManyItems):
			msg = "Selection has too many items"
		}
		handlerutils.RespondWithError(w, h.logger, status, handlerutils.CodeNotSet, msg, nil)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Missing import file", err)
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "account-import-*.zip")
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to stage import", err)
		return
	}
	tmpPath := tmp.Name()
	handedOff := false
	defer func() {
		if !handedOff {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()
	written, err := io.Copy(tmp, &io.LimitedReader{R: file, N: int64(maxImportBytes) + 1})
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to stage import", err)
		return
	}
	if written > maxImportBytes {
		handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, handlerutils.CodeNotSet, "Import file too large (max 100MB)", nil)
		return
	}
	if written == 0 {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Import file is empty", nil)
		return
	}
	if err := tmp.Close(); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to stage import", err)
		return
	}
	telemetry.Global().RecordFileSize(r.Context(), telemetry.FileOpAccountImport, telemetry.FileKind("application/zip"), written)

	progress, _ := json.Marshal(models.AccountImportProgress{Phase: "queued", Message: "Import queued."})
	job, err := h.ds.CreateJob(r.Context(), userID, models.Job{
		JobType:   models.JobTypeAccountImport,
		Reference: userID.String(),
		Status:    models.JobStatusPending,
		Progress:  string(progress),
	})
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to start import", err)
		return
	}

	handedOff = true
	go h.runAccountImport(userID, job.ID, tmpPath, selection)
	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, job)
}

// runAccountImport owns the staged archive after the response returns. It serializes expensive
// restores per API process and always removes the temporary file.
//
// Metrics: the wait for an import slot is recorded on JobQueueWait, the run itself is tracked as an
// account_import job, and each phase is timed on FileOperationDuration. Memory item counts come
// from the datastore memory importer (operation memory_import), so they aren't repeated here.
func (h *Handler) runAccountImport(userID, jobID uuid.UUID, tmpPath string, selection *models.AccountImportSelection) {
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			h.logger.Warn("account import: failed to remove temp file", zap.String("path", tmpPath), zap.Error(err))
		}
	}()
	// finishJob is set once the job holds a slot. Its defer is registered before the recover below
	// so it runs after it and sees a panic outcome; every early return is a failure.
	metrics := telemetry.Global()
	var finishJob func(outcome string)
	outcome := telemetry.JobOutcomeFailed
	defer func() {
		if finishJob != nil {
			finishJob(outcome)
		}
	}()
	defer func() {
		if v := recover(); v != nil {
			outcome = telemetry.JobOutcomePanic
			h.logger.Error("account import: panic in background job",
				zap.String("job_id", jobID.String()), zap.Any("panic", v), zap.ByteString("stack", debug.Stack()))
			h.failAccountImport(context.Background(), userID, jobID, "Import failed unexpectedly", nil)
		}
	}()

	release := h.acquireImportSlot(context.Background())
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), importJobTimeout)
	defer cancel()
	finishJob = metrics.TrackJob(ctx, models.JobTypeAccountImport)
	doneValidate := metrics.TimeFileStage(ctx, telemetry.FileOpAccountImport, telemetry.FileStageValidate)
	validated := false
	defer func() {
		if !validated {
			doneValidate(errAccountImportInvalid)
		}
	}()
	if _, err := h.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusProcessing, ""); err != nil {
		h.logger.Warn("account import: failed to mark processing", zap.String("job_id", jobID.String()), zap.Error(err))
	}
	h.writeAccountImportProgress(ctx, userID, jobID, models.AccountImportProgress{Phase: "validating", Message: "Validating account export."})

	f, err := os.Open(tmpPath)
	if err != nil {
		h.failAccountImport(ctx, userID, jobID, "Failed to read staged import", nil)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		h.failAccountImport(ctx, userID, jobID, "Failed to read staged import", nil)
		return
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		h.failAccountImport(ctx, userID, jobID, "Import file is not a valid export ZIP", nil)
		return
	}
	if err := validateImportArchive(zr); err != nil {
		h.failAccountImport(ctx, userID, jobID, "Import file exceeds safety limits", nil)
		return
	}
	manifestBytes, found, err := readZipEntry(zr, "manifest.json", maxImportExpandedBytes)
	if err != nil {
		h.failAccountImport(ctx, userID, jobID, "Import manifest could not be read", nil)
		return
	}
	if !found {
		h.failAccountImport(ctx, userID, jobID, "Import file is missing manifest.json", nil)
		return
	}
	var manifest exporter.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		h.failAccountImport(ctx, userID, jobID, "Import manifest is invalid", nil)
		return
	}
	if manifest.SchemaVersion != exporter.SchemaVersion {
		h.failAccountImport(ctx, userID, jobID, "Unsupported account export version", nil)
		return
	}
	validated = true
	doneValidate(nil)

	result := models.AccountImportResult{}
	h.writeAccountImportProgress(ctx, userID, jobID, models.AccountImportProgress{Phase: "importing", Message: "Importing personalities."})

	// Personalities must precede conversations and memories: account exports carry source
	// personality IDs in both places, while every imported account receives fresh destination IDs.
	donePersonalities := metrics.TimeFileStage(ctx, telemetry.FileOpAccountImport, telemetry.FileStagePersonalities)
	personalityCounts, personalityIDs := h.importPersonalities(ctx, userID, zr, selection)
	donePersonalities(nil) // a personality that fails is logged and left out; the phase goes on
	result.Personalities = personalityCounts
	h.writeAccountImportProgress(ctx, userID, jobID, progressForAccountImport("importing", "Importing conversations.", result))

	// Conversations — reuse the same importer as /chat/import, with Whatiff's additional state.
	chatIDs := make(map[uuid.UUID]uuid.UUID)
	if cb, ok, readErr := readZipEntry(zr, "conversations.json", maxImportExpandedBytes); readErr != nil {
		result.Warnings = append(result.Warnings, "Conversations could not be read from the export.")
	} else if ok {
		parsed, perr := exporter.ParseConversations(cb)
		if perr != nil {
			h.logger.Warn("account import: conversations.json parse failed", zap.Error(perr))
			result.Warnings = append(result.Warnings, "Conversations could not be imported from the export.")
		} else {
			if selection != nil {
				parsed = filterSelectedConversations(parsed, selection.ConversationIDs)
			}
			convs := toImportConversations(parsed, personalityIDs)
			doneConversations := metrics.TimeFileStage(ctx, telemetry.FileOpAccountImport, telemetry.FileStageConversations)
			res, ierr := h.ds.ImportChats(ctx, userID, convs, nil)
			doneConversations(ierr)
			if ierr != nil {
				h.logger.Error("account import: conversation import failed", zap.Error(ierr))
				result.Warnings = append(result.Warnings, "Conversations could not be imported.")
			} else if res != nil {
				result.Conversations = *res
				hashes := make([]string, 0, len(parsed))
				for _, c := range parsed {
					if strings.TrimSpace(c.UUID) != "" {
						hashes = append(hashes, conversationImportHash(c.UUID))
					}
				}
				destinations, mapErr := h.ds.ImportedChatIDsByHash(ctx, userID, hashes)
				if mapErr != nil {
					h.logger.Warn("account import: could not resolve imported conversation IDs", zap.Error(mapErr))
					result.Warnings = append(result.Warnings, "Conversation-linked memories could not be restored.")
				} else {
					for _, c := range parsed {
						if id, found := destinations[conversationImportHash(c.UUID)]; found {
							if sourceID, parseErr := uuid.Parse(c.UUID); parseErr == nil {
								chatIDs[sourceID] = id
							}
						}
					}
					h.writeAccountImportProgress(ctx, userID, jobID, progressForAccountImport("importing", "Indexing thread summaries.", result))
					doneSummaries := metrics.TimeFileStage(ctx, telemetry.FileOpAccountImport, telemetry.FileStageSummaries)
					partial := h.importConversationSummaryMemories(ctx, userID, parsed, chatIDs)
					doneSummaries(nil) // best effort: a partial result is a warning, not a failed phase
					if partial {
						result.Warnings = append(result.Warnings, "Some thread summaries could not be indexed for search.")
					}
				}
			}
		}
	} else {
		result.Warnings = append(result.Warnings, "The export did not contain conversations.")
	}

	// Memories — nested memories.zip through the existing memory importer (needs embeddings).
	// Skipped entirely when the selection opts out (memories are a single all-or-nothing toggle).
	if selection == nil || selection.IncludeMemories {
		h.writeAccountImportProgress(ctx, userID, jobID, progressForAccountImport("importing", "Importing memories.", result))
		if mb, ok, readErr := readZipEntry(zr, "memories.zip", maxImportExpandedBytes); readErr != nil {
			result.Warnings = append(result.Warnings, "Memories could not be read from the export.")
		} else if ok {
			if h.oaiClient == nil {
				h.logger.Warn("account import: skipping memories (OpenAI key not configured)")
				result.Warnings = append(result.Warnings, "Memories were skipped because embedding generation is unavailable.")
			} else if mzr, zerr := zip.NewReader(bytes.NewReader(mb), int64(len(mb))); zerr != nil {
				h.logger.Warn("account import: memories.zip is not a valid ZIP", zap.Error(zerr))
				result.Warnings = append(result.Warnings, "Memories could not be imported from the export.")
			} else if zerr := validateImportArchive(mzr); zerr != nil {
				h.logger.Warn("account import: memories.zip exceeds safety limits", zap.Error(zerr))
				result.Warnings = append(result.Warnings, "Memories could not be imported because the archive exceeds safety limits.")
			} else {
				resolveNative := func(ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
					return h.ds.MemoryIDsOwnedByUser(ctx, userID, ids)
				}
				remapped, remapErr := remapMemoryArchive(mzr, userID, chatIDs, personalityIDs, resolveNative)
				if remapErr != nil {
					h.logger.Warn("account import: could not remap memory references", zap.Error(remapErr))
					result.Warnings = append(result.Warnings, "Memories could not be imported from the export.")
				} else {
					mzr, zerr = zip.NewReader(bytes.NewReader(remapped), int64(len(remapped)))
					if zerr != nil {
						h.logger.Warn("account import: remapped memory archive is invalid", zap.Error(zerr))
						result.Warnings = append(result.Warnings, "Memories could not be imported from the export.")
					} else {
						// The importer returns a partial result on error; keep it either way.
						// Batch embeddings avoid one remote request per memory for large account restores.
						doneMemories := metrics.TimeFileStage(ctx, telemetry.FileOpAccountImport, telemetry.FileStageMemories)
						res, merr := h.ds.ImportMemoriesWithBatchEmbeddings(ctx, userID, mzr, h.createEmbedding, h.createEmbeddings)
						doneMemories(merr)
						if merr != nil {
							h.logger.Error("account import: memory import failed", zap.Error(merr))
							result.Warnings = append(result.Warnings, "Some memories could not be imported.")
						}
						result.Memories = res
					}
				}
			}
		} else {
			result.Warnings = append(result.Warnings, "The export did not contain memories.")
		}
	}

	recordAccountImportItems(ctx, metrics, result)
	if err := ctx.Err(); err != nil {
		outcome = telemetry.JobOutcomeFromError(err)
		// The import context was cancelled before completion (a timeout, or something aborting the
		// run — memory embedding is the usual long pole). Mark it failed on a fresh context so the
		// job reaches a terminal state and shows on the activity log with what did land, instead of
		// silently staying "processing".
		freshCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		h.failAccountImport(freshCtx, userID, jobID, "Import was interrupted before completing; some memories or thread summaries may not have imported", &result)
		return
	}

	h.logger.Info("account import complete",
		zap.String("user_id", userID.String()),
		zap.Int("conversations_imported", result.Conversations.Imported),
		zap.Int("memories_imported", result.Memories.ImportedCount),
		zap.Int("personalities_created", result.Personalities.Created))
	outcome = telemetry.JobOutcomeSuccess
	h.writeAccountImportProgress(ctx, userID, jobID, progressForAccountImport("complete", "Account import complete.", result))
	if _, err := h.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusComplete, ""); err != nil {
		h.logger.Warn("account import: failed to mark complete", zap.String("job_id", jobID.String()), zap.Error(err))
	}
	h.ds.AuditAccountImport(ctx, userID, "imported", "user imported account export", map[string]any{
		"conversations_imported": result.Conversations.Imported,
		"memories_imported":      result.Memories.ImportedCount,
		"personalities_created":  result.Personalities.Created,
		"warnings":               len(result.Warnings),
	})
}

// errAccountImportInvalid labels a failed validate phase on FileOperationDuration; the specific
// reason is in the job's user-facing message.
var errAccountImportInvalid = errors.New("account import archive rejected")

// acquireImportSlot waits for one of the per-process import slots, recording the wait on
// JobQueueWait (the only place in the API where background work queues), and returns the
// function that frees the slot.
func (h *Handler) acquireImportSlot(ctx context.Context) (release func()) {
	start := time.Now()
	h.imports <- struct{}{}
	telemetry.Global().RecordDuration(ctx, telemetry.JobQueueWait, time.Since(start),
		telemetry.AttrJobType.String(models.JobTypeAccountImport))
	return func() { <-h.imports }
}

// recordAccountImportItems records the conversation and personality counts of one import run.
func recordAccountImportItems(ctx context.Context, metrics *telemetry.Metrics, result models.AccountImportResult) {
	op := telemetry.FileOpAccountImport
	metrics.RecordFileItems(ctx, op, telemetry.FileItemConversation, telemetry.FileItemImported, result.Conversations.Imported)
	metrics.RecordFileItems(ctx, op, telemetry.FileItemConversation, telemetry.FileItemSkipped, result.Conversations.Skipped)
	metrics.RecordFileItems(ctx, op, telemetry.FileItemConversation, telemetry.FileItemFailed, len(result.Conversations.Errors))
	metrics.RecordFileItems(ctx, op, telemetry.FileItemPersonality, telemetry.FileItemImported, result.Personalities.Created)
	metrics.RecordFileItems(ctx, op, telemetry.FileItemPersonality, telemetry.FileItemSkipped, result.Personalities.Skipped)
}

func progressForAccountImport(phase, message string, result models.AccountImportResult) models.AccountImportProgress {
	return models.AccountImportProgress{
		Phase: phase, Message: message, Counts: accountImportCounts(result), Conversations: result.Conversations, Memories: result.Memories,
		Personalities: result.Personalities, Warnings: result.Warnings,
		Result: &result,
	}
}

func accountImportCounts(result models.AccountImportResult) map[string]int {
	return map[string]int{
		"conversations_imported": result.Conversations.Imported,
		"conversations_skipped":  result.Conversations.Skipped,
		"personalities_created":  result.Personalities.Created,
		"personalities_skipped":  result.Personalities.Skipped,
		"memories_imported":      result.Memories.ImportedCount,
		"memories_skipped":       result.Memories.DuplicateCount,
	}
}

const (
	// summaryEmbedBatchSize bounds one OpenAI embeddings request during summary indexing (mirrors
	// memoryImportBatchSize). One request per conversation was the P1 timeout risk.
	summaryEmbedBatchSize = 200
	// maxSummariesIndexed caps how many exported summaries we index in a single import so a very
	// large restore cannot spend most of the import window here. The summaries themselves are already
	// restored on the chats (durable); only their best-effort search index is bounded.
	maxSummariesIndexed = 2000
)

// importConversationSummaryMemories restores exported checkpoint summaries as internal Summary
// memories so find_context can retrieve them after an account import. The Chat checkpoint summary
// is already durable and authoritative; this indexing work is deliberately best-effort, matching
// live checkpoint creation and thread rehydration.
//
// Embeddings are generated in bounded batches (not one request per conversation) and the total is
// capped, so a large restore cannot exhaust the import window on summary indexing. Each summary is
// upserted in its own transaction, so a partial failure never corrupts the ones that succeeded.
//
// It returns true when at least one eligible summary could not be indexed (including any dropped by
// the cap or an early cancellation), so the caller can surface a best-effort partial-result warning.
func (h *Handler) importConversationSummaryMemories(ctx context.Context, userID uuid.UUID, parsed []exporter.ParsedConversation, chatIDs map[uuid.UUID]uuid.UUID) bool {
	return indexSummaryMemories(ctx, userID, summaryImportCandidates(parsed, chatIDs),
		h.createEmbeddings, h.ds.UpsertChatSummaryMemory, h.logger)
}

// indexSummaryMemories embeds and upserts the given summary candidates in bounded batches, capped at
// maxSummariesIndexed, stopping cleanly on context cancellation. The embed/upsert dependencies are
// injected so the batching, cap, and cancellation behaviour are unit-testable without OpenAI or a DB.
// Returns true when at least one candidate was not indexed (batch/upsert failure, the cap, or an
// early cancellation) so the caller can surface a best-effort partial-result warning.
func indexSummaryMemories(
	ctx context.Context,
	userID uuid.UUID,
	candidates []summaryImportCandidate,
	embed func(context.Context, []string) ([][]float32, error),
	upsert func(context.Context, uuid.UUID, uuid.UUID, string, []float32) error,
	logger *zap.Logger,
) bool {
	hadFailure := false
	if len(candidates) > maxSummariesIndexed {
		candidates = candidates[:maxSummariesIndexed]
		hadFailure = true // the remainder is intentionally not indexed
	}

	for start := 0; start < len(candidates); start += summaryEmbedBatchSize {
		if ctx.Err() != nil {
			return true // stop cleanly on cancellation; the caller reports the partial result
		}
		batch := candidates[start:min(start+summaryEmbedBatchSize, len(candidates))]

		inputs := make([]string, len(batch))
		for i, c := range batch {
			inputs[i] = c.summary
		}
		vectors, err := embed(ctx, inputs)
		if err != nil || len(vectors) != len(batch) {
			logger.Warn("account import: summary embedding batch failed",
				zap.Int("batch_size", len(batch)), zap.Error(err))
			hadFailure = true
			continue
		}
		for i, c := range batch {
			if err := upsert(ctx, userID, c.chatID, c.summary, vectors[i]); err != nil {
				logger.Warn("account import: summary memory upsert failed",
					zap.String("chat_id", c.chatID.String()), zap.Error(err))
				hadFailure = true
			}
		}
	}
	return hadFailure
}

type summaryImportCandidate struct {
	chatID  uuid.UUID
	summary string
}

// summaryImportCandidates selects only non-empty exported summaries whose source conversation was
// resolved to a destination chat. Invalid source IDs and deduped native chats with no import hash
// have no safe mapping and are ignored.
func summaryImportCandidates(parsed []exporter.ParsedConversation, chatIDs map[uuid.UUID]uuid.UUID) []summaryImportCandidate {
	candidates := make([]summaryImportCandidate, 0)
	for _, conversation := range parsed {
		sourceID, err := uuid.Parse(conversation.UUID)
		if err != nil {
			continue
		}
		chatID, found := chatIDs[sourceID]
		summary := strings.TrimSpace(conversation.WhatiffCheckpointSummary)
		if !found || summary == "" {
			continue
		}
		candidates = append(candidates, summaryImportCandidate{chatID: chatID, summary: summary})
	}
	return candidates
}

func (h *Handler) writeAccountImportProgress(ctx context.Context, userID, jobID uuid.UUID, progress models.AccountImportProgress) {
	data, err := json.Marshal(progress)
	if err == nil {
		if err := h.ds.UpdateJobProgress(ctx, userID, jobID, string(data)); err != nil {
			h.logger.Warn("account import: failed to update progress", zap.String("job_id", jobID.String()), zap.Error(err))
		}
	}
}

func (h *Handler) failAccountImport(ctx context.Context, userID, jobID uuid.UUID, message string, result *models.AccountImportResult) {
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}
	progress := models.AccountImportProgress{Phase: "failed", Message: message, Result: result}
	if result != nil {
		progress.Counts = accountImportCounts(*result)
		progress.Conversations = result.Conversations
		progress.Memories = result.Memories
		progress.Personalities = result.Personalities
		progress.Warnings = result.Warnings
	}
	h.writeAccountImportProgress(ctx, userID, jobID, progress)
	if _, err := h.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusFailed, message); err != nil {
		h.logger.Warn("account import: failed to mark failed", zap.String("job_id", jobID.String()), zap.Error(err))
	}
	meta := map[string]any{"success": false, "message": message}
	if result != nil {
		meta["conversations_imported"] = result.Conversations.Imported
		meta["memories_imported"] = result.Memories.ImportedCount
		meta["personalities_created"] = result.Personalities.Created
		meta["warnings"] = len(result.Warnings)
	}
	h.ds.AuditAccountImport(ctx, userID, "import_failed", "account import failed: "+message, meta)
}

// importPersonalities creates each exported personality that does not already exist (by name) for the
// target user, with a fresh ID. It returns the source-to-destination map needed to reconnect
// conversations and pinned memories even when a personality was already present in the account.
func (h *Handler) importPersonalities(ctx context.Context, userID uuid.UUID, zr *zip.Reader, selection *models.AccountImportSelection) (models.SectionImportCounts, map[uuid.UUID]uuid.UUID) {
	var counts models.SectionImportCounts
	ids := make(map[uuid.UUID]uuid.UUID)

	// When a selection is present, only its listed personalities are restored. Unselected ones are
	// silently skipped (not counted as "skipped" — the user chose not to import them).
	var selected map[uuid.UUID]struct{}
	if selection != nil {
		selected = make(map[uuid.UUID]struct{}, len(selection.PersonalityIDs))
		for _, id := range selection.PersonalityIDs {
			selected[id] = struct{}{}
		}
	}

	existing, err := h.ds.ExportPersonalityInputs(ctx, userID)
	if err != nil {
		h.logger.Warn("account import: could not list existing personalities; proceeding", zap.Error(err))
	}
	seen := make(map[string]uuid.UUID, len(existing))
	for _, p := range existing {
		seen[normalizeName(p.Name)] = p.ID
	}

	suffix := "/" + exporter.PersonalityFileName
	for _, zf := range zr.File {
		if !strings.HasPrefix(zf.Name, "personalities/") || !strings.HasSuffix(zf.Name, suffix) {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			h.logger.Warn("account import: open personality entry failed", zap.String("entry", zf.Name), zap.Error(err))
			continue
		}
		var pf exporter.PersonalityFile
		derr := json.NewDecoder(rc).Decode(&pf)
		rc.Close()
		if derr != nil {
			h.logger.Warn("account import: decode personality failed", zap.String("entry", zf.Name), zap.Error(derr))
			continue
		}
		sourceID := pf.ID
		if sourceID == uuid.Nil {
			sourceID = personalityIDFromArchivePath(zf.Name)
		}

		if selected != nil {
			if _, want := selected[sourceID]; !want {
				continue
			}
		}

		name := strings.TrimSpace(pf.Name)
		if name == "" || strings.TrimSpace(pf.SystemPrompt) == "" {
			counts.Skipped++
			continue
		}
		if existingID, dup := seen[normalizeName(name)]; dup {
			if sourceID != uuid.Nil {
				ids[sourceID] = existingID
			}
			counts.Skipped++
			continue
		}

		created, err := h.ds.CreatePersonality(ctx, userID, models.Personality{
			Name:            name,
			SystemPrompt:    pf.SystemPrompt,
			Scratchpad:      pf.Scratchpad,
			AutoPinMemories: pf.AutoPinMemories,
		})
		if err != nil {
			h.logger.Warn("account import: create personality failed", zap.String("name", name), zap.Error(err))
			counts.Skipped++
			continue
		}
		seen[normalizeName(name)] = created.ID
		if sourceID != uuid.Nil {
			ids[sourceID] = created.ID
		}
		counts.Created++
	}
	return counts, ids
}

// filterSelectedConversations keeps only the parsed conversations whose source uuid is listed in
// the selection. Entries with an unparseable uuid are dropped when a selection is active.
func filterSelectedConversations(parsed []exporter.ParsedConversation, ids []uuid.UUID) []exporter.ParsedConversation {
	want := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	out := make([]exporter.ParsedConversation, 0, len(want))
	for _, c := range parsed {
		id, err := uuid.Parse(strings.TrimSpace(c.UUID))
		if err != nil {
			continue
		}
		if _, ok := want[id]; ok {
			out = append(out, c)
		}
	}
	return out
}

// toImportConversations maps decoded conversations.json entries to the datastore import model,
// computing the dedup hash from the exported uuid (matches the /chat/import convention).
func toImportConversations(parsed []exporter.ParsedConversation, personalityIDs map[uuid.UUID]uuid.UUID) []models.ImportConversation {
	now := time.Now().UTC()
	out := make([]models.ImportConversation, 0, len(parsed))
	for _, c := range parsed {
		if strings.TrimSpace(c.UUID) == "" {
			continue
		}
		msgs := make([]models.ChatMessage, 0, len(c.Messages))
		for _, m := range c.Messages {
			origin, ok := senderToOrigin(m.Sender)
			if !ok || m.Text == "" {
				continue
			}
			sentAt := m.CreatedAt
			if sentAt.IsZero() {
				sentAt = now
			}
			msgs = append(msgs, models.ChatMessage{
				Message:    m.Text,
				Origin:     origin,
				ReadStatus: models.MessageReadStatusRead,
				SentAt:     sentAt.UTC(),
			})
		}
		if len(msgs) == 0 {
			continue
		}
		title := c.Name
		createdAt := c.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		if strings.TrimSpace(title) == "" {
			title = "Imported chat " + createdAt.Format("2006-01-02 15:04")
		}
		var personalityID *uuid.UUID
		if c.WhatiffPersonalityID != nil {
			if destinationID, found := personalityIDs[*c.WhatiffPersonalityID]; found {
				personalityID = &destinationID
			}
		}
		// The export's uuid is the source chat's own id. Carrying it lets the importer skip a
		// round-trip into the origin account (where a native chat with this id already exists but
		// has no import_hash to match). A non-uuid source id simply leaves this nil.
		var sourceID *uuid.UUID
		if parsed, perr := uuid.Parse(c.UUID); perr == nil {
			sourceID = &parsed
		}
		out = append(out, models.ImportConversation{
			Title:                      title,
			CreatedAt:                  createdAt.UTC(),
			Source:                     models.ChatSourceAnthropic,
			ImportHash:                 conversationImportHash(c.UUID),
			SourceID:                   sourceID,
			Messages:                   msgs,
			PersonalityID:              personalityID,
			CheckpointSummary:          c.WhatiffCheckpointSummary,
			CheckpointUserMessageCount: c.WhatiffCheckpointUserMessageCnt,
			LastCheckpointAt:           c.WhatiffLastCheckpointAt,
			DisabledTools:              c.WhatiffDisabledTools,
			Tags:                       c.WhatiffTags,
			IsFavorite:                 c.WhatiffIsFavorite,
			IsAutoMood:                 c.WhatiffIsAutoMood,
			AccountExport:              true,
			RestoreReady:               strings.TrimSpace(c.WhatiffCheckpointSummary) != "",
		})
	}
	return out
}

func senderToOrigin(sender string) (models.MessageOrigin, bool) {
	switch sender {
	case "human":
		return models.MessageOriginUser, true
	case "assistant":
		return models.MessageOriginAssistant, true
	default:
		return "", false
	}
}

// conversationImportHash mirrors the /chat/import dedup key: sha256 of the source conversation id.
func conversationImportHash(uuid string) string {
	sum := sha256.Sum256([]byte(uuid))
	return hex.EncodeToString(sum[:])
}

func normalizeName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func personalityIDFromArchivePath(name string) uuid.UUID {
	id, err := uuid.Parse(path.Base(path.Dir(name)))
	if err != nil {
		return uuid.Nil
	}
	return id
}

// isMemoryRecordEntry reports whether an archive entry name holds newline-delimited MemoryRecords
// (as opposed to, e.g., a manifest). Both id collection and remapping key off the same predicate.
func isMemoryRecordEntry(name string) bool {
	return name == "chat.json" || name == "user.json" ||
		(strings.HasPrefix(name, "personality-") && strings.HasSuffix(name, ".json"))
}

// memoryRecordIDs extracts the source memory ids from a record entry, skipping malformed lines
// (the generic importer owns invalid-record accounting). Used to resolve which exported memories
// are already the target user's own before deciding how to key each one.
func memoryRecordIDs(data []byte) []uuid.UUID {
	var ids []uuid.UUID
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		var record models.MemoryRecord
		if err := json.Unmarshal(trimmed, &record); err != nil {
			continue
		}
		ids = append(ids, record.ID)
	}
	return ids
}

// remapMemoryArchive rewrites account-export relationships to target-account IDs before the
// generic memory importer validates ownership. Source memory IDs are rekeyed per target account
// because Memory.ID is globally unique; chat references and personality section filenames change.
// It rejects duplicate names, including names that collide after personality-ID remapping.
//
// resolveNative reports which source memory ids already belong to the target user; those keep their
// original id so the importer's id-dedup skips them (an idempotent round-trip into the origin
// account) instead of creating a namespaced duplicate of a memory the user already has.
func remapMemoryArchive(zr *zip.Reader, targetUserID uuid.UUID, chatIDs, personalityIDs map[uuid.UUID]uuid.UUID, resolveNative func([]uuid.UUID) (map[uuid.UUID]struct{}, error)) ([]byte, error) {
	// Pass 1: read every entry and collect the source memory ids across all record entries.
	type archiveEntry struct {
		name string
		data []byte
	}
	raw := make([]archiveEntry, 0, len(zr.File))
	var allIDs []uuid.UUID
	for _, zf := range zr.File {
		data, err := readZipFile(zf, maxImportExpandedBytes)
		if err != nil {
			return nil, err
		}
		raw = append(raw, archiveEntry{name: zf.Name, data: data})
		if isMemoryRecordEntry(zf.Name) {
			allIDs = append(allIDs, memoryRecordIDs(data)...)
		}
	}

	native := map[uuid.UUID]struct{}{}
	if resolveNative != nil {
		resolved, err := resolveNative(allIDs)
		if err != nil {
			return nil, fmt.Errorf("resolve native memory ids: %w", err)
		}
		if resolved != nil {
			native = resolved
		}
	}

	// Pass 2: remap record ids/chat references and rename personality section files.
	entries := make(map[string][]byte, len(zr.File))
	for _, entry := range raw {
		name := entry.name
		data := entry.data
		if isMemoryRecordEntry(name) {
			var err error
			data, err = remapMemoryRecords(data, targetUserID, chatIDs, native)
			if err != nil {
				return nil, err
			}
		}
		if strings.HasPrefix(name, "personality-") && strings.HasSuffix(name, ".json") {
			source := strings.TrimSuffix(strings.TrimPrefix(name, "personality-"), ".json")
			if sourceID, parseErr := uuid.Parse(source); parseErr == nil {
				if destinationID, found := personalityIDs[sourceID]; found {
					name = "personality-" + destinationID.String() + ".json"
				}
			}
		}
		if _, duplicate := entries[name]; duplicate {
			return nil, fmt.Errorf("%w %q", errDuplicateMemoryArchiveEntry, name)
		}
		entries[name] = data
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(entries[name]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// remapMemoryRecords rewrites the chat references and ids of every record in one archive entry.
// nativeIDs is the set (from remapMemoryArchive's resolveNative) of source memory ids the target
// user already owns; a nil or empty set simply namespaces every record. Reading a nil map is safe
// in Go, so the nil case needs no special handling here.
func remapMemoryRecords(data []byte, targetUserID uuid.UUID, chatIDs map[uuid.UUID]uuid.UUID, nativeIDs map[uuid.UUID]struct{}) ([]byte, error) {
	var out bytes.Buffer
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		var record models.MemoryRecord
		if err := json.Unmarshal(trimmed, &record); err != nil {
			// Preserve malformed lines: the generic importer owns invalid-record accounting.
			out.Write(line)
			continue
		}
		if record.ChatID != nil {
			if destinationID, found := chatIDs[*record.ChatID]; found {
				record.ChatID = &destinationID
			}
		}
		// Memory IDs are global primary keys. A memory that already belongs to the target user
		// (an import back into the origin account) keeps its original id so the importer's id-dedup
		// treats it as an existing duplicate and skips it. Otherwise the id is derived from the
		// target user and source id, which keeps a repeated import idempotent for that account while
		// letting the same ZIP restore into another account without a primary-key collision.
		if _, isNative := nativeIDs[record.ID]; !isNative {
			record.ID = uuid.NewSHA1(targetUserID, record.ID[:])
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		out.Write(encoded)
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

// readZipEntry returns the bytes of the named archive entry (exact path match) and whether it exists.
func validateImportArchive(zr *zip.Reader) error {
	if len(zr.File) > maxImportEntries {
		return fmt.Errorf("archive has %d entries (limit %d)", len(zr.File), maxImportEntries)
	}
	var expanded uint64
	for _, zf := range zr.File {
		if zf.UncompressedSize64 > uint64(maxImportExpandedBytes) {
			return fmt.Errorf("archive entry %q exceeds expanded-size limit", zf.Name)
		}
		expanded += zf.UncompressedSize64
		if expanded > uint64(maxImportExpandedBytes) {
			return fmt.Errorf("archive exceeds expanded-size limit")
		}
	}
	return nil
}

func readZipEntry(zr *zip.Reader, name string, maxBytes int) ([]byte, bool, error) {
	want := path.Clean(name)
	for _, zf := range zr.File {
		if path.Clean(zf.Name) != want {
			continue
		}
		data, err := readZipFile(zf, maxBytes)
		return data, true, err
	}
	return nil, false, nil
}

func readZipFile(zf *zip.File, maxBytes int) ([]byte, error) {
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("archive entry %q exceeds expanded-size limit", zf.Name)
	}
	return data, nil
}
