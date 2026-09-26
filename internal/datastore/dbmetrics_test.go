package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/job"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
	"go.opentelemetry.io/otel/attribute"
)

func TestInstrumentEntClientRecordsQueriesAndMutations(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	tm := telemetrytest.New(t)
	InstrumentEntClient(ds.dbClient, tm.Metrics)
	ctx := context.Background()
	name := telemetry.DBOperationDuration.Name
	op := func(collection, operation string, extra ...attribute.KeyValue) uint64 {
		return tm.HistogramCount(t, name, append([]attribute.KeyValue{
			telemetry.AttrDBCollection.String(collection),
			telemetry.AttrDBOperation.String(operation),
		}, extra...)...)
	}

	userID := createJobTestUser(t, ds)
	require.Equal(t, uint64(1), op("user", "create"))

	// A bulk create is one sample, not one per row.
	builders := make([]*ent.JobCreate, 3)
	for i := range builders {
		builders[i] = ds.dbClient.Job.Create().SetID(uuid.New()).SetOwnerID(userID).
			SetJobType("chat_message").SetReference("ref")
	}
	jobs, err := ds.dbClient.Job.CreateBulk(builders...).Save(ctx)
	require.NoError(t, err)
	require.Len(t, jobs, 3)
	require.Equal(t, uint64(1), op("job", "create"))

	// Eager-loaded edges are part of their parent query's sample.
	got, err := ds.dbClient.Job.Query().Where(job.ReferenceEQ("ref")).WithOwner().All(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, uint64(1), op("job", "query"))
	require.Equal(t, uint64(0), op("user", "query"))

	// An empty result (ent NotFound from First) is a successful query.
	_, err = ds.dbClient.Job.Query().Where(job.ReferenceEQ("missing")).First(ctx)
	require.True(t, ent.IsNotFound(err))
	require.Equal(t, uint64(2), op("job", "query"))

	// UpdateOne on a missing row is a not_found failure; update/delete collapse their *_one forms.
	err = ds.dbClient.Job.UpdateOneID(uuid.New()).SetReference("x").Exec(ctx)
	require.True(t, ent.IsNotFound(err))
	require.Equal(t, uint64(1), op("job", "update", telemetry.AttrErrorType.String(telemetry.ErrorTypeNotFound)))
	_, err = ds.dbClient.Job.Update().Where(job.ReferenceEQ("ref")).SetReference("ref2").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, ds.dbClient.Job.DeleteOneID(jobs[0].ID).Exec(ctx))
	require.Equal(t, uint64(2), op("job", "update"))
	require.Equal(t, uint64(1), op("job", "delete"))

	// Transactions opened from the client are instrumented too.
	tx, err := ds.dbClient.Tx(ctx)
	require.NoError(t, err)
	_, err = tx.Job.Query().Count(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.Equal(t, uint64(3), op("job", "query"))

	require.ElementsMatch(t, []string{"user", "job"}, tm.AttributeValues(t, name, telemetry.AttrDBCollection))
	require.ElementsMatch(t, []string{"create", "query", "update", "delete"}, tm.AttributeValues(t, name, telemetry.AttrDBOperation))
	require.Equal(t, []string{telemetry.ErrorTypeNotFound}, tm.AttributeValues(t, name, telemetry.AttrErrorType))
}

func TestInstrumentEntClientSequentialCreatesOnOneContext(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	tm := telemetrytest.New(t)
	InstrumentEntClient(ds.dbClient, tm.Metrics)

	// The in-flight marker is released after each create, so reusing a context doesn't
	// swallow later samples.
	createJobTestUser(t, ds)
	createJobTestUser(t, ds)
	require.Equal(t, uint64(2), tm.HistogramCount(t, telemetry.DBOperationDuration.Name,
		telemetry.AttrDBCollection.String("user"), telemetry.AttrDBOperation.String("create")))
}

func TestCollectionNames(t *testing.T) {
	require.Equal(t, "chat_message_context_item", entityCollection("ChatMessageContextItem"))
	require.Equal(t, "mcp_server", entityCollection("MCPServer"))
	require.Equal(t, "user", entityCollection("User"))
	require.Equal(t, "mcp_server", queryCollection(&ent.MCPServerQuery{}))
	require.Equal(t, "file_chunk", queryCollection(&ent.FileChunkQuery{}))
}
