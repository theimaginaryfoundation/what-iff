package filechunker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/embedding"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"go.uber.org/zap"
)

// SupportedTextTypes maps MIME content types that we can chunk directly as text.
// Note: XML (application/xml) intentionally excluded - VectorSupport: false in fileutils.go
// Follow-up: add PDF/DOCX extraction for application/pdf,
// application/vnd.openxmlformats-officedocument.wordprocessingml.document
var SupportedTextTypes = map[string]struct{}{
	// text/* types
	"text/plain":           {},
	"text/markdown":        {},
	"text/html":            {},
	"text/css":             {},
	"text/x-golang":        {},
	"text/x-script.python": {},
	"text/x-java":          {},
	"text/x-c":             {},
	"text/x-c++":           {},
	"text/x-csharp":        {},

	// application/* types that are really text
	"application/json":       {},
	"text/javascript":        {},
	"application/x-sh":       {},
	"text/x-ruby":            {},
	"text/x-php":             {},
	"text/x-tex":             {},
	"application/typescript": {},
}

// textFileExtensions lists file extensions we support as text content.
var textFileExtensions = map[string]struct{}{
	".txt":  {},
	".md":   {},
	".go":   {},
	".py":   {},
	".js":   {},
	".ts":   {},
	".html": {},
	".css":  {},
	".json": {},
	".c":    {},
	".cpp":  {},
	".cs":   {},
	".java": {},
	".rb":   {},
	".sh":   {},
	".php":  {},
	".tex":  {},
}

// IsTextType returns true if the given MIME content type can be chunked as text.
func IsTextType(contentType string) bool {
	// Normalize: strip parameters like "; charset=utf-8"
	ct := strings.TrimSpace(contentType)
	if idx := strings.Index(ct, ";"); idx != -1 {
		ct = strings.TrimSpace(ct[:idx])
	}
	ct = strings.ToLower(ct)
	_, ok := SupportedTextTypes[ct]
	return ok
}

// IsTextFileByExtension returns true for file extensions we support as text.
func IsTextFileByExtension(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	_, ok := textFileExtensions[ext]
	return ok
}

// FileChunkPipeline orchestrates chunking and embedding of file content.
type FileChunkPipeline struct {
	oaiClient *openai.Client
	ds        *datastore.Datastore
	logger    *zap.Logger
	// skipEmbeddings marks uploads as chunked without calling the embeddings
	// API. Set under MOCK_LLM so file uploads complete deliberately instead of
	// failing on the deny-network transport.
	skipEmbeddings bool
}

// NewFileChunkPipeline creates a new pipeline with the given dependencies.
func NewFileChunkPipeline(oaiClient *openai.Client, ds *datastore.Datastore, logger *zap.Logger) *FileChunkPipeline {
	return &FileChunkPipeline{
		oaiClient: oaiClient,
		ds:        ds,
		logger:    logger,
	}
}

// NewMockFileChunkPipeline builds a pipeline that skips embedding generation
// (MOCK_LLM mode): uploads are accepted and marked chunked, but no chunk
// vectors are produced, so file search returns nothing deterministic.
func NewMockFileChunkPipeline(ds *datastore.Datastore, logger *zap.Logger) *FileChunkPipeline {
	return &FileChunkPipeline{ds: ds, logger: logger, skipEmbeddings: true}
}

// ProcessAndStore extracts text from content, chunks it, embeds each chunk, and stores them.
// It updates chunk_status on the FileAttachment to "chunked" (on success) or "failed" (on error).
// This method is safe to call from a goroutine — it logs errors rather than panicking.
func (p *FileChunkPipeline) ProcessAndStore(ctx context.Context, fileAttachmentID uuid.UUID, content []byte, fileName string, contentType string) error {
	// 1. Check if the content type is supported.
	if !IsTextType(contentType) && !IsTextFileByExtension(fileName) {
		return fmt.Errorf("unsupported content type %q for file %q", contentType, fileName)
	}

	// Mock mode: accept the upload and mark it chunked without embeddings so
	// the attachment lifecycle completes; no provider call is attempted.
	if p.skipEmbeddings {
		p.logger.Debug("mock mode: marking file chunked without embeddings",
			zap.String("file_name", fileName),
			zap.String("file_attachment_id", fileAttachmentID.String()))
		if p.ds != nil {
			if err := p.ds.SetFileChunkStatus(ctx, fileAttachmentID, fileattachment.ChunkStatusChunked); err != nil {
				return fmt.Errorf("marking file as chunked in mock mode: %w", err)
			}
		}
		return nil
	}

	// 2. Convert content bytes to string.
	text := string(content)

	// 3. Chunk the text.
	textChunks := ChunkText(text, DefaultChunkSize, DefaultOverlap)

	// 4. If no chunks returned (empty file), mark as chunked and return.
	if len(textChunks) == 0 {
		p.logger.Info("empty file, marking as chunked with no chunks",
			zap.String("file_name", fileName),
			zap.String("file_attachment_id", fileAttachmentID.String()))
		// Set chunk_status to "chunked" so backfill doesn't reprocess empty files.
		if p.ds != nil {
			if err := p.ds.SetFileChunkStatus(ctx, fileAttachmentID, fileattachment.ChunkStatusChunked); err != nil {
				return fmt.Errorf("marking empty file as chunked: %w", err)
			}
		}
		return nil
	}

	// 5. Embed each chunk and build storage inputs.
	chunkInputs := make([]datastore.FileChunkInput, 0, len(textChunks))
	for _, tc := range textChunks {
		vec, err := embedding.CreateEmbedding(ctx, p.oaiClient, tc.Content)
		if err != nil {
			p.logger.Error("failed to create embedding for chunk",
				zap.Error(err),
				zap.String("file_name", fileName),
				zap.Int("sequence", tc.Sequence),
				zap.String("file_attachment_id", fileAttachmentID.String()))
			return fmt.Errorf("embedding chunk %d of %q: %w", tc.Sequence, fileName, err)
		}

		chunkInputs = append(chunkInputs, datastore.FileChunkInput{
			Content:   tc.Content,
			Embedding: vec,
			Sequence:  tc.Sequence,
			Metadata:  map[string]string{"fileName": fileName},
		})
	}

	// 6. Store chunks. The datastore handles setting chunk_status to "chunked" on
	//    success and "failed" on error.
	// TODO: Add nil check for p.ds before calling CreateFileChunks. Currently all
	// callers provide a datastore, but defensive check would prevent nil panic.
	if err := p.ds.CreateFileChunks(ctx, fileAttachmentID, chunkInputs); err != nil {
		// Idempotency: if already chunked, treat as success.
		if errors.Is(err, datastore.ErrAlreadyChunked) {
			p.logger.Info("file already chunked, skipping",
				zap.String("file_name", fileName),
				zap.String("file_attachment_id", fileAttachmentID.String()))
			return nil
		}
		p.logger.Error("failed to store file chunks",
			zap.Error(err),
			zap.String("file_name", fileName),
			zap.String("file_attachment_id", fileAttachmentID.String()))
		return fmt.Errorf("storing chunks for %q: %w", fileName, err)
	}

	p.logger.Info("file chunked and stored successfully",
		zap.String("file_name", fileName),
		zap.Int("chunk_count", len(chunkInputs)),
		zap.String("file_attachment_id", fileAttachmentID.String()))

	return nil
}
