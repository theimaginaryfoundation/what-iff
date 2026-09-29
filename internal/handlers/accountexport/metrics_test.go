package accountexport

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
	"go.uber.org/zap"
)

// Waiting for a busy import slot is recorded on JobQueueWait for account_import.
func TestAcquireImportSlotRecordsQueueWait(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	h := &Handler{imports: make(chan struct{}, 1)}

	releaseFirst, err := h.acquireImportSlot(context.Background())
	require.NoError(t, err)
	acquired := make(chan func())
	go func() {
		release, _ := h.acquireImportSlot(context.Background())
		acquired <- release
	}()

	const hold = 50 * time.Millisecond
	time.Sleep(hold)
	select {
	case <-acquired:
		t.Fatal("second import acquired a slot while the only one was held")
	default:
	}
	releaseFirst()
	releaseSecond := <-acquired
	releaseSecond()

	jt := telemetry.AttrJobType.String(models.JobTypeAccountImport)
	require.Equal(t, uint64(2), tm.HistogramCount(t, telemetry.JobQueueWait.Name, jt))
	require.GreaterOrEqual(t, tm.HistogramSum(t, telemetry.JobQueueWait.Name, jt), hold.Seconds())
}

// A slot wait gives up when its context ends, and the wait is still recorded.
func TestAcquireImportSlotGivesUpWhenContextEnds(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	h := &Handler{imports: make(chan struct{}, 1)}
	releaseHeld, err := h.acquireImportSlot(context.Background())
	require.NoError(t, err)
	defer releaseHeld()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	release, err := h.acquireImportSlot(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, release)
	require.Len(t, h.imports, 1, "the held slot is untouched")
	require.Equal(t, uint64(2), tm.HistogramCount(t, telemetry.JobQueueWait.Name,
		telemetry.AttrJobType.String(models.JobTypeAccountImport)))
}

// An account import that can't get a slot within its timeout ends as a failed job with a
// timeout outcome instead of waiting forever, and leaves the busy slot alone.
func TestRunAccountImportTimesOutWaitingForSlot(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	// No sqlmock expectations: the failed-status writes error and are logged, which is fine here.
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	defer client.Close()
	ds, err := datastore.NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	h := &Handler{ds: ds, logger: zap.NewNop(), imports: make(chan struct{}, 1), importTimeout: 30 * time.Millisecond}
	h.imports <- struct{}{} // another import holds the only slot and never finishes

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.runAccountImport(uuid.New(), uuid.New(), filepath.Join(t.TempDir(), "missing.zip"), nil)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("account import kept waiting for a slot past its timeout")
	}

	jt := telemetry.AttrJobType.String(models.JobTypeAccountImport)
	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.JobDuration.Name, jt,
		telemetry.AttrOutcome.String(telemetry.JobOutcomeTimeout)))
	require.Equal(t, int64(0), tm.CounterValue(t, telemetry.JobsInFlight.Name, jt))
	require.Len(t, h.imports, 1, "the busy slot is untouched")
}

// A server shutdown reaches an import that is still queued: it stops waiting, ends as a failed job
// with a cancelled outcome (not a timeout), and leaves the busy slot alone.
func TestRunAccountImportStopsWhenServerShutsDown(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	defer client.Close()
	ds, err := datastore.NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	lifecycle, shutdown := context.WithCancel(context.Background())
	// A long timeout, so only the shutdown can end the wait.
	h := &Handler{ds: ds, logger: zap.NewNop(), imports: make(chan struct{}, 1), importTimeout: time.Hour, lifecycleCtx: lifecycle}
	h.imports <- struct{}{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.runAccountImport(uuid.New(), uuid.New(), filepath.Join(t.TempDir(), "missing.zip"), nil)
	}()
	time.Sleep(20 * time.Millisecond) // let it start waiting for the slot
	shutdown()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("account import ignored the server shutting down")
	}

	jt := telemetry.AttrJobType.String(models.JobTypeAccountImport)
	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.JobDuration.Name, jt,
		telemetry.AttrOutcome.String(telemetry.JobOutcomeCancelled)))
	require.Len(t, h.imports, 1, "the busy slot is untouched")
}

func TestBackgroundContextDefaultsToBackground(t *testing.T) {
	require.NoError(t, (&Handler{}).backgroundContext().Err())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, (&Handler{lifecycleCtx: ctx}).backgroundContext().Err())
}

func TestRecordAccountImportItems(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	recordAccountImportItems(context.Background(), telemetry.Global(), models.AccountImportResult{
		Conversations: models.ImportResult{Imported: 3, Skipped: 1, Errors: []string{"x"}},
		Personalities: models.SectionImportCounts{Created: 2},
	})

	op := telemetry.AttrOperation.String(telemetry.FileOpAccountImport)
	sum := func(kind, outcome string) float64 {
		return tm.HistogramSum(t, telemetry.FileOperationItems.Name, op,
			telemetry.AttrKind.String(kind), telemetry.AttrOutcome.String(outcome))
	}
	require.Equal(t, 3.0, sum(telemetry.FileItemConversation, telemetry.FileItemImported))
	require.Equal(t, 1.0, sum(telemetry.FileItemConversation, telemetry.FileItemSkipped))
	require.Equal(t, 1.0, sum(telemetry.FileItemConversation, telemetry.FileItemFailed))
	require.Equal(t, 2.0, sum(telemetry.FileItemPersonality, telemetry.FileItemImported))
	require.ElementsMatch(t, []string{telemetry.FileItemConversation, telemetry.FileItemPersonality},
		tm.AttributeValues(t, telemetry.FileOperationItems.Name, telemetry.AttrKind))
}

func TestRecordAccountExportItemsUsesBoundedKinds(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	recordAccountExportItems(context.Background(), telemetry.Global(),
		map[string]int{"conversations": 4, "personalities": 1, "files": 7, "unexpected": 9})

	require.ElementsMatch(t,
		[]string{telemetry.FileItemConversation, telemetry.FileItemPersonality, telemetry.FileItemFile},
		tm.AttributeValues(t, telemetry.FileOperationItems.Name, telemetry.AttrKind))
	require.Equal(t, 12.0, tm.HistogramSum(t, telemetry.FileOperationItems.Name,
		telemetry.AttrOutcome.String(telemetry.FileItemExported)))
}
