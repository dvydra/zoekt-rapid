package main

import (
	"context"
	"log"
	"time"
)

// Poller periodically polls all managed repos for state changes.
type Poller struct {
	config  Config
	state   *StateTable
	reindex *ReindexManager // nil if reindex not wired up (e.g. poll-only mode)
	proxy   *SearchProxy    // nil if proxy not wired up (e.g. poll-only mode)
}

func NewPoller(cfg Config, state *StateTable) *Poller {
	return &Poller{config: cfg, state: state}
}

// Run starts the polling loop. It blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	// Initial discovery + poll.
	p.discoverAndPoll()

	pollTicker := time.NewTicker(p.config.RepoPollInterval)
	defer pollTicker.Stop()

	discoveryTicker := time.NewTicker(p.config.DiscoveryInterval)
	defer discoveryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			p.pollAll()
		case <-discoveryTicker.C:
			p.discoverAndPoll()
		}
	}
}

func (p *Poller) discoverAndPoll() {
	repos, err := DiscoverRepos(p.config.Roots, p.config.ScanDepth, p.config.ExcludePatterns)
	if err != nil {
		log.Printf("discovery error: %v", err)
		return
	}

	// Remove repos that no longer exist.
	tracked := p.state.Paths()
	repoSet := make(map[string]bool, len(repos))
	for _, r := range repos {
		repoSet[r] = true
	}
	for _, path := range tracked {
		if !repoSet[path] {
			log.Printf("[%s] removed (no longer exists)", path)
			p.state.Remove(path)
		}
	}

	// Poll all discovered repos.
	for _, path := range repos {
		p.pollRepo(path)
	}
}

func (p *Poller) pollAll() {
	for _, path := range p.state.Paths() {
		p.pollRepo(path)
	}
}

func (p *Poller) pollRepo(path string) {
	bh, err := GetBranchAndHead(path)
	if err != nil {
		log.Printf("[%s] git error: %v", path, err)
		return
	}

	dirty, err := GetDirtyFiles(path)
	if err != nil {
		log.Printf("[%s] git status error: %v", path, err)
		return
	}

	change := p.state.Update(path, bh, dirty)

	needsReindex := false

	if change.BranchChanged && change.OldBranch == "" {
		// New repo — check if zoekt already has a valid shard.
		if p.proxy != nil {
			indexedSHA := p.proxy.IndexedSHA(path)
			if indexedSHA == bh.SHA {
				// Zoekt shard matches current HEAD — no reindex needed.
				p.state.SetIndexed(path, indexedSHA)
				p.state.SetStatus(path, RepoIdle)
				log.Printf("[%s] new repo on %s@%s (%d dirty) — shard current",
					path, change.NewBranch, shortSHA(change.NewSHA), change.DirtyCount)
			} else if indexedSHA != "" {
				// Shard exists but stale.
				log.Printf("[%s] new repo on %s@%s (%d dirty) — shard stale (indexed %s)",
					path, change.NewBranch, shortSHA(change.NewSHA), change.DirtyCount, shortSHA(indexedSHA))
				p.state.SetIndexed(path, indexedSHA)
				needsReindex = true
			} else {
				// No shard at all.
				log.Printf("[%s] new repo on %s@%s (%d dirty) — no shard",
					path, change.NewBranch, shortSHA(change.NewSHA), change.DirtyCount)
				needsReindex = true
			}
		} else {
			log.Printf("[%s] new repo on %s@%s (%d dirty)",
				path, change.NewBranch, shortSHA(change.NewSHA), change.DirtyCount)
		}
	} else if change.BranchChanged {
		log.Printf("[%s] branch changed: %s → %s",
			path, change.OldBranch, change.NewBranch)
		needsReindex = true
	} else if change.HeadChanged {
		log.Printf("[%s] HEAD changed: %s → %s",
			path, shortSHA(change.OldSHA), shortSHA(change.NewSHA))
		needsReindex = true
	} else if change.DirtyChanged {
		log.Printf("[%s] dirty files changed: %d files",
			path, change.DirtyCount)
	}

	if needsReindex && p.reindex != nil {
		// Destroy delta — it's relative to the old HEAD.
		p.state.SetDelta(path, nil)
		p.state.SetStatus(path, RepoStale)
		p.reindex.TriggerReindex(path)
	} else {
		// Rebuild delta index if dirty files changed.
		if len(dirty) > 0 && change.DirtyChanged {
			delta := BuildDeltaIndex(path, dirty)
			p.state.SetDelta(path, delta)

			// Check delta threshold — trigger early reindex if too large.
			if p.reindex != nil && p.deltaExceedsThreshold(len(dirty), delta) {
				log.Printf("[%s] delta exceeds threshold (%d files), triggering early reindex", path, len(dirty))
				p.reindex.TriggerReindex(path)
			}
		} else if len(dirty) == 0 {
			p.state.SetDelta(path, nil)
		}
	}
}

func (p *Poller) deltaExceedsThreshold(dirtyCount int, delta *DeltaIndex) bool {
	if dirtyCount > p.config.MaxDirtyFiles {
		return true
	}
	if delta == nil {
		return false
	}
	var totalBytes int64
	for _, data := range delta.Files {
		totalBytes += int64(len(data))
	}
	return totalBytes > p.config.MaxDeltaBytes
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
