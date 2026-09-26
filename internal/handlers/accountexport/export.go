package accountexport

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/email"
	"github.com/theimaginaryfoundation/what-iff/internal/exporter"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

// runAccountExport builds the export ZIP, uploads it, presigns a link, and emails it. All failures
// are recorded on the job and swallowed so a panic never takes down the request path. The download
// link is emailed and never written to the job (email-only delivery is the security control).
func (h *Handler) runAccountExport(ctx context.Context, userID, jobID uuid.UUID) {
	defer func() {
		if v := recover(); v != nil {
			h.logger.Error("account export: panic in build",
				zap.String("job_id", jobID.String()), zap.Any("panic", v), zap.ByteString("stack", debug.Stack()))
			h.failExport(ctx, userID, jobID, "Export failed unexpectedly")
		}
	}()

	if _, err := h.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusProcessing, ""); err != nil {
		h.logger.Warn("account export: mark processing failed", zap.String("job_id", jobID.String()), zap.Error(err))
	}
	h.setProgress(ctx, userID, jobID, models.AccountExportProgress{Phase: models.AccountExportPhaseBuilding})

	userEmail, username, err := h.ds.ExportAccountIdentity(ctx, userID)
	if err != nil {
		h.logger.Error("account export: identity load failed", zap.String("job_id", jobID.String()), zap.Error(err))
		h.failExport(ctx, userID, jobID, "Failed to build export")
		return
	}

	zipBytes, counts, err := h.buildAccountZip(ctx, userID, userEmail, username)
	if err != nil {
		h.logger.Error("account export: build failed", zap.String("job_id", jobID.String()), zap.Error(err))
		h.failExport(ctx, userID, jobID, "Failed to build export")
		return
	}

	h.setProgress(ctx, userID, jobID, models.AccountExportProgress{Phase: models.AccountExportPhaseUploading, Counts: counts})

	key := path.Join(bundlePrefix, userID.String(), fmt.Sprintf("account-export-%s.zip", time.Now().UTC().Format("20060102-150405")))
	if err := h.fileStore.UploadFile(ctx, key, zipBytes, "application/zip"); err != nil {
		h.logger.Error("account export: upload failed", zap.String("job_id", jobID.String()), zap.Error(err))
		h.failExport(ctx, userID, jobID, "Failed to store export")
		return
	}

	url, expiresAt, err := h.presignBundle(ctx, key)
	if err != nil {
		h.logger.Error("account export: presign failed", zap.String("job_id", jobID.String()), zap.Error(err))
		h.failExport(ctx, userID, jobID, "Failed to prepare download link")
		return
	}

	// Email is the sole delivery channel; if it fails the user can't get the link, so fail the job.
	if err := h.sender.SendExportReady(ctx, userEmail, email.ExportReadyData{
		Username:   username,
		BundleURL:  url,
		ExpiresAt:  expiresAt,
		FilesCount: counts["files"],
	}); err != nil {
		h.logger.Error("account export: email send failed", zap.String("job_id", jobID.String()), zap.Error(err))
		// The link is undeliverable, so the uploaded archive is unreachable — clean it up rather than
		// leaving an orphan (best effort; an S3 lifecycle rule is the backstop).
		if delErr := h.fileStore.DeleteFile(ctx, key); delErr != nil {
			h.logger.Warn("account export: failed to remove archive after email failure", zap.String("key", key), zap.Error(delErr))
		}
		h.failExport(ctx, userID, jobID, "Export built but the notification email could not be sent")
		return
	}

	h.setProgress(ctx, userID, jobID, models.AccountExportProgress{
		Phase:   models.AccountExportPhaseComplete,
		Counts:  counts,
		Message: "Export ready — a download link has been emailed to you.",
	})
	if _, err := h.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusComplete, ""); err != nil {
		h.logger.Warn("account export: mark complete failed", zap.String("job_id", jobID.String()), zap.Error(err))
	}
	h.ds.AuditAccountExport(ctx, userID, "completed", "account export delivered by email", map[string]any{
		"job_id": jobID.String(),
		"counts": counts,
	})
	h.logger.Info("account export complete", zap.String("job_id", jobID.String()), zap.String("key", key))
}

// buildAccountZip assembles the in-memory export archive and returns it with section counts. The
// account identity is passed in (already loaded by the caller) to stamp the manifest.
func (h *Handler) buildAccountZip(ctx context.Context, userID uuid.UUID, userEmail, username string) ([]byte, map[string]int, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	tree := exporter.NewZipTree(zw)

	convs, err := h.ds.ExportConversationInputs(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("conversations: %w", err)
	}
	if err := exporter.WriteConversations(tree, convs); err != nil {
		return nil, nil, err
	}

	personas, err := h.ds.ExportPersonalityInputs(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("personalities: %w", err)
	}
	if err := exporter.WritePersonalities(tree, personas); err != nil {
		return nil, nil, err
	}

	// Memories ride as a nested memories.zip in the existing memory-export format, so they re-import
	// through the same ds.ImportMemories path (embedding regeneration included).
	var memBuf bytes.Buffer
	if err := h.ds.ExportMemories(ctx, userID, &memBuf); err != nil {
		return nil, nil, fmt.Errorf("memories: %w", err)
	}
	if err := tree.Write("memories.zip", memBuf.Bytes()); err != nil {
		return nil, nil, err
	}

	filesPrefix := path.Join("users", userID.String()) + "/"
	fileObjs, err := h.listUserFiles(ctx, filesPrefix)
	if err != nil {
		return nil, nil, fmt.Errorf("files: %w", err)
	}
	if err := exporter.WriteFilesManifest(tree, filesPrefix, fileObjs); err != nil {
		return nil, nil, err
	}

	counts := map[string]int{
		"conversations": len(convs),
		"personalities": len(personas),
		"files":         len(fileObjs),
	}
	if err := exporter.WriteManifest(tree, exporter.Manifest{
		ExportedAt: time.Now().UTC(),
		Account:    exporter.ManifestAccount{UserID: userID.String(), Email: userEmail, Username: username},
		Counts:     counts,
	}); err != nil {
		return nil, nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, nil, fmt.Errorf("finalize zip: %w", err)
	}
	return buf.Bytes(), counts, nil
}

// listUserFiles enumerates the user's raw file objects when the store supports listing.
func (h *Handler) listUserFiles(ctx context.Context, prefix string) ([]exporter.FileObject, error) {
	lister, ok := h.fileStore.(storage.ObjectLister)
	if !ok {
		return nil, nil
	}
	objs, err := lister.ListObjects(ctx, prefix)
	if err != nil {
		return nil, err
	}
	out := make([]exporter.FileObject, 0, len(objs))
	for _, o := range objs {
		out = append(out, exporter.FileObject{Key: o.Key, Size: o.Size, ContentType: o.ContentType, ETag: o.ETag})
	}
	return out, nil
}

// presignBundle returns a presigned download URL and its expiry.
func (h *Handler) presignBundle(ctx context.Context, key string) (string, time.Time, error) {
	signer, ok := h.fileStore.(storage.Presigner)
	if !ok {
		return "", time.Time{}, fmt.Errorf("file store does not support presigned URLs")
	}
	url, err := signer.PresignGetObject(ctx, key, presignTTL)
	if err != nil {
		return "", time.Time{}, err
	}
	return url, time.Now().Add(presignTTL), nil
}

func (h *Handler) setProgress(ctx context.Context, userID, jobID uuid.UUID, p models.AccountExportProgress) {
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	if err := h.ds.UpdateJobProgress(ctx, userID, jobID, string(b)); err != nil {
		h.logger.Warn("account export: progress update failed", zap.String("job_id", jobID.String()), zap.Error(err))
	}
}

func (h *Handler) failExport(ctx context.Context, userID, jobID uuid.UUID, msg string) {
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}
	h.setProgress(ctx, userID, jobID, models.AccountExportProgress{Phase: models.AccountExportPhaseFailed, Message: msg})
	if _, err := h.ds.UpdateJobStatus(ctx, userID, jobID, models.JobStatusFailed, msg); err != nil {
		h.logger.Warn("account export: mark failed failed", zap.String("job_id", jobID.String()), zap.Error(err))
	}
	h.ds.AuditAccountExport(ctx, userID, "failed", "account export failed", map[string]any{
		"job_id": jobID.String(),
		"reason": msg,
	})
}
