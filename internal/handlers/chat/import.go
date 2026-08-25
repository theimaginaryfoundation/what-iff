package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	migration "github.com/theimaginaryfoundation/what-iff/internal/chatimport"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const (
	// JobTypeChatImport is the Job.job_type for asynchronous conversation imports.
	JobTypeChatImport = "chat_import"
	// maxChatImportBytes is the hard cap on the conversations.json file size (63 MiB).
	// Per-request limit is ~60 MB; the extra headroom covers multipart overhead. Larger exports are
	// split client-side at conversation boundaries before upload.
	maxChatImportBytes = 63 << 20
	// maxImportErrors is the cap on per-conversation errors logged in one batch; excess are summarized.
	maxImportErrors = 100
	// multipartMemory is the in-memory buffer for ParseMultipartForm. Only form field metadata
	// is kept in memory; the file part is spooled to a temp file, so this can be small.
	multipartMemory = 32 << 10 // 32 KiB
	// importProgressInterval throttles progress DB writes during a large import.
	importProgressInterval = 750 * time.Millisecond
)

// ImportChats handles POST /chat/import — accepts an OpenAI (ChatGPT) or Anthropic (Claude)
// conversations.json export and imports it as archived threads for the current user.
//
// The request body is spooled to a temp file (capped via MaxBytesReader + LimitedReader), the export
// format is sniffed, and the actual parse + persistence run in a detached background job. The handler
// responds 202 with the Job so the client can poll progress (imported/skipped/total) and status.
func (h *Handler) ImportChats(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxChatImportBytes)
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, handlerutils.CodeNotSet, "Import file too large (max 60MB)", nil)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid multipart form (expected multipart/form-data with a 'file' field)", err)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Missing import file", err)
		return
	}
	defer file.Close()

	// Spool to a temp file so the (potentially large) upload outlives the request and can be parsed
	// in the background. LimitedReader caps the file part separately as defense-in-depth (N = limit+1
	// so written > maxChatImportBytes detects overflow even if the body cap is stripped upstream).
	tmp, err := os.CreateTemp("", "chat-import-*.json")
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

	lr := &io.LimitedReader{R: file, N: int64(maxChatImportBytes) + 1}
	written, err := io.Copy(tmp, lr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read import file", err)
		return
	}
	if written > maxChatImportBytes {
		handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, handlerutils.CodeNotSet, "Import file too large (max 60MB)", nil)
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

	// Sniff the format from the staged file so we can route to the right parser in the background.
	detectFile, err := os.Open(tmpPath)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read staged import", err)
		return
	}
	format, derr := detectImportFormat(detectFile)
	_ = detectFile.Close()
	if derr != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Unrecognized export. Upload the conversations.json from an OpenAI or Claude export.", derr)
		return
	}

	initialProgress := marshalImportProgress(models.ImportProgress{Phase: "parsing", Source: format})
	job, err := h.ds.CreateJob(r.Context(), userID, models.Job{
		JobType:   JobTypeChatImport,
		Reference: userID.String(),
		Status:    models.JobStatusPending,
		Progress:  initialProgress,
	})
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to start import", err)
		return
	}

	// Detach from request cancellation: the background job must outlive the HTTP response.
	bgCtx, ok := middleware.CopyUserToIDContext(r.Context(), context.Background())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to start import", nil)
		return
	}

	h.logger.Info("chat import upload staged",
		zap.String("user_id", userID.String()),
		zap.String("job_id", job.ID.String()),
		zap.String("format", format),
		zap.Int64("payload_bytes", written))

	handedOff = true
	go h.runChatImport(bgCtx, userID, job.ID, tmpPath, format)

	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, job)
}

// runChatImport parses the staged export and persists it, updating the job's progress and status.
// It always removes the temp file and never leaves the job in a non-terminal state on panic.
func (h *Handler) runChatImport(ctx context.Context, userID, jobID uuid.UUID, tmpPath, format string) {
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			h.logger.Warn("chat import: failed to remove temp file", zap.String("path", tmpPath), zap.Error(err))
		}
	}()
	defer func() {
		if v := recover(); v != nil {
			h.logger.Error("chat import: panic in background job",
				zap.String("job_id", jobID.String()),
				zap.Any("panic", v),
				zap.ByteString("stack", debug.Stack()))
			h.failImportJob(ctx, userID, jobID, "Import failed unexpectedly")
		}
	}()

	if _, err := h.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusProcessing, ""); err != nil {
		h.logger.Error("chat import: failed to mark job processing", zap.String("job_id", jobID.String()), zap.Error(err))
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		h.failImportJob(ctx, userID, jobID, "Failed to read staged import")
		return
	}
	defer f.Close()

	now := time.Now().UTC()
	var (
		convs       []models.ImportConversation
		parseErrors []string
	)
	switch format {
	case importFormatAnthropic:
		convs, parseErrors, err = parseAnthropicArchive(ctx, f, now)
	default:
		var raw []migration.SimplifiedConversation
		raw, err = migration.SplitConversationArchiveFromReader(ctx, f, migration.ReaderSplitOptions{})
		if err == nil {
			convs, parseErrors = prepareConversations(raw, now)
		}
	}
	if err != nil {
		h.logger.Error("chat import: parse failed",
			zap.String("job_id", jobID.String()), zap.String("format", format), zap.Error(err))
		h.failImportJob(ctx, userID, jobID, "Failed to parse conversations.json")
		return
	}

	total := len(convs)
	h.writeImportProgress(ctx, userID, jobID, models.ImportProgress{Phase: "importing", Source: format, Total: total})

	lastUpdate := time.Now()
	onProgress := func(imported, skipped int) {
		if imported+skipped >= total || time.Since(lastUpdate) >= importProgressInterval {
			lastUpdate = time.Now()
			h.writeImportProgress(ctx, userID, jobID, models.ImportProgress{
				Phase: "importing", Source: format, Total: total, Imported: imported, Skipped: skipped,
			})
		}
	}

	result, err := h.ds.ImportChats(ctx, userID, convs, onProgress)
	if err != nil {
		// result may be partial (e.g. context cancellation); record what we have, then fail.
		h.logger.Error("chat import: datastore error",
			zap.String("job_id", jobID.String()), zap.Error(err))
		h.failImportJob(ctx, userID, jobID, "Failed to import some conversations")
		return
	}

	totalErrors := len(parseErrors) + len(result.Errors)
	if totalErrors > 0 {
		allErrors := append(parseErrors, result.Errors...)
		if len(allErrors) > maxImportErrors {
			allErrors = allErrors[:maxImportErrors]
		}
		h.logger.Warn("chat import: completed with per-conversation errors",
			zap.String("job_id", jobID.String()),
			zap.Int("imported", result.Imported),
			zap.Int("skipped", result.Skipped),
			zap.Int("total_errors", totalErrors),
			zap.Strings("errors", allErrors))
	}

	h.writeImportProgress(ctx, userID, jobID, models.ImportProgress{
		Phase: "complete", Source: format, Total: total,
		Imported: result.Imported, Skipped: result.Skipped, ImportedIDs: result.ImportedIDs,
	})
	if _, err := h.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusComplete, ""); err != nil {
		h.logger.Error("chat import: failed to mark job complete", zap.String("job_id", jobID.String()), zap.Error(err))
	}

	if total > 0 && result.Imported == 0 && result.Skipped == total {
		h.logger.Info("chat import completed: all conversations already imported (dedup)",
			zap.String("user_id", userID.String()),
			zap.String("job_id", jobID.String()),
			zap.Int("skipped", result.Skipped),
			zap.Int("total_errors", totalErrors))
		return
	}

	h.logger.Info("chat import completed",
		zap.String("user_id", userID.String()),
		zap.String("job_id", jobID.String()),
		zap.Int("imported", result.Imported),
		zap.Int("skipped", result.Skipped),
		zap.Int("total_errors", totalErrors))
}

// failImportJob marks the import job failed with a user-safe message, preserving last-known progress.
func (h *Handler) failImportJob(ctx context.Context, userID, jobID uuid.UUID, msg string) {
	if _, err := h.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusFailed, msg); err != nil {
		h.logger.Error("chat import: failed to mark job failed", zap.String("job_id", jobID.String()), zap.Error(err))
	}
}

// writeImportProgress persists a progress snapshot, logging (but not failing) on error.
func (h *Handler) writeImportProgress(ctx context.Context, userID, jobID uuid.UUID, p models.ImportProgress) {
	if err := h.ds.UpdateJobProgress(ctx, userID, jobID, marshalImportProgress(p)); err != nil {
		h.logger.Warn("chat import: failed to update progress", zap.String("job_id", jobID.String()), zap.Error(err))
	}
}

// marshalImportProgress encodes progress to JSON; on the (practically impossible) marshal error it
// returns an empty string so the caller still makes forward progress.
func marshalImportProgress(p models.ImportProgress) string {
	b, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(b)
}

// openaiImportOrigin maps a ChatGPT export author.role to a persistable MessageOrigin.
// ok=false means the message is not imported:
//   - knownSkip=true  for expected non-chat roles (system/tool) — silent drop
//   - knownSkip=false for truly unknown roles — counted and surfaced as a parse warning
func openaiImportOrigin(role string) (origin models.MessageOrigin, ok bool, knownSkip bool) {
	switch role {
	case "user":
		return models.MessageOriginUser, true, false
	case "assistant":
		return models.MessageOriginAssistant, true, false
	case "system", "tool":
		// Common in ChatGPT exports (hidden prompts, browsing/file tool payloads). We only
		// persist the user/assistant transcript; these are expected and must not look like failures.
		return "", false, true
	default:
		return "", false, false
	}
}

// prepareConversations filters and maps raw parsed conversations, returning per-conversation parse
// errors separately. now must be in UTC and is used as the fallback when create_time is absent;
// callers should inject a fixed value in tests for deterministic output.
func prepareConversations(raw []migration.SimplifiedConversation, now time.Time) ([]models.ImportConversation, []string) {
	now = now.UTC() // normalize defensively in case caller passes a non-UTC value
	convs := make([]models.ImportConversation, 0, len(raw))
	var errs []string

	for _, r := range raw {
		title := r.Title
		if title == "" {
			ts := now
			if r.CreateTime != nil {
				ts = floatToTime(*r.CreateTime)
			}
			title = "Imported chat " + ts.Format("2006-01-02 15:04")
		}

		var (
			msgs         []models.ChatMessage
			unknownRoles int
		)
		for _, m := range r.Messages {
			origin, ok, knownSkip := openaiImportOrigin(m.Role)
			if !ok {
				if !knownSkip {
					unknownRoles++
				}
				continue
			}

			if m.Text == "" {
				continue
			}

			sentAt := now
			if m.CreateTime != nil {
				sentAt = floatToTime(*m.CreateTime)
			}

			msgs = append(msgs, models.ChatMessage{
				Message:    m.Text,
				Origin:     origin,
				ReadStatus: models.MessageReadStatusRead,
				SentAt:     sentAt,
			})
		}

		if unknownRoles > 0 {
			errs = append(errs, fmt.Sprintf("conversation %q: dropped %d message(s) with unknown/unsupported roles", models.TruncateImportTitle(title), unknownRoles))
		}

		if len(msgs) == 0 {
			errs = append(errs, fmt.Sprintf("conversation %q: no user/assistant messages after filtering; skipped", models.TruncateImportTitle(title)))
			continue
		}

		if r.ConversationID == "" {
			errs = append(errs, fmt.Sprintf("conversation %q: missing conversation ID; skipped", models.TruncateImportTitle(title)))
			continue
		}

		createdAt := now
		if r.CreateTime != nil {
			createdAt = floatToTime(*r.CreateTime)
		}

		convs = append(convs, models.ImportConversation{
			Title:      title,
			CreatedAt:  createdAt,
			Source:     models.ChatSourceOpenAI,
			ImportHash: conversationHash(r.ConversationID),
			Messages:   msgs,
		})
	}

	return convs, errs
}

// conversationHash returns sha256(conversationID) as a stable hex dedup key.
// Using the export's own conversation ID (not timestamps or message counts) ensures
// the same conversation always hashes identically regardless of when it is imported.
func conversationHash(conversationID string) string {
	h := sha256.New()
	h.Write([]byte(conversationID)) // hash.Hash.Write never returns a non-nil error
	return hex.EncodeToString(h.Sum(nil))
}

// floatToTime converts a Unix float64 timestamp (as used in ChatGPT exports) to a UTC time,
// preserving sub-second precision. math.Modf avoids float subtraction rounding that can produce
// negative nanoseconds. nsec is clamped to [0, 1e9); values at the boundary are biased inward
// by at most one nanosecond — an acceptable trade-off for correctness.
func floatToTime(ts float64) time.Time {
	sec, frac := math.Modf(ts)
	nsec := int64(frac * 1e9)
	if nsec < 0 {
		nsec = 0
	} else if nsec >= 1e9 {
		nsec = 1e9 - 1
	}
	return time.Unix(int64(sec), nsec).UTC()
}
