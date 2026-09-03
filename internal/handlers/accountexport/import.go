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
)

var (
	errMemoryImportUnavailable = errors.New("memory import unavailable: OpenAI API key is not configured")
	// errDuplicateMemoryArchiveEntry marks malformed nested memory ZIPs with repeated entry names.
	errDuplicateMemoryArchiveEntry = errors.New("duplicate memory archive entry")
)

// ImportAccount stages an export ZIP and enqueues its additive restore. The job outlives the HTTP
// request and records a user-safe AccountImportProgress snapshot for polling clients.
func (h *Handler) ImportAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
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
	go h.runAccountImport(userID, job.ID, tmpPath)
	handlerutils.RespondWithJSON(w, h.logger, http.StatusAccepted, job)
}

// runAccountImport owns the staged archive after the response returns. It serializes expensive
// restores per API process and always removes the temporary file.
func (h *Handler) runAccountImport(userID, jobID uuid.UUID, tmpPath string) {
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			h.logger.Warn("account import: failed to remove temp file", zap.String("path", tmpPath), zap.Error(err))
		}
	}()
	defer func() {
		if v := recover(); v != nil {
			h.logger.Error("account import: panic in background job",
				zap.String("job_id", jobID.String()), zap.Any("panic", v), zap.ByteString("stack", debug.Stack()))
			h.failAccountImport(context.Background(), userID, jobID, "Import failed unexpectedly", nil)
		}
	}()

	h.imports <- struct{}{}
	defer func() { <-h.imports }()
	ctx, cancel := context.WithTimeout(context.Background(), importJobTimeout)
	defer cancel()
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

	result := models.AccountImportResult{}
	h.writeAccountImportProgress(ctx, userID, jobID, models.AccountImportProgress{Phase: "importing", Message: "Importing personalities."})

	// Personalities must precede conversations and memories: account exports carry source
	// personality IDs in both places, while every imported account receives fresh destination IDs.
	personalityCounts, personalityIDs := h.importPersonalities(ctx, userID, zr)
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
			convs := toImportConversations(parsed, personalityIDs)
			if res, ierr := h.ds.ImportChats(ctx, userID, convs, nil); ierr != nil {
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
				}
			}
		}
	} else {
		result.Warnings = append(result.Warnings, "The export did not contain conversations.")
	}

	h.writeAccountImportProgress(ctx, userID, jobID, progressForAccountImport("importing", "Importing memories.", result))
	// Memories — nested memories.zip through the existing memory importer (needs embeddings).
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
			remapped, remapErr := remapMemoryArchive(mzr, userID, chatIDs, personalityIDs)
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
					res, merr := h.ds.ImportMemoriesWithBatchEmbeddings(ctx, userID, mzr, h.createEmbedding, h.createEmbeddings)
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

	h.logger.Info("account import complete",
		zap.String("user_id", userID.String()),
		zap.Int("conversations_imported", result.Conversations.Imported),
		zap.Int("memories_imported", result.Memories.ImportedCount),
		zap.Int("personalities_created", result.Personalities.Created))
	h.writeAccountImportProgress(ctx, userID, jobID, progressForAccountImport("complete", "Account import complete.", result))
	if _, err := h.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusComplete, ""); err != nil {
		h.logger.Warn("account import: failed to mark complete", zap.String("job_id", jobID.String()), zap.Error(err))
	}
	h.ds.AuditAccountExport(ctx, userID, "imported", "user imported account export", map[string]any{
		"conversations_imported": result.Conversations.Imported,
		"memories_imported":      result.Memories.ImportedCount,
		"personalities_created":  result.Personalities.Created,
		"warnings":               len(result.Warnings),
	})
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
}

// importPersonalities creates each exported personality that does not already exist (by name) for the
// target user, with a fresh ID. It returns the source-to-destination map needed to reconnect
// conversations and pinned memories even when a personality was already present in the account.
func (h *Handler) importPersonalities(ctx context.Context, userID uuid.UUID, zr *zip.Reader) (models.SectionImportCounts, map[uuid.UUID]uuid.UUID) {
	var counts models.SectionImportCounts
	ids := make(map[uuid.UUID]uuid.UUID)

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
		out = append(out, models.ImportConversation{
			Title:                      title,
			CreatedAt:                  createdAt.UTC(),
			Source:                     models.ChatSourceAnthropic,
			ImportHash:                 conversationImportHash(c.UUID),
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

// remapMemoryArchive rewrites account-export relationships to target-account IDs before the
// generic memory importer validates ownership. Source memory IDs are rekeyed per target account
// because Memory.ID is globally unique; chat references and personality section filenames change.
// It rejects duplicate names, including names that collide after personality-ID remapping.
func remapMemoryArchive(zr *zip.Reader, targetUserID uuid.UUID, chatIDs, personalityIDs map[uuid.UUID]uuid.UUID) ([]byte, error) {
	entries := make(map[string][]byte, len(zr.File))
	for _, zf := range zr.File {
		data, err := readZipFile(zf, maxImportExpandedBytes)
		if err != nil {
			return nil, err
		}
		name := zf.Name
		if name == "chat.json" || name == "user.json" || (strings.HasPrefix(name, "personality-") && strings.HasSuffix(name, ".json")) {
			data, err = remapMemoryRecords(data, targetUserID, chatIDs)
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

func remapMemoryRecords(data []byte, targetUserID uuid.UUID, chatIDs map[uuid.UUID]uuid.UUID) ([]byte, error) {
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
		// Memory IDs are global primary keys. Deriving a destination ID from the target user and
		// source ID keeps a repeated import idempotent for that account while allowing the same
		// account ZIP to be restored into another account without a primary-key collision.
		record.ID = uuid.NewSHA1(targetUserID, record.ID[:])
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
