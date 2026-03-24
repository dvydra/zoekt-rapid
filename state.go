package main

import (
	"sync"
	"time"
)

// RepoStatus represents the current state of a repo's index.
type RepoStatus int

const (
	RepoIdle     RepoStatus = iota // index is current
	RepoIndexing                   // reindexing in progress
	RepoStale                      // index is stale (branch/HEAD changed)
	RepoError                      // last indexing attempt failed
)

func (s RepoStatus) String() string {
	switch s {
	case RepoIdle:
		return "idle"
	case RepoIndexing:
		return "indexing"
	case RepoStale:
		return "stale"
	case RepoError:
		return "error"
	default:
		return "unknown"
	}
}

// RepoState tracks the known state of a single repo.
type RepoState struct {
	Path       string
	Branch     string
	HeadSHA    string
	IndexedSHA string
	IndexedAt  time.Time
	DirtyFiles []DirtyFile
	DeltaIndex *DeltaIndex
	Status     RepoStatus
}

// StateChange describes what changed for a repo between two poll cycles.
type StateChange struct {
	Path          string
	BranchChanged bool
	OldBranch     string
	NewBranch     string
	HeadChanged   bool
	OldSHA        string
	NewSHA        string
	DirtyChanged  bool
	DirtyCount    int
}

// StateTable is a thread-safe map of repo path → RepoState.
type StateTable struct {
	mu    sync.RWMutex
	repos map[string]*RepoState
}

func NewStateTable() *StateTable {
	return &StateTable{
		repos: make(map[string]*RepoState),
	}
}

// Get returns the state for a repo, or nil if not tracked.
func (t *StateTable) Get(path string) *RepoState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s := t.repos[path]
	if s == nil {
		return nil
	}
	// Return a copy.
	cp := *s
	return &cp
}

// SetDelta sets the delta index for a repo.
func (t *StateTable) SetDelta(path string, delta *DeltaIndex) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.repos[path]; ok {
		s.DeltaIndex = delta
	}
}

// SetStatus sets the repo status.
func (t *StateTable) SetStatus(path string, status RepoStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.repos[path]; ok {
		s.Status = status
	}
}

// SetIndexed updates the indexed SHA and timestamp.
func (t *StateTable) SetIndexed(path string, sha string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.repos[path]; ok {
		s.IndexedSHA = sha
		s.IndexedAt = time.Now()
	}
}

// Update applies new poll data to a repo and returns what changed.
// If the repo is new, it is added to the table.
func (t *StateTable) Update(path string, bh BranchHead, dirty []DirtyFile) StateChange {
	t.mu.Lock()
	defer t.mu.Unlock()

	change := StateChange{
		Path:       path,
		DirtyCount: len(dirty),
	}

	existing, ok := t.repos[path]
	if !ok {
		// New repo.
		t.repos[path] = &RepoState{
			Path:       path,
			Branch:     bh.Branch,
			HeadSHA:    bh.SHA,
			DirtyFiles: dirty,
			Status:     RepoStale, // needs initial indexing
		}
		change.BranchChanged = true
		change.NewBranch = bh.Branch
		change.HeadChanged = true
		change.NewSHA = bh.SHA
		change.DirtyChanged = true
		return change
	}

	if existing.Branch != bh.Branch {
		change.BranchChanged = true
		change.OldBranch = existing.Branch
		change.NewBranch = bh.Branch
		existing.Branch = bh.Branch
	}

	if existing.HeadSHA != bh.SHA {
		change.HeadChanged = true
		change.OldSHA = existing.HeadSHA
		change.NewSHA = bh.SHA
		existing.HeadSHA = bh.SHA
	}

	if dirtySetChanged(existing.DirtyFiles, dirty) {
		change.DirtyChanged = true
	}
	existing.DirtyFiles = dirty

	return change
}

// Remove removes a repo from the table.
func (t *StateTable) Remove(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.repos, path)
}

// Paths returns all tracked repo paths.
func (t *StateTable) Paths() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	paths := make([]string, 0, len(t.repos))
	for p := range t.repos {
		paths = append(paths, p)
	}
	return paths
}

// All returns a snapshot of all repo states.
func (t *StateTable) All() map[string]RepoState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]RepoState, len(t.repos))
	for k, v := range t.repos {
		out[k] = *v
	}
	return out
}

// dirtySetChanged returns true if the dirty file sets differ.
func dirtySetChanged(old, new []DirtyFile) bool {
	if len(old) != len(new) {
		return true
	}
	oldMap := make(map[string]FileStatus, len(old))
	for _, f := range old {
		oldMap[f.Path] = f.Status
	}
	for _, f := range new {
		if oldMap[f.Path] != f.Status {
			return true
		}
	}
	return false
}
