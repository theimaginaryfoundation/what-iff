package mcpoauth

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type Sweeper struct {
	service      *Service
	interval     time.Duration
	refreshAhead time.Duration
	maxFailures  int
}

func NewSweeper(service *Service, interval, refreshAhead time.Duration, maxFailures int) *Sweeper {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	if refreshAhead <= 0 {
		refreshAhead = 15 * time.Minute
	}
	if maxFailures <= 0 {
		maxFailures = 3
	}
	return &Sweeper{
		service:      service,
		interval:     interval,
		refreshAhead: refreshAhead,
		maxFailures:  maxFailures,
	}
}

func (s *Sweeper) Run(ctx context.Context) {
	if s == nil || s.service == nil {
		return
	}
	runOnce := func() {
		defer func() {
			if r := recover(); r != nil {
				s.service.logger.Error("oauth refresh sweeper recovered from panic",
					zap.Any("panic", r))
			}
		}()
		s.service.RefreshDueConnectors(ctx, s.refreshAhead, s.maxFailures)
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	// Run immediately once.
	runOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}
