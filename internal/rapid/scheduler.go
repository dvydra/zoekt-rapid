package rapid

import (
	"context"
	"log"
	"sync"
	"time"
)

// Scheduler handles periodic full reindexing.
type Scheduler struct {
	config  Config
	reindex *ReindexManager

	mu            sync.RWMutex
	nextReindex   time.Time
	startedAt     time.Time
}

func NewScheduler(cfg Config, reindex *ReindexManager) *Scheduler {
	return &Scheduler{
		config:      cfg,
		reindex:     reindex,
		nextReindex: time.Now().Add(cfg.ReindexInterval),
		startedAt:   time.Now(),
	}
}

// NextReindexAt returns when the next full reindex is scheduled.
func (s *Scheduler) NextReindexAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nextReindex
}

// Run starts the hourly reindex scheduler. Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.config.ReindexInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Printf("scheduled full reindex starting")
			s.reindex.ReindexAll()

			s.mu.Lock()
			s.nextReindex = time.Now().Add(s.config.ReindexInterval)
			s.mu.Unlock()
		}
	}
}
