package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/ent/filechunk"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"go.uber.org/zap"
)

var ErrNoChunks = errors.New("no chunks provided")
var ErrNoFileSearchContext = errors.New("at least one of personalityID or chatID must be provided")
var ErrAlreadyChunked = errors.New("file attachment already has chunks")

const FileChunkRelevanceThreshold = 1.2

// FileChunkInput represents the input data for creating a file chunk
type FileChunkInput struct {
	Content   string
	Embedding []float32
	Sequence  int
	Metadata  map[string]string
}

// FileChunkResult represents a file chunk returned from a similarity search
type FileChunkResult struct {
	Content  string
	FileName string
	Sequence int
	Score    float64
	// CreatedAt is the parent file attachment's upload time. Populated so callers can
	// time-filter file results (e.g. recall's time_scope). Zero when the attachment edge
	// was not loaded.
	CreatedAt time.Time
}

// CreateFileChunks persists file chunks for a file attachment and updates chunk_status.
// It is idempotent: if the file already has chunk_status set, it returns ErrAlreadyChunked.
func (d *Datastore) CreateFileChunks(ctx context.Context, fileAttachmentID uuid.UUID, chunks []FileChunkInput) error {
	if len(chunks) == 0 {
		return ErrNoChunks
	}

	// Idempotency check: if chunk_status is already set, skip processing.
	// TODO: Move this check inside the transaction with SELECT ... FOR UPDATE to
	// prevent TOCTOU race where concurrent calls both pass this check before either
	// commits. Current risk is low (backfill runs sequentially, uploads are user-initiated).
	fa, err := d.dbClient.FileAttachment.Get(ctx, fileAttachmentID)
	if err != nil {
		d.logger.Error(i18n.T1("filechunk.fetch_failed", "FileAttachmentID", fileAttachmentID.String()), zap.Error(err))
		return fmt.Errorf("fetching file attachment: %w", err)
	}
	if fa.ChunkStatus != nil {
		d.logger.Info(i18n.T2("filechunk.already_processed", "FileAttachmentID", fileAttachmentID.String(), "ExistingStatus", string(*fa.ChunkStatus)))
		return ErrAlreadyChunked
	}

	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Create each chunk within the transaction
	for i, chunk := range chunks {
		_, err := tx.FileChunk.Create().
			SetContent(chunk.Content).
			SetEmbedding(pgvector.NewVector(chunk.Embedding)).
			SetSequence(chunk.Sequence).
			SetMetadata(chunk.Metadata).
			SetFileAttachmentID(fileAttachmentID).
			Save(ctx)
		if err != nil {
			d.logger.Error(i18n.T2("filechunk.create_failed", "ChunkIndex", i, "FileAttachmentID", fileAttachmentID.String()), zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			// Mark the file attachment as failed
			d.updateChunkStatus(ctx, fileAttachmentID, fileattachment.ChunkStatusFailed)
			return err
		}
	}

	// Update chunk_status to "chunked" on success
	_, err = tx.FileAttachment.UpdateOneID(fileAttachmentID).
		SetChunkStatus(fileattachment.ChunkStatusChunked).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("filechunk.status_update_failed", "FileAttachmentID", fileAttachmentID.String()), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return err
	}

	return nil
}

// updateChunkStatus updates the chunk_status of a file attachment outside of a transaction.
// Used for error handling when the main transaction has already been rolled back.
func (d *Datastore) updateChunkStatus(ctx context.Context, fileAttachmentID uuid.UUID, status fileattachment.ChunkStatus) {
	_, err := d.dbClient.FileAttachment.UpdateOneID(fileAttachmentID).
		SetChunkStatus(status).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T2("filechunk.status_update_after_error_failed", "FileAttachmentID", fileAttachmentID.String(), "Status", string(status)), zap.Error(err))
	}
}

// SetFileChunkStatus sets the chunk_status of a file attachment.
// Used for marking empty files as "chunked" without creating any chunks.
func (d *Datastore) SetFileChunkStatus(ctx context.Context, fileAttachmentID uuid.UUID, status fileattachment.ChunkStatus) error {
	_, err := d.dbClient.FileAttachment.UpdateOneID(fileAttachmentID).
		SetChunkStatus(status).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T2("filechunk.set_status_failed", "FileAttachmentID", fileAttachmentID.String(), "Status", string(status)), zap.Error(err))
		return fmt.Errorf("setting chunk_status: %w", err)
	}
	return nil
}

// GetRelatedFileChunks returns file chunks relevant to a query embedding, scoped by user and optionally by personality/chat.
// At least one of personalityID or chatID must be non-nil to provide search context.
func (d *Datastore) GetRelatedFileChunks(ctx context.Context, userID uuid.UUID, personalityID *uuid.UUID, chatID *uuid.UUID, queryEmbedding []float32, limit int) ([]FileChunkResult, error) {
	if personalityID == nil && chatID == nil {
		return nil, ErrNoFileSearchContext
	}

	embVec := pgvector.NewVector(queryEmbedding)

	// Build the query with conditional JOINs and WHERE clauses
	//
	// Table/column references (from ent/migrate/schema.go):
	//   file_chunks.file_chunk_file_attachment       -> file_attachments.id
	//   file_attachments.user_file_attachments        -> users.id
	//   file_attachments.personality_file_attachments  -> personalities.id
	//   file_attachments.chat_message_file_attachments -> chat_messages.id
	//   chat_messages.chat_messages                    -> chats.id
	//
	// Vector is passed as $1 (parameterized) to avoid SQL injection from string interpolation.

	baseQuery := `
		SELECT fc.content, fa.name, fc.sequence, fc.embedding <-> $1::vector AS score, fa.created_at
		FROM file_chunks fc
		JOIN file_attachments fa ON fc.file_chunk_file_attachment = fa.id
		WHERE fa.user_file_attachments = $2`

	args := []interface{}{embVec, userID}
	argIdx := 3

	var filterClause string
	if personalityID != nil && chatID != nil {
		// OR filter: personality files OR chat files
		filterClause = fmt.Sprintf(`
			AND (
				fa.personality_file_attachments = $%d
				OR fa.chat_message_file_attachments IN (
					SELECT cm.id FROM chat_messages cm WHERE cm.chat_messages = $%d
				)
			)`, argIdx, argIdx+1)
		args = append(args, *personalityID, *chatID)
		argIdx += 2
	} else if personalityID != nil {
		filterClause = fmt.Sprintf(`
			AND fa.personality_file_attachments = $%d`, argIdx)
		args = append(args, *personalityID)
		argIdx++
	} else if chatID != nil {
		filterClause = fmt.Sprintf(`
			AND fa.chat_message_file_attachments IN (
				SELECT cm.id FROM chat_messages cm WHERE cm.chat_messages = $%d
			)`, argIdx)
		args = append(args, *chatID)
		argIdx++
	}

	distanceClause := fmt.Sprintf(`
		AND fc.embedding <-> $1::vector <= $%d`, argIdx)
	args = append(args, FileChunkRelevanceThreshold)
	argIdx++

	orderClause := fmt.Sprintf(`
		ORDER BY fc.embedding <-> $1::vector ASC
		LIMIT $%d`, argIdx)
	args = append(args, limit)

	query := baseQuery + filterClause + distanceClause + orderClause

	// Execute raw SQL directly — no transaction needed for a read-only query.
	rows, err := d.sqlDB.QueryContext(ctx, query, args...)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "related file chunks"), zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var results []FileChunkResult
	for rows.Next() {
		var r FileChunkResult
		if err := rows.Scan(&r.Content, &r.FileName, &r.Sequence, &r.Score, &r.CreatedAt); err != nil {
			d.logger.Error(i18n.T("filechunk.scan_failed"), zap.Error(err))
			return nil, err
		}
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		d.logger.Error(i18n.T("filechunk.iterate_failed"), zap.Error(err))
		return nil, err
	}

	return results, nil
}

// ListFileChunksForAttachment returns up to limit chunks for one attachment ordered by
// sequence (the first chunks of the file). Used for large-file previews in chat context.
func (d *Datastore) ListFileChunksForAttachment(ctx context.Context, fileAttachmentID uuid.UUID, limit int) ([]FileChunkResult, error) {
	if fileAttachmentID == uuid.Nil {
		return nil, fmt.Errorf("file attachment id is required")
	}
	if limit <= 0 {
		limit = 5
	}

	rows, err := d.dbClient.FileChunk.Query().
		Where(
			filechunk.HasFileAttachmentWith(
				fileattachment.IDEQ(fileAttachmentID),
			),
		).
		Order(filechunk.BySequence()).
		Limit(limit).
		WithFileAttachment().
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "file chunks by attachment"), zap.Error(err))
		return nil, err
	}

	results := make([]FileChunkResult, 0, len(rows))
	for _, row := range rows {
		name := ""
		var createdAt time.Time
		if row.Edges.FileAttachment != nil {
			name = row.Edges.FileAttachment.Name
			createdAt = row.Edges.FileAttachment.CreatedAt
		}
		results = append(results, FileChunkResult{
			Content:   row.Content,
			FileName:  name,
			Sequence:  row.Sequence,
			CreatedAt: createdAt,
		})
	}
	return results, nil
}
