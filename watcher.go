package main

import (
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher uses fsnotify to react to file changes instantly,
// triggering a poll for the affected repo without waiting for
// the next 2s poll cycle.
type Watcher struct {
	poller  *Poller
	state   *StateTable
	watcher *fsnotify.Watcher

	mu       sync.Mutex
	repoDirs map[string]bool // watched repo paths
}

func NewWatcher(poller *Poller, state *StateTable) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		poller:   poller,
		state:    state,
		watcher:  fsw,
		repoDirs: make(map[string]bool),
	}, nil
}

// Sync adds/removes watches to match the current set of managed repos.
func (w *Watcher) Sync() {
	w.mu.Lock()
	defer w.mu.Unlock()

	current := make(map[string]bool)
	for _, path := range w.state.Paths() {
		current[path] = true
	}

	// Add new repos.
	for path := range current {
		if !w.repoDirs[path] {
			if err := w.watcher.Add(path); err != nil {
				log.Printf("[watcher] failed to watch %s: %v", path, err)
			}
		}
	}

	// Remove stale repos.
	for path := range w.repoDirs {
		if !current[path] {
			w.watcher.Remove(path)
		}
	}

	w.repoDirs = current
}

// Run processes fsnotify events and triggers repo polls. Blocks until closed.
func (w *Watcher) Run() {
	// Debounce: batch rapid edits (e.g. save-all) into one poll per repo.
	pending := make(map[string]time.Time)
	var mu sync.Mutex
	debounce := 50 * time.Millisecond

	// Flush goroutine checks pending repos and polls them.
	go func() {
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			now := time.Now()
			var ready []string
			for repo, deadline := range pending {
				if now.After(deadline) {
					ready = append(ready, repo)
				}
			}
			for _, repo := range ready {
				delete(pending, repo)
			}
			mu.Unlock()

			for _, repo := range ready {
				w.poller.pollRepo(repo)
			}
		}
	}()

	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			// Skip .git internal changes.
			if strings.Contains(event.Name, "/.git/") {
				continue
			}
			// Find which repo this file belongs to.
			repo := w.repoForPath(event.Name)
			if repo == "" {
				continue
			}
			mu.Lock()
			pending[repo] = time.Now().Add(debounce)
			mu.Unlock()

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[watcher] error: %v", err)
		}
	}
}

// Close shuts down the watcher.
func (w *Watcher) Close() error {
	return w.watcher.Close()
}

// repoForPath finds the managed repo that contains the given file path.
func (w *Watcher) repoForPath(path string) string {
	w.mu.Lock()
	defer w.mu.Unlock()

	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}

	for repo := range w.repoDirs {
		if strings.HasPrefix(abs, repo+"/") || abs == repo {
			return repo
		}
	}
	return ""
}
