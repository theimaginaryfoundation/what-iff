package datastore

import (
	"context"
	"encoding/json"
	"fmt"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent/auditlog"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const (
	auditCategoryModel         = "model"
	auditCategoryQuota         = "quota"
	auditCategoryAccountBackup = "account_backup"
	auditCategoryAccountExport = "account_export"
	auditCategoryMemoryPack    = "memory_pack"
	auditCategoryChatImport    = "chat_import"
)

// accountActivityCategories are the audit categories surfaced on the Import & Export screen's
// activity log — the user-facing import/export flows.
var accountActivityCategories = []string{auditCategoryAccountExport, auditCategoryChatImport}

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
		"invalid_reasons":                   result.InvalidReasons,
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

// AuditChatImport records a ChatGPT/Claude conversation import so it shows on the activity log
// alongside account export/import. Best-effort, like the other audit writes.
func (d *Datastore) AuditChatImport(ctx context.Context, userID uuid.UUID, message string, metadata map[string]any) {
	subject := userID
	d.writeAuditLog(ctx, auditEntry{
		Category:      auditCategoryChatImport,
		Action:        "import",
		Message:       message,
		SubjectUserID: &subject,
		Metadata:      metadata,
	})
}

// ListAccountActivity returns the user's recent import/export audit entries, newest first.
func (d *Datastore) ListAccountActivity(ctx context.Context, userID uuid.UUID, limit int) ([]models.AccountActivityEntry, error) {
	if d == nil || d.dbClient == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := d.dbClient.AuditLog.Query().
		Where(
			auditlog.SubjectUserID(userID),
			auditlog.CategoryIn(accountActivityCategories...),
		).
		Order(auditlog.ByOccurredAt(sql.OrderDesc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.AccountActivityEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.AccountActivityEntry{
			OccurredAt: r.OccurredAt,
			Category:   r.Category,
			Action:     r.Action,
			Message:    r.Message,
		})
	}
	return out, nil
}

// AuditAccountExport records an account-portability action without storing archive URLs or contents.
func (d *Datastore) AuditAccountExport(ctx context.Context, userID uuid.UUID, action, message string, metadata map[string]any) {
	subject := userID
	d.writeAuditLog(ctx, auditEntry{
		Category:      auditCategoryAccountExport,
		Action:        action,
		Message:       message,
		SubjectUserID: &subject,
		Metadata:      metadata,
	})
}
