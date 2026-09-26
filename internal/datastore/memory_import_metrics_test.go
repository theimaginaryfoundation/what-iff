package datastore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

func TestImportMemoriesRecordsFileMetrics(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newSQLiteDatastore(t)
	defer cleanup()
	tm := telemetrytest.New(t)
	ds.metrics = tm.Metrics

	userID := uuid.New()
	_, err := ds.dbClient.User.Create().SetID(userID).SetUsername("metrics").
		SetEmail("metrics@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	zr := buildZipReaderForTest(t, map[string]string{
		"user.json": strings.Join([]string{
			memoryRecordJSON(t, models.MemoryRecord{ID: uuid.New(), Content: "one", CreatedAt: now}),
			memoryRecordJSON(t, models.MemoryRecord{ID: uuid.New(), Content: "two", CreatedAt: now}),
			`{"id":`,
		}, "\n"),
	})

	_, err = ds.ImportMemoriesWithBatchEmbeddings(ctx, userID, zr, nil, func(_ context.Context, inputs []string) ([][]float32, error) {
		out := make([][]float32, len(inputs))
		for i := range inputs {
			out[i] = []float32{1, 2, 3}
		}
		return out, nil
	})
	require.NoError(t, err)

	op := telemetry.AttrOperation.String(telemetry.FileOpMemoryImport)
	for _, stage := range []string{telemetry.FileStageParse, telemetry.FileStageEmbed, telemetry.FileStageStore} {
		require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.FileOperationDuration.Name, op, telemetry.AttrStage.String(stage)), stage)
	}
	items := func(outcome string) float64 {
		return tm.HistogramSum(t, telemetry.FileOperationItems.Name, op,
			telemetry.AttrKind.String(telemetry.FileItemMemory), telemetry.AttrOutcome.String(outcome))
	}
	require.Equal(t, 2.0, items(telemetry.FileItemImported))
	require.Equal(t, 0.0, items(telemetry.FileItemSkipped))
	require.Equal(t, 1.0, items(telemetry.FileItemFailed))
}

func TestImportMemoriesRecordsFailedEmbedStage(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newSQLiteDatastore(t)
	defer cleanup()
	tm := telemetrytest.New(t)
	ds.metrics = tm.Metrics

	userID := uuid.New()
	_, err := ds.dbClient.User.Create().SetID(userID).SetUsername("metrics").
		SetEmail("metrics@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	zr := buildZipReaderForTest(t, map[string]string{
		"user.json": memoryRecordJSON(t, models.MemoryRecord{ID: uuid.New(), Content: "one", CreatedAt: time.Now().UTC()}),
	})

	_, err = ds.ImportMemoriesWithBatchEmbeddings(ctx, userID, zr, nil, func(context.Context, []string) ([][]float32, error) {
		return nil, errors.New("embeddings down")
	})
	require.Error(t, err)

	op := telemetry.AttrOperation.String(telemetry.FileOpMemoryImport)
	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.FileOperationDuration.Name, op,
		telemetry.AttrStage.String(telemetry.FileStageEmbed), telemetry.AttrErrorType.String(telemetry.ErrorTypeOther)))
	require.Zero(t, tm.HistogramCount(t, telemetry.FileOperationDuration.Name, op, telemetry.AttrStage.String(telemetry.FileStageStore)))
	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.FileOperationItems.Name, op,
		telemetry.AttrOutcome.String(telemetry.FileItemImported)), "partial results still report counts")
}
