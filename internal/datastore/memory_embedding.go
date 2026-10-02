package datastore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/embedding"
	"github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"go.uber.org/zap"
)

// DefaultMemoryEmbeddingBackfillBatchSize is the page size BackfillMemoryEmbeddings
// uses when the caller passes a non-positive batch size.
const DefaultMemoryEmbeddingBackfillBatchSize = 50

// memoryHasNoEmbedding matches memories with no Embedding row. Memory has no
// edge back to Embedding (the FK lives on embeddings.embedding_memory), so this
// is a correlated NOT EXISTS rather than a generated Has* predicate.
func memoryHasNoEmbedding() predicate.Memory {
	return func(s *sql.Selector) {
		b := sql.Dialect(s.Dialect())
		t := b.Table(embedding.Table)
		s.Where(sql.NotExists(
			b.Select(t.C(embedding.FieldID)).
				From(t).
				Where(sql.ColumnsEQ(t.C(embedding.MemoryColumn), s.C(memory.FieldID))),
		))
	}
}

// lockRowForUpdate adds FOR UPDATE on dialects that support row locks.
// SQLite (tests) has no FOR UPDATE; it serialises writers with a database-level
// lock instead, so the clause is simply omitted there.
func lockRowForUpdate(s *sql.Selector) {
	switch s.Dialect() {
	case dialect.Postgres, dialect.MySQL:
		s.ForUpdate()
	}
}

func toMemoryEmbeddingCandidate(m *ent.Memory) models.MemoryEmbeddingCandidate {
	return models.MemoryEmbeddingCandidate{
		MemoryID:  m.ID,
		Content:   m.Content,
		CreatedAt: m.CreatedAt,
	}
}

// OwnedMemoriesMissingEmbedding returns the memories among ids that userID
// owns, that are not Summary-scope, and that have no Embedding row. Status is
// not filtered: an inactive memory is embedded too, so reactivating it makes it
// recallable without waiting for a backfill. Summary memories are excluded
// because UpsertChatSummaryMemory owns their embedding.
func (d *Datastore) OwnedMemoriesMissingEmbedding(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]models.MemoryEmbeddingCandidate, error) {
	if len(ids) == 0 {
		return []models.MemoryEmbeddingCandidate{}, nil
	}
	rows, err := d.dbClient.Memory.Query().
		Where(
			memory.IDIn(ids...),
			memory.HasOwnerWith(user.ID(userID)),
			memory.ScopeNEQ(memory.ScopeSummary),
			memoryHasNoEmbedding(),
		).
		Order(ent.Asc(memory.FieldCreatedAt), ent.Asc(memory.FieldID)).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "memories missing embedding"), zap.Error(err))
		return nil, err
	}
	out := make([]models.MemoryEmbeddingCandidate, 0, len(rows))
	for _, m := range rows {
		out = append(out, toMemoryEmbeddingCandidate(m))
	}
	return out, nil
}

// ListMemoriesMissingEmbedding returns active, non-Summary memories (across all
// users) that have no Embedding row, ordered by (created_at, id) and starting
// strictly after the given cursor. Pass the zero cursor for the first page.
// Keyset paging lets a backfill walk past rows whose embedding keeps failing
// instead of re-reading them forever.
func (d *Datastore) ListMemoriesMissingEmbedding(ctx context.Context, afterCreatedAt time.Time, afterID uuid.UUID, limit int) ([]models.MemoryEmbeddingCandidate, error) {
	if limit <= 0 {
		limit = DefaultMemoryEmbeddingBackfillBatchSize
	}
	q := d.dbClient.Memory.Query().
		Where(
			memory.StatusEQ(memory.StatusActive),
			memory.ScopeNEQ(memory.ScopeSummary),
			memoryHasNoEmbedding(),
		)
	q = memoryCursorPredicate(afterCreatedAt, afterID)(q)
	rows, err := q.
		Order(ent.Asc(memory.FieldCreatedAt), ent.Asc(memory.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("list.failed", "Entity", "memory embedding backfill candidates"), zap.Error(err))
		return nil, err
	}
	out := make([]models.MemoryEmbeddingCandidate, 0, len(rows))
	for _, m := range rows {
		out = append(out, toMemoryEmbeddingCandidate(m))
	}
	return out, nil
}

// SetMemoryEmbedding stores vec as the embedding of memoryID, replacing any
// existing Embedding row(s), but only while the memory's content still equals
// content (the text vec was computed from). It reports whether it wrote.
//
// Rows written by other paths (the create_memory tool) have random IDs; they
// are deleted first so the memory ends up with exactly one embedding.
//
// The content guard keeps a slow writer from clobbering a newer embedding: if
// the memory was edited after content was read, the edit already dropped the
// stale row and its own re-embed (or the next backfill) owns the write. Missing
// and Summary-scope memories are skipped too; UpsertChatSummaryMemory owns the
// Summary embedding.
func (d *Datastore) SetMemoryEmbedding(ctx context.Context, memoryID uuid.UUID, content string, vec []float32) (bool, error) {
	if len(vec) == 0 {
		return false, fmt.Errorf("%w: embedding vector is empty", ErrInvalidRequestBody)
	}

	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return false, err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Lock the row so the content check and the write are atomic with respect
	// to a concurrent content edit: without it, under READ COMMITTED an edit
	// could commit between the check and the insert and leave the new text
	// with the old text's vector.
	current, err := tx.Memory.Query().
		Where(memory.ID(memoryID)).
		Modify(lockRowForUpdate).
		Only(ctx)
	if err != nil {
		tx.Rollback()
		if ent.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if current.Scope == memory.ScopeSummary || current.Content != content {
		tx.Rollback()
		return false, nil
	}

	if _, err := tx.Embedding.Delete().Where(embedding.HasMemoryWith(memory.ID(memoryID))).Exec(ctx); err != nil {
		d.logger.Error(i18n.T1("delete.failed", "Entity", "embedding"), zap.Error(err))
		tx.Rollback()
		return false, err
	}
	// Key the row by the memory ID, as import does: concurrent writers for the
	// same memory (a save racing the startup backfill, or two instances
	// backfilling) then conflict on the primary key and upsert instead of
	// leaving duplicate rows, without relying on a unique embedding_memory
	// constraint that older databases may lack.
	if err := tx.Embedding.Create().
		SetID(memoryID).
		SetEmbedding(pgvector.NewVector(vec)).
		SetMemoryID(memoryID).
		OnConflictColumns(embedding.FieldID).
		UpdateNewValues().
		Exec(ctx); err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "embedding"), zap.Error(err))
		tx.Rollback()
		return false, err
	}
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return false, err
	}
	return true, nil
}

// MemoryEmbeddingBackfillStats summarises one BackfillMemoryEmbeddings run.
type MemoryEmbeddingBackfillStats struct {
	// Embedded counts memories that received an embedding.
	Embedded int
	// Skipped counts memories that changed (edited, deleted, or embedded by
	// someone else) between being listed and being written.
	Skipped int
	// Failed counts memories whose embedding or write failed; they stay
	// unembedded for the next run.
	Failed int
}

// memoryEmbeddingProviderProbe is embedded when every memory in a backfill
// page failed, to tell "the provider is down" (probe fails too: stop) from
// "these rows are bad input" (probe succeeds: skip them and keep going).
const memoryEmbeddingProviderProbe = "memory embedding backfill probe"

// BackfillMemoryEmbeddings embeds every active, non-Summary memory that has no
// Embedding row, batchSize at a time, using createEmbeddings (one vector per
// input, in input order). It is idempotent: a memory that gets an embedding
// drops out of ListMemoriesMissingEmbedding, so re-running only picks up what
// is still missing.
//
// The run walks a keyset cursor, so rows that fail are passed over rather than
// re-read: a block of memories that never embed cannot stall the rows behind
// them. When a batch embedding call fails, each memory in it is retried on its
// own so one bad input cannot sink its neighbours, and the IDs that still fail
// are logged. If every memory in a page fails, a trivial probe input decides
// whether the provider is unavailable (the run stops with that error, leaving
// the rest for the next run) or the page is just bad input (skipped).
func (d *Datastore) BackfillMemoryEmbeddings(ctx context.Context, batchSize int, createEmbeddings MemoryImportBatchEmbeddingFunc) (MemoryEmbeddingBackfillStats, error) {
	var stats MemoryEmbeddingBackfillStats
	if createEmbeddings == nil {
		return stats, fmt.Errorf("memory embedding backfill: no embedding function")
	}
	if batchSize <= 0 {
		batchSize = DefaultMemoryEmbeddingBackfillBatchSize
	}

	var afterCreatedAt time.Time
	var afterID uuid.UUID
	for {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		candidates, err := d.ListMemoriesMissingEmbedding(ctx, afterCreatedAt, afterID, batchSize)
		if err != nil {
			return stats, err
		}
		if len(candidates) == 0 {
			return stats, nil
		}
		last := candidates[len(candidates)-1]
		afterCreatedAt, afterID = last.CreatedAt, last.MemoryID

		vectors, err := embedMemoryCandidates(ctx, candidates, createEmbeddings)
		if err != nil {
			d.logger.Warn("memory embedding backfill: batch embedding failed; retrying one by one",
				zap.Int("batch_size", len(candidates)),
				zap.Error(err))
			var providerErr error
			vectors, providerErr = d.embedMemoryCandidatesOneByOne(ctx, candidates, createEmbeddings)
			if providerErr != nil {
				stats.Failed += len(candidates)
				return stats, fmt.Errorf("memory embedding backfill: embedding provider unavailable: %w", providerErr)
			}
		}

		var failedIDs []string
		for i, c := range candidates {
			if len(vectors[i]) == 0 {
				stats.Failed++
				failedIDs = append(failedIDs, c.MemoryID.String())
				continue
			}
			wrote, err := d.SetMemoryEmbedding(ctx, c.MemoryID, c.Content, vectors[i])
			switch {
			case err != nil:
				d.logger.Warn("memory embedding backfill: failed to store embedding",
					zap.String("memory_id", c.MemoryID.String()),
					zap.Error(err))
				stats.Failed++
			case wrote:
				stats.Embedded++
			default:
				stats.Skipped++
			}
		}
		if len(failedIDs) > 0 {
			d.logger.Warn("memory embedding backfill: memories failed to embed; skipped until the next run",
				zap.Strings("memory_ids", failedIDs))
		}

		if len(candidates) < batchSize {
			return stats, nil
		}
	}
}

// embedMemoryCandidatesOneByOne embeds each candidate in its own call. A
// failed candidate gets a nil vector. It returns an error only when every
// candidate failed and the provider probe fails too, i.e. the provider itself
// is unavailable.
func (d *Datastore) embedMemoryCandidatesOneByOne(ctx context.Context, candidates []models.MemoryEmbeddingCandidate, createEmbeddings MemoryImportBatchEmbeddingFunc) ([][]float32, error) {
	vectors := make([][]float32, len(candidates))
	succeeded := 0
	for i, c := range candidates {
		one, err := embedMemoryCandidates(ctx, []models.MemoryEmbeddingCandidate{c}, createEmbeddings)
		if err != nil {
			d.logger.Debug("memory embedding backfill: embedding failed",
				zap.String("memory_id", c.MemoryID.String()),
				zap.Error(err))
			continue
		}
		vectors[i] = one[0]
		succeeded++
	}
	if succeeded == 0 {
		probe, err := createEmbeddings(ctx, []string{memoryEmbeddingProviderProbe})
		if err == nil && (len(probe) != 1 || len(probe[0]) == 0) {
			err = fmt.Errorf("create embeddings: empty probe response")
		}
		if err != nil {
			return nil, err
		}
	}
	return vectors, nil
}

func embedMemoryCandidates(ctx context.Context, candidates []models.MemoryEmbeddingCandidate, createEmbeddings MemoryImportBatchEmbeddingFunc) ([][]float32, error) {
	inputs := make([]string, len(candidates))
	for i, c := range candidates {
		inputs[i] = c.Content
	}
	vectors, err := createEmbeddings(ctx, inputs)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(inputs) {
		return nil, fmt.Errorf("create embeddings: got %d vectors for %d memories", len(vectors), len(inputs))
	}
	for i, v := range vectors {
		if len(v) == 0 {
			return nil, fmt.Errorf("create embeddings: empty vector for memory %s", candidates[i].MemoryID)
		}
	}
	return vectors, nil
}

// memoryContentChanged reports whether two memory contents differ once
// surrounding whitespace is ignored (API writes store trimmed content; older
// rows may not be).
func memoryContentChanged(stored, next string) bool {
	return strings.TrimSpace(stored) != strings.TrimSpace(next)
}
