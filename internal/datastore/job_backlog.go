package datastore

import (
	"context"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/job"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// jobBacklogSampleTimeout bounds the backlog query so a slow database can't stall an export.
const jobBacklogSampleTimeout = 10 * time.Second

// nonTerminalJobStatuses are the job statuses that still have work ahead of them.
var nonTerminalJobStatuses = []job.Status{
	job.StatusPending,
	job.StatusProcessing,
	job.StatusInferenceComplete,
	job.StatusExpressionComplete,
	job.StatusCompactionComplete,
}

// JobBacklogGroup is the count and oldest creation time of unfinished jobs sharing a type and
// status.
type JobBacklogGroup struct {
	JobType  string
	Status   string
	Count    int
	OldestAt time.Time
}

// JobBacklog groups unfinished jobs by type and status. One grouped count (filtered on the
// indexed status column) plus one oldest-row lookup per group; there are only a handful of
// groups, and orphaned jobs are failed at startup, so the scanned set stays small.
func (d *Datastore) JobBacklog(ctx context.Context) ([]JobBacklogGroup, error) {
	var rows []struct {
		JobType string `json:"job_type"`
		Status  string `json:"status"`
		Count   int    `json:"count"`
	}
	err := d.dbClient.Job.Query().
		Where(job.StatusIn(nonTerminalJobStatuses...)).
		GroupBy(job.FieldJobType, job.FieldStatus).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make([]JobBacklogGroup, 0, len(rows))
	for _, r := range rows {
		g := JobBacklogGroup{JobType: r.JobType, Status: r.Status, Count: r.Count}
		oldest, err := d.dbClient.Job.Query().
			Where(job.JobTypeEQ(r.JobType), job.StatusEQ(job.Status(r.Status))).
			Order(job.ByCreatedAt()).
			Select(job.FieldCreatedAt).
			First(ctx)
		if err == nil && oldest != nil {
			g.OldestAt = oldest.CreatedAt
		}
		out = append(out, g)
	}
	return out, nil
}

// RegisterJobBacklogGauges samples telemetry.JobsBacklog and telemetry.JobsOldestAge from
// JobBacklog at each metrics export. Every API instance reports the same database-wide values,
// so dashboards should take the max across instances rather than the sum.
func (d *Datastore) RegisterJobBacklogGauges(m *telemetry.Metrics) (func() error, error) {
	return m.RegisterGauges([]telemetry.Gauge{telemetry.JobsBacklog, telemetry.JobsOldestAge},
		func(ctx context.Context, report telemetry.GaugeObserver) error {
			ctx, cancel := context.WithTimeout(ctx, jobBacklogSampleTimeout)
			defer cancel()
			groups, err := d.JobBacklog(ctx)
			if err != nil {
				d.logger.Warn("failed to sample job backlog", zap.Error(err))
				return nil
			}
			now := time.Now()
			for _, g := range groups {
				attrs := []attribute.KeyValue{telemetry.AttrJobType.String(g.JobType), telemetry.AttrStatus.String(g.Status)}
				report(telemetry.JobsBacklog, float64(g.Count), attrs...)
				if !g.OldestAt.IsZero() {
					report(telemetry.JobsOldestAge, now.Sub(g.OldestAt).Seconds(), attrs...)
				}
			}
			return nil
		})
}
