package datastore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateFileChunks_Success(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	fileAttachmentID := uuid.New()
	chunks := []FileChunkInput{
		{
			Content:   "chunk one content",
			Embedding: []float32{0.1, 0.2, 0.3},
			Sequence:  0,
			Metadata:  map[string]string{"page": "1"},
		},
		{
			Content:   "chunk two content",
			Embedding: []float32{0.4, 0.5, 0.6},
			Sequence:  1,
			Metadata:  map[string]string{"page": "2"},
		},
	}

	// Expect idempotency check: SELECT to verify chunk_status is nil
	mock.ExpectQuery("SELECT.*file_attachments.*").
		WithArgs(fileAttachmentID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "file_id", "name", "file_type", "file_content", "chunk_status", "created_at"}).
			AddRow(fileAttachmentID, "file-123", "test.pdf", "application/pdf", nil, nil, time.Now()))

	// Expect transaction begin
	mock.ExpectBegin()

	// Expect INSERT for each chunk
	for range chunks {
		mock.ExpectExec("INSERT INTO .*file_chunks.*").
			WillReturnResult(sqlmock.NewResult(1, 1))
	}

	// Expect UPDATE to set chunk_status to "chunked"
	mock.ExpectExec("UPDATE .*file_attachments.*").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Ent's UpdateOneID().Save() issues a SELECT after the UPDATE to return the entity
	mock.ExpectQuery("SELECT.*file_attachments.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "file_id", "name", "file_type", "file_content", "chunk_status", "created_at"}).
			AddRow(fileAttachmentID, "file-123", "test.pdf", "application/pdf", nil, "chunked", time.Now()))

	// Expect transaction commit
	mock.ExpectCommit()

	err := ds.CreateFileChunks(context.Background(), fileAttachmentID, chunks)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateFileChunks_InsertError(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	fileAttachmentID := uuid.New()
	chunks := []FileChunkInput{
		{
			Content:   "chunk one content",
			Embedding: []float32{0.1, 0.2, 0.3},
			Sequence:  0,
			Metadata:  map[string]string{"page": "1"},
		},
	}

	// Expect idempotency check: SELECT to verify chunk_status is nil
	mock.ExpectQuery("SELECT.*file_attachments.*").
		WithArgs(fileAttachmentID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "file_id", "name", "file_type", "file_content", "chunk_status", "created_at"}).
			AddRow(fileAttachmentID, "file-123", "test.pdf", "application/pdf", nil, nil, time.Now()))

	// Expect transaction begin
	mock.ExpectBegin()

	// First INSERT fails
	mock.ExpectExec("INSERT INTO .*file_chunks.*").
		WillReturnError(errors.New("insert failed"))

	// Expect rollback
	mock.ExpectRollback()

	// Expect UPDATE to set chunk_status to "failed" (outside the transaction via updateChunkStatus)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE .*file_attachments.*").
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Ent's UpdateOneID().Save() issues a SELECT after the UPDATE to return the entity
	mock.ExpectQuery("SELECT.*file_attachments.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "file_id", "name", "file_type", "file_content", "chunk_status", "created_at"}).
			AddRow(fileAttachmentID, "file-123", "test.pdf", "application/pdf", nil, "failed", time.Now()))
	mock.ExpectCommit()

	err := ds.CreateFileChunks(context.Background(), fileAttachmentID, chunks)
	require.Error(t, err)
	require.ErrorContains(t, err, "insert failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetRelatedFileChunks_WithResults(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	userID := uuid.New()
	personalityID := uuid.New()
	chatID := uuid.New()
	queryEmbedding := []float32{0.1, 0.2, 0.3}

	// Expect the SELECT query with JOINs (no transaction for read-only query)
	chunkCreatedAt := time.Now().UTC().Truncate(time.Second)
	rows := sqlmock.NewRows([]string{"content", "name", "id", "sequence", "score", "created_at"}).
		AddRow("relevant chunk content", "document.pdf", uuid.New(), 0, 0.85, chunkCreatedAt).
		AddRow("another relevant chunk", "notes.txt", uuid.New(), 1, 0.92, chunkCreatedAt)

	mock.ExpectQuery("SELECT.*file_chunks.*").
		WithArgs(sqlmock.AnyArg(), userID, personalityID, chatID, FileChunkRelevanceThreshold, 10).
		WillReturnRows(rows)

	results, err := ds.GetRelatedFileChunks(context.Background(), userID, &personalityID, &chatID, queryEmbedding, 10)
	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.Equal(t, "relevant chunk content", results[0].Content)
	assert.Equal(t, "document.pdf", results[0].FileName)
	assert.Equal(t, 0, results[0].Sequence)
	assert.Equal(t, 0.85, results[0].Score)
	assert.Equal(t, chunkCreatedAt, results[0].CreatedAt.UTC())

	assert.Equal(t, "another relevant chunk", results[1].Content)
	assert.Equal(t, "notes.txt", results[1].FileName)
	assert.Equal(t, 1, results[1].Sequence)
	assert.Equal(t, 0.92, results[1].Score)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetRelatedFileChunks_NoResults(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	userID := uuid.New()
	personalityID := uuid.New()
	chatID := uuid.New()
	queryEmbedding := []float32{0.1, 0.2, 0.3}

	// Expect the SELECT query returning no rows (no transaction for read-only query)
	rows := sqlmock.NewRows([]string{"content", "name", "id", "sequence", "score", "created_at"})

	mock.ExpectQuery("SELECT.*file_chunks.*").
		WithArgs(sqlmock.AnyArg(), userID, personalityID, chatID, FileChunkRelevanceThreshold, 10).
		WillReturnRows(rows)

	results, err := ds.GetRelatedFileChunks(context.Background(), userID, &personalityID, &chatID, queryEmbedding, 10)
	require.NoError(t, err)
	assert.Empty(t, results)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetRelatedFileChunks_PersonalityOnly(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	userID := uuid.New()
	personalityID := uuid.New()
	queryEmbedding := []float32{0.1, 0.2, 0.3}

	// Expect the SELECT query with only personality filter (no chat subquery, no transaction)
	rows := sqlmock.NewRows([]string{"content", "name", "id", "sequence", "score", "created_at"}).
		AddRow("personality chunk", "persona-doc.pdf", uuid.New(), 0, 0.75, time.Now())

	mock.ExpectQuery("SELECT.*file_chunks.*").
		WithArgs(sqlmock.AnyArg(), userID, personalityID, FileChunkRelevanceThreshold, 5).
		WillReturnRows(rows)

	results, err := ds.GetRelatedFileChunks(context.Background(), userID, &personalityID, nil, queryEmbedding, 5)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "personality chunk", results[0].Content)
	assert.Equal(t, "persona-doc.pdf", results[0].FileName)
	assert.Equal(t, 0, results[0].Sequence)
	assert.Equal(t, 0.75, results[0].Score)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetRelatedFileChunks_ChatOnly(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	userID := uuid.New()
	chatID := uuid.New()
	queryEmbedding := []float32{0.1, 0.2, 0.3}

	// Expect the SELECT query with only chat filter (no personality filter, no transaction)
	rows := sqlmock.NewRows([]string{"content", "name", "id", "sequence", "score", "created_at"}).
		AddRow("chat chunk", "uploaded-file.txt", uuid.New(), 2, 0.60, time.Now())

	mock.ExpectQuery("SELECT.*file_chunks.*").
		WithArgs(sqlmock.AnyArg(), userID, chatID, FileChunkRelevanceThreshold, 5).
		WillReturnRows(rows)

	results, err := ds.GetRelatedFileChunks(context.Background(), userID, nil, &chatID, queryEmbedding, 5)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "chat chunk", results[0].Content)
	assert.Equal(t, "uploaded-file.txt", results[0].FileName)
	assert.Equal(t, 2, results[0].Sequence)
	assert.Equal(t, 0.60, results[0].Score)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateFileChunks_EmptyChunks(t *testing.T) {
	ds, _, cleanup := newMockDatastore(t)
	defer cleanup()

	err := ds.CreateFileChunks(context.Background(), uuid.New(), []FileChunkInput{})
	require.ErrorIs(t, err, ErrNoChunks)
}

func TestCreateFileChunks_AlreadyChunked(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	fileAttachmentID := uuid.New()
	chunks := []FileChunkInput{
		{
			Content:   "chunk content",
			Embedding: []float32{0.1, 0.2, 0.3},
			Sequence:  0,
			Metadata:  map[string]string{"page": "1"},
		},
	}

	// Expect idempotency check: SELECT returns file with chunk_status already set
	mock.ExpectQuery("SELECT.*file_attachments.*").
		WithArgs(fileAttachmentID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "file_id", "name", "file_type", "file_content", "chunk_status", "created_at"}).
			AddRow(fileAttachmentID, "file-123", "test.pdf", "application/pdf", nil, "chunked", time.Now()))

	// No transaction expected - should return early

	err := ds.CreateFileChunks(context.Background(), fileAttachmentID, chunks)
	require.ErrorIs(t, err, ErrAlreadyChunked)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetRelatedFileChunks_NoContext(t *testing.T) {
	ds, _, cleanup := newMockDatastore(t)
	defer cleanup()

	queryEmbedding := []float32{0.1, 0.2, 0.3}
	results, err := ds.GetRelatedFileChunks(context.Background(), uuid.New(), nil, nil, queryEmbedding, 5)
	require.ErrorIs(t, err, ErrNoFileSearchContext)
	assert.Nil(t, results)
}

func TestFileChunkRelevanceThreshold(t *testing.T) {
	assert.Equal(t, 1.2, FileChunkRelevanceThreshold)
}

func TestSetFileChunkStatus_Success(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	fileAttachmentID := uuid.New()
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE .*file_attachments.*").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT.*file_attachments.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "file_id", "name", "file_type", "file_content", "chunk_status", "created_at"}).
			AddRow(fileAttachmentID, "file-123", "test.pdf", "application/pdf", nil, "chunked", time.Now()))
	mock.ExpectCommit()

	err := ds.SetFileChunkStatus(context.Background(), fileAttachmentID, "chunked")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
