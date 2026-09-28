package telemetry

import "context"

// Bounded label values for the file metrics (FileUploads, FileSize, FileOperationDuration,
// FileOperationItems). Dashboards rely on these sets, so add a constant here rather than passing
// a new string at a call site.

// Operations for the operation attribute on file metrics.
const (
	// FileOpUpload is a file attachment upload, including its async chunk-and-embed pass.
	FileOpUpload        = "upload"
	FileOpChatImport    = "chat_import"
	FileOpChatExport    = "chat_export"
	FileOpMemoryImport  = "memory_import"
	FileOpAccountImport = "account_import"
	FileOpAccountExport = "account_export"
)

// Stages for the stage attribute on FileOperationDuration.
const (
	FileStageNormalize     = "normalize"     // upload: image resize/re-encode
	FileStageChunk         = "chunk"         // upload: split text into chunks
	FileStageEmbed         = "embed"         // upload, memory_import: embedding generation
	FileStageStore         = "store"         // upload, memory_import: database inserts
	FileStageParse         = "parse"         // chat_import, memory_import: read and decode the payload
	FileStageInsert        = "insert"        // chat_import: persist conversations
	FileStageValidate      = "validate"      // account_import: open and check the archive
	FileStagePersonalities = "personalities" // account_import
	FileStageConversations = "conversations" // account_import (includes summary indexing)
	FileStageSummaries     = "summaries"     // account_import: summary embedding + upsert
	FileStageMemories      = "memories"      // account_import: nested memory import
	FileStageBuildZip      = "build_zip"     // account_export
	FileStageEmail         = "email"         // account_export: delivery email
)

// Item kinds for the kind attribute on FileOperationItems.
const (
	FileItemConversation = "conversation"
	FileItemMemory       = "memory"
	FileItemPersonality  = "personality"
	FileItemChunk        = "chunk"
	FileItemFile         = "file"
)

// Item outcomes for the outcome attribute on FileOperationItems, and upload outcomes for
// FileUploads.
const (
	FileItemImported = "imported"
	FileItemSkipped  = "skipped"
	FileItemFailed   = "failed"
	FileItemExported = "exported"

	FileUploadSuccess = "success"
	FileUploadFailure = "failure"
)

// RecordFileSize records the size of a file or payload a heavy operation handled. kind should
// come from FileKind.
func (m *Metrics) RecordFileSize(ctx context.Context, operation, kind string, bytes int64) {
	m.Record(ctx, FileSize, float64(bytes), AttrOperation.String(operation), AttrKind.String(kind))
}

// TimeFileStage times one phase of a heavy file operation; call the returned function with the
// phase's error (nil on success).
func (m *Metrics) TimeFileStage(ctx context.Context, operation, stage string) func(err error) {
	return m.Time(ctx, FileOperationDuration, AttrOperation.String(operation), AttrStage.String(stage))
}

// RecordFileItems records how many items of one kind an operation handled with one outcome.
// Zero is recorded too, so the histogram count stays one per operation run.
func (m *Metrics) RecordFileItems(ctx context.Context, operation, kind, outcome string, n int) {
	m.Record(ctx, FileOperationItems, float64(n),
		AttrOperation.String(operation), AttrKind.String(kind), AttrOutcome.String(outcome))
}

// RecordFileUpload counts one file attachment upload attempt; contentType is mapped through
// FileKind.
func (m *Metrics) RecordFileUpload(ctx context.Context, contentType, outcome string) {
	m.Add(ctx, FileUploads, 1, AttrKind.String(FileKind(contentType)), AttrOutcome.String(outcome))
}
