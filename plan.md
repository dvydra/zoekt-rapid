# zoekt-delta: Multi-Repo Layered Search for ~/src

## Problem

An AI coding agent needs indexed regex search across all local repos. Zoekt provides fast trigram search, but its shards go stale when files are edited, branches are switched, or upstream changes are pulled. The agent searches for its own writes and gets nothing. It searches after a branch switch and gets results from the old branch.

We need a system that:

1. Indexes the current checkout of every repo under `~/src`.
2. Instantly reflects any working tree change (edits, staging, new files).
3. Throws away state and fully reindexes on branch switch, pull, or rebase.
4. Reindexes everything on an hourly schedule regardless.

## Definitions

**Base index:** Zoekt's on-disk trigram shards. Built by `zoekt-git-index`. Represents a known-good snapshot of a repo at a specific commit. Expensive to build (seconds to minutes per repo), cheap to query.

**Delta index:** A small in-memory trigram index of files whose working tree content differs from what's in the base index. Cheap to build (microseconds per file), cheap to query. Covers the gap between the base index and the current working tree.

**Repo state:** The tuple `(branch, HEAD SHA)` for a given repo. When either component changes, the repo's base index is invalid and must be rebuilt.

**Dirty file:** Any file where the working tree content differs from `HEAD`. Detected by `git status`. This includes: modified tracked files, staged changes, untracked files (that aren't gitignored), and deleted files.

## Architecture

```
~/src/
├── repo-a/          ← git repo, on main
├── repo-b/          ← git repo, on main
├── repo-c/          ← git repo, on feature-x (still indexed)
└── ...

                    ┌──────────────────────────────┐
                    │         zoekt-delta           │
                    │                               │
                    │  ┌─────────┐  ┌────────────┐  │
                    │  │  Repo   │  │  Reindex   │  │
                    │  │ Scanner │  │ Scheduler  │  │
                    │  └────┬────┘  └─────┬──────┘  │
                    │       │             │         │
                    │       ▼             ▼         │
                    │  ┌──────────────────────┐     │
                    │  │   Repo State Table   │     │
                    │  │                      │     │
                    │  │ repo-a: main@abc123  │     │
                    │  │   base: shards OK    │     │
                    │  │   delta: 3 files     │     │
                    │  │                      │     │
                    │  │ repo-b: main@def456  │     │
                    │  │   base: shards OK    │     │
                    │  │   delta: 0 files     │     │
                    │  └──────────┬───────────┘     │
                    │             │                  │
                    │  ┌──────────┴───────────┐      │
                    │  │    Query Engine      │      │
                    │  │  zoekt + delta merge │      │
                    │  └──────────────────────┘      │
                    │             │                  │
                    └─────────────┼──────────────────┘
                                  │
                           search results
                                  │
                    ┌─────────────┴──────────────┐
                    │     Agent Tool / CLI        │
                    └────────────────────────────┘
```

## Repo Discovery and Scanning

### Discovery

On startup and periodically (every 60s), scan configured roots for git repositories:

```
find ~/src -maxdepth 3 -name .git -type d
```

Each discovered `.git` parent is a managed repo. New repos are picked up automatically. Repos that disappear have their shards and delta state cleaned up.

The scan depth and root path are configurable. Multiple roots are supported (e.g. `~/src` and `~/work`).

### Per-Repo State

For each repo, the system tracks:

```go
type RepoState struct {
    Path        string       // absolute path, e.g. /home/user/src/repo-a
    Branch      string       // current branch name (from HEAD)
    HeadSHA     string       // current HEAD commit SHA
    IndexedSHA  string       // SHA that the base index was built from
    IndexedAt   time.Time    // when the base index was last built
    DeltaIndex  *DeltaIndex  // in-memory delta, nil if clean
    Status      RepoStatus   // idle | indexing | error
}
```

### State Polling

Every 2 seconds, for each managed repo:

```bash
# Get current branch and HEAD — single git call
git -C /path/to/repo rev-parse --abbrev-ref HEAD HEAD
# → main
# → abc123def456...

# Get dirty files — single git call
git -C /path/to/repo status --porcelain=v2
```

This gives us two signals:

1. **Branch or HEAD changed** → trigger full reindex (see below).
2. **Working tree dirty** → update delta index (see below).

For a workspace with 50 repos, this is 100 git subprocess calls every 2 seconds. Each takes ~5ms. Total: ~500ms, well within budget. If the repo count grows large, stagger polls across the interval.

## Index Lifecycle

### When Nothing Changes

Zoekt shards serve all queries. No delta index exists. This is the steady state for most repos most of the time.

### When Files Are Edited (Delta Path)

Triggered by: `git status` shows dirty files that differ from `IndexedSHA`.

1. Compute dirty set: parse `git status --porcelain=v2` output.
2. For each dirty file:
   - **Modified/added:** Read file content from working tree. Build trigram postings. Insert into delta index.
   - **Deleted:** Record path in a tombstone set (suppresses Zoekt results for this path).
3. Delta index is rebuilt from scratch on each poll cycle (it's small — rebuilding is cheaper than diffing).

The delta index for a repo is a self-contained unit:

```go
type DeltaIndex struct {
    // Trigram map for dirty file contents
    Trigrams   map[Trigram][]Posting

    // File contents, for running full regex verification after trigram candidate filtering
    Files      map[string][]byte

    // Paths where the working tree has deleted a file that exists in the base index.
    // Zoekt results for these paths must be suppressed.
    Tombstones map[string]bool

    // All dirty paths (union of Files keys and Tombstones keys).
    // Any Zoekt result matching a dirty path is suppressed in favor of delta results.
    DirtyPaths map[string]bool
}
```

### When Branch or HEAD Changes (Invalidate + Reindex)

Triggered by: polled `(branch, HEAD)` differs from `(RepoState.Branch, RepoState.HeadSHA)`.

This means one of:
- `git checkout other-branch`
- `git pull` (fast-forward or merge)
- `git rebase`
- `git commit` (HEAD moves)
- `git reset`

Response — **scorched earth for that repo:**

1. **Immediately invalidate:** Mark the repo's base index as stale. Any in-flight query that would return results from this repo's Zoekt shards must be suppressed until reindexing completes.
2. **Destroy delta index:** Set `DeltaIndex = nil`. The delta was relative to the old HEAD; it's meaningless now.
3. **Rebuild base index:**
   ```bash
   zoekt-git-index \
       -branches HEAD \
       -index ~/.zoekt \
       -repo_name "repo-a" \
       /home/user/src/repo-a
   ```
4. **Update state:** `IndexedSHA = HeadSHA`, `IndexedAt = now()`.
5. **Recompute delta:** Run `git status` again. If the working tree is dirty relative to the new HEAD, build a fresh delta index.
6. **Resume serving:** The repo is now live with the new shards + any delta.

**During reindex (the gap):** The repo is in a degraded state. Options, in order of preference:

- **Option A — stale results with warning:** Continue serving from old shards but tag results with a `stale: true` flag. The agent tool can decide whether to trust them.
- **Option B — blackout:** Return no results for this repo until reindex completes. Other repos are unaffected.

Option A is better for the agent. A branch switch usually changes a small fraction of files; most old results are still valid. But the agent tool should be aware and can re-search after reindex if needed.

**Commit (HEAD moves but branch doesn't change):** Same path. The commit introduces new content that should be searchable, and the delta for pre-commit dirty files is now wrong. Scorched earth is the simplest correct behavior. Since `zoekt-git-index` for a single repo on local disk is fast (seconds for typical repos), the downtime is brief.

### Hourly Full Reindex

Every 60 minutes, unconditionally reindex all repos:

1. For each managed repo, run `zoekt-git-index` against current HEAD.
2. Reset all delta indexes.
3. Recompute deltas from `git status`.

This catches any drift from missed events, corrupted state, or repos where the polling somehow fell behind. It's the consistency backstop.

The hourly reindex processes repos sequentially to avoid thrashing disk I/O. Repos currently being reindexed due to a branch change are skipped (they're already getting a fresh index).

## Query Path

### Query Flow

```
Agent sends: search("handleAuth", {repos: ["*"]})

1. Parse query, extract trigrams.

2. For each managed repo:
   a. If repo is in blackout (reindexing, Option B): skip.
   b. Query Zoekt shards for this repo.
   c. If repo has a delta index:
      - Query delta index.
      - Filter Zoekt results: remove any match whose file path is in DirtyPaths.
      - Merge: delta results + filtered Zoekt results.
   d. If no delta index: Zoekt results pass through unchanged.

3. Aggregate across all repos.
4. Return to agent.
```

### Result Merge Detail

For a single repo with a delta index:

```
Zoekt returns:  [file_a:10, file_b:25, file_c:42]
Delta returns:  [file_b:30]
DirtyPaths:     {file_b, file_d}

Merge:
  file_a:10  → from Zoekt (file_a not dirty, keep)
  file_b:25  → from Zoekt (file_b is dirty, SUPPRESS)
  file_b:30  → from delta (KEEP, this is current content)
  file_c:42  → from Zoekt (file_c not dirty, keep)

Final: [file_a:10, file_b:30, file_c:42]
```

`file_d` is in DirtyPaths but returned no matches from either source — it was deleted or the query doesn't match its content. Correct behavior: nothing returned.

## Configuration

```toml
# Roots to scan for git repos
roots = ["~/src"]

# How deep to look for .git directories
scan_depth = 3

# Repos to exclude (glob patterns matched against repo path)
exclude = ["*/node_modules/*", "*/vendor/*", "*/.terraform/*"]

[proxy]
listen = "localhost:6071"
zoekt_url = "http://localhost:6070"

[polling]
# How often to check each repo for state changes
repo_poll_interval = "2s"

# How often to scan for new/removed repos
discovery_interval = "60s"

[reindex]
# Full reindex of all repos
interval = "1h"

# Zoekt indexing config
data_dir = "~/.zoekt"

# Max concurrent reindex jobs (avoid disk thrash)
max_concurrent = 2

[delta]
# Trigger early reindex if delta gets too large
max_dirty_files_per_repo = 500
max_delta_bytes_per_repo = 52428800  # 50 MB

[behavior]
# What to do during reindex gap
# "stale" = serve old results with stale flag
# "blackout" = return nothing for that repo
reindex_gap_mode = "stale"
```

## API

Drop-in replacement for Zoekt webserver, same search endpoints.

```
# Search (same as Zoekt)
GET /api/search?q=handleAuth&repo=repo-a

# Search all repos
GET /api/search?q=handleAuth
```

### Management Endpoints

```
GET  /api/status
  → {
      "repos": {
        "repo-a": {
          "path": "/home/user/src/repo-a",
          "branch": "main",
          "head": "abc123",
          "indexed_sha": "abc123",
          "indexed_at": "2026-03-24T10:00:00Z",
          "dirty_files": 3,
          "delta_size_bytes": 12400,
          "status": "idle"
        },
        "repo-b": {
          "status": "indexing",
          "dirty_files": 0
        }
      },
      "next_full_reindex": "2026-03-24T11:00:00Z"
    }

POST /api/reindex
  → Trigger full reindex of all repos now.

POST /api/reindex/{repo}
  → Trigger reindex of a specific repo.

POST /api/rescan
  → Re-discover repos under configured roots.
```

## Agent Tool Integration

The agent tool points at the proxy and doesn't need to know about the layering:

```bash
# .claude/tools/search.sh
#!/bin/bash
query="$1"
curl -s "http://localhost:6071/api/search?q=$(python3 -c \
    "import urllib.parse; print(urllib.parse.quote('$query'))")" \
    | jq -r '.Result.FileMatches[] |
        "\(.Repository):\(.FileName):\(.LineMatches[].LineNumber): \(.LineMatches[].Line)"'
```

The proxy handles everything: repo discovery, delta merging, reindex triggers. The agent sees a single search endpoint that always returns current results.

## Startup Sequence

1. Scan roots for git repos.
2. For each repo, check if valid Zoekt shards exist in `data_dir`:
   - **Shards exist and match current HEAD:** Repo is live immediately. Compute delta from `git status`.
   - **Shards exist but stale:** Serve stale shards, queue reindex. Compute delta from `git status` against current HEAD (delta will be large but correct).
   - **No shards:** Queue reindex. Repo is in blackout until indexed.
3. Start reindexing queued repos (up to `max_concurrent`).
4. Start polling loop.
5. Start HTTP proxy.

Cold start with 50 repos: the proxy is serving within seconds (for repos with existing shards). Full indexing of all repos runs in background. Agent can start working immediately on already-indexed repos.

## Failure Modes

| Scenario | Behavior |
|---|---|
| `zoekt-git-index` fails for a repo | Repo stays on old shards (if any) + delta. Error logged. Retried at next hourly cycle. |
| Git subprocess hangs | 10s timeout per git call. Repo skipped for this poll cycle. |
| Repo has detached HEAD | `branch` = "HEAD" (detached). Indexed normally. Branch change detection uses SHA comparison. |
| Repo has uncommitted merge conflicts | `git status` still works. Dirty files indexed in delta as-is. |
| Repo is bare or corrupted | Detected at discovery time, excluded from management. Logged. |
| Disk full during reindex | `zoekt-git-index` fails. Old shards preserved. Delta continues working. |
| Delta index grows beyond threshold | Early reindex triggered for that repo specifically. |
| Process restart | All delta state lost (in-memory). Shards on disk survive. Startup sequence rebuilds deltas from `git status` within seconds. |

## Implementation Plan

### Phase 1: Core loop (~1500 lines Go)

- Repo discovery via `find`.
- State polling via `git rev-parse` + `git status`.
- In-memory delta index with trigram map.
- Branch/HEAD change detection → trigger `zoekt-git-index` + delta reset.
- HTTP proxy with Zoekt fan-out + delta merge.
- `git status` delta rebuild on each poll.

### Phase 2: Robustness (~500 lines)

- Hourly full reindex scheduler.
- `max_concurrent` reindex limiting.
- Stale-result tagging during reindex gap.
- Status and management API endpoints.
- Config file parsing.
- Graceful shutdown (finish in-flight reindex jobs).

### Phase 3: Performance (~500 lines)

- `fsnotify` watcher to supplement polling (react to saves instantly instead of waiting up to 2s).
- Incremental delta update (diff against previous dirty set instead of full rebuild each cycle).
- Repo poll staggering for large workspaces.
- Reuse Zoekt's `query.RegexpQuery` trigram extraction instead of reimplementing.

## Non-Goals

- **Modifying Zoekt internals.** This wraps Zoekt, not forks it.
- **Indexing non-HEAD branches.** Each repo is indexed at its current checkout. Historical branches are not searched.
- **Remote repos.** This indexes local working trees only.
- **Sub-file granularity.** Deltas replace whole files.
- **Persisting delta to disk.** It rebuilds from `git status` in seconds on restart. Not worth the complexity.


comments:
- simple endpoint with all repo state: clean, last indexed, delta count, detla date, branch in delta, etc. etc.
- cli with reindex now, check repo state, other things.
- make a milestone plan out of this. moving to ~/src/entirehq/zoekt-rapid
