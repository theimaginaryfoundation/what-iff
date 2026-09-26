package accountexport

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

// Waiting for a busy import slot is recorded on JobQueueWait for account_import.
func TestAcquireImportSlotRecordsQueueWait(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	h := &Handler{imports: make(chan struct{}, 1)}

	releaseFirst := h.acquireImportSlot(context.Background())
	acquired := make(chan func())
	go func() { acquired <- h.acquireImportSlot(context.Background()) }()

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
