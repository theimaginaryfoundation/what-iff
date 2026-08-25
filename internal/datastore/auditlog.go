package datastore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const (
	auditCategoryModel         = "model"
	auditCategoryQuota         = "quota"
	auditCategoryAccountBackup = "account_backup"
	auditCategoryMemoryPack    = "memory_pack"
)

type auditEntry struct {
	Category      string
	Action        string
	Message       string
	SubjectUserID *uuid.UUID
	Metadata      map[string]any
}

func (d *Datastore) writeAuditLog(ctx context.Context, e auditEntry) {
	if d == nil || d.dbClient == nil {
		return
	}
	msg := e.Message
	if len(e.Metadata) > 0 {
		if b, err := json.Marshal(e.Metadata); err == nil && len(b) > 0 {
			msg = fmt.Sprintf("%s | metadata=%s", msg, string(b))
		}
	}
	var actor *uuid.UUID
	if id, ok := apicontext.UserIDFrom(ctx); ok {
		actor = &id
	}
	_, err := d.dbClient.AuditLog.Create().
		SetCategory(e.Category).
		SetAction(e.Action).
		SetMessage(msg).
		SetNillableActorUserID(actor).
		SetNillableSubjectUserID(e.SubjectUserID).
		Save(ctx)
	if err != nil {
		d.logger.Warn("audit log write failed",
			zap.String("category", e.Category),
			zap.String("action", e.Action),
			zap.Error(err))
	}
}

func (d *Datastore) auditMemoryPackImport(ctx context.Context, userID uuid.UUID, result models.MemoryImportResult, opErr error) {
	sub := userID
	meta := map[string]any{
		"success":                           opErr == nil,
		"imported_count":                    result.ImportedCount,
		"duplicate_count":                   result.DuplicateCount,
		"invalid_record_count":              result.InvalidRecordCount,
		"skipped_missing_chat_count":        result.SkippedMissingChat,
		"skipped_missing_personality_count": result.SkippedMissingPersonality,
	}
	if opErr != nil {
		meta["error"] = opErr.Error()
	}
	d.writeAuditLog(ctx, auditEntry{
		Category:      auditCategoryMemoryPack,
		Action:        "import",
		Message:       fmt.Sprintf("user memory ZIP import (success=%v)", opErr == nil),
		SubjectUserID: &sub,
		Metadata:      meta,
	})
}
