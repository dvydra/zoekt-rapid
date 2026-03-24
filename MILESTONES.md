# zoekt-rapid Milestones

## Milestone 1: Go project scaffold + repo discovery

**Goal:** A runnable Go binary that discovers git repos under configured roots and prints them.

**Scope:**
- `go mod init` with Go project structure
- Config struct with defaults (roots, scan depth, exclude patterns, ports, polling intervals)
- TOML config file parsing (optional config file, sensible defaults)
- Repo discovery: walk configured roots up to `scan_depth`, find `.git` directories
- Exclude pattern matching (glob against repo path)
- CLI entry point that runs discovery once and prints found repos
- Unit tests for discovery and config

**Deliverables:**
- `go.mod`, `main.go`, `config.go`, `discovery.go` (or similar)
- `zoekt-rapid discover` prints all found repos
- Tests for discovery logic and exclude patterns

**Acceptance:** Run `zoekt-rapid discover` against `~/src` and see a correct list of repos.

---

## Milestone 2: Repo state polling

**Goal:** Continuously poll discovered repos for branch, HEAD SHA, and dirty files.

**Scope:**
- `RepoState` struct as described in plan
- Git subprocess helpers: `rev-parse --abbrev-ref HEAD HEAD`, `status --porcelain=v2`
- 10s timeout per git call
- Polling loop: every 2s, poll all managed repos
- Detect branch/HEAD changes vs previous poll
- Parse `git status --porcelain=v2` into dirty file list (modified, added, deleted, untracked)
- Repo state table (in-memory map of repo path → RepoState)
- Log state changes (branch switch, HEAD move, dirty file count changes)
- Unit tests for git output parsing

**Deliverables:**
- `git.go` (subprocess helpers), `state.go` (repo state table), `poller.go`
- `zoekt-rapid poll` runs the polling loop with log output
- Tests for porcelain v2 parsing and state change detection

**Acceptance:** Edit a file in a repo under `~/src`, see the poller log the dirty file within 2s. Switch branches, see it detect the branch change.

---

## Milestone 3: Delta index (trigram + search)

**Goal:** In-memory trigram index for dirty files that can answer regex queries.

**Scope:**
- Trigram extraction from file contents
- `DeltaIndex` struct: trigram postings, file contents, tombstones, dirty paths
- Build delta index from dirty file list (read file contents from working tree)
- Trigram candidate filtering: given a regex query, extract trigrams, find candidate files
- Full regex verification against candidate file contents
- Return matches with file path, line number, line content
- Rebuild delta from scratch each poll cycle (per plan: cheaper than diffing)
- Unit tests for trigram extraction, index building, and querying

**Deliverables:**
- `trigram.go`, `delta.go`
- Tests covering: trigram extraction, index build, query with matches, query with no matches, tombstone suppression, deleted file handling

**Acceptance:** Unit tests pass. Can programmatically build a delta index from test file contents and query it.

---

## Milestone 4: HTTP proxy with zoekt fan-out + delta merge

**Goal:** HTTP server that proxies search to zoekt-webserver and merges delta results.

**Scope:**
- HTTP server on configurable port (default 6071)
- Proxy search requests to upstream zoekt-webserver (default localhost:6070)
- Parse zoekt search response
- For each repo with a delta index:
  - Query delta index for the search pattern
  - Filter zoekt results: suppress any match whose file path is in DirtyPaths
  - Merge delta results into zoekt results
- Return merged results in zoekt-compatible response format
- Wire up: discovery → polling → delta building → query merging (the full loop)
- Integration test: start proxy, verify search returns results

**Deliverables:**
- `proxy.go`, `query.go`, `server.go`
- `zoekt-rapid serve` starts the full system (discovery + polling + proxy)
- Tests for result merging logic

**Acceptance:** Start zoekt-webserver on 6070, start zoekt-rapid on 6071. Edit a file in a repo, search through zoekt-rapid, see the edit reflected in results. Search for content only in the edited file, get results. Delete a line, confirm it no longer appears.

---

## Milestone 5: Reindexing on branch/HEAD change

**Goal:** Detect branch or HEAD changes and trigger `zoekt-git-index` to rebuild base shards.

**Scope:**
- When poller detects branch or HEAD change for a repo:
  - Mark repo as stale (set status to `indexing`)
  - Destroy delta index
  - Run `zoekt-git-index` as subprocess
  - On success: update IndexedSHA, IndexedAt, set status to `idle`
  - Recompute delta from `git status`
- `max_concurrent` reindex limiting (default 2)
- Stale-result tagging during reindex gap (reindex_gap_mode: "stale" or "blackout")
- Reindex error handling: log error, keep old shards, retry at next hourly cycle
- Tests for reindex triggering logic

**Deliverables:**
- `reindex.go`
- Stale flag on search results during reindex
- Concurrent reindex limiter

**Acceptance:** Switch branches in a repo, see zoekt-rapid trigger reindex, verify new branch content is searchable after reindex completes. Verify delta is rebuilt after reindex.

---

## Milestone 6: Hourly full reindex + startup sequence

**Goal:** Consistency backstop and correct cold/warm start behavior.

**Scope:**
- Hourly reindex scheduler: unconditionally reindex all repos every 60min
  - Sequential processing to avoid disk thrash
  - Skip repos currently being reindexed
  - Reset delta indexes after reindex
- Startup sequence (per plan):
  - Scan roots for repos
  - Check existing shards: match HEAD → live immediately, stale → serve stale + queue reindex, missing → blackout + queue reindex
  - Compute deltas from `git status`
  - Start polling loop and HTTP proxy
- Delta size threshold: if dirty files exceed `max_dirty_files_per_repo` or `max_delta_bytes_per_repo`, trigger early reindex
- Graceful shutdown: finish in-flight reindex jobs

**Deliverables:**
- `scheduler.go`
- Startup sequence in `main.go`
- Graceful shutdown with signal handling

**Acceptance:** Start zoekt-rapid, verify repos with existing shards are searchable immediately. Kill and restart, verify recovery. Wait for hourly reindex (or set interval to 1m for testing).

---

## Milestone 7: Status + management API + CLI

**Goal:** Observability and manual control.

**Scope:**
- `GET /api/status` — all repo state (path, branch, HEAD, indexed SHA, indexed at, dirty file count, delta size, status)
- `POST /api/reindex` — trigger full reindex of all repos
- `POST /api/reindex/{repo}` — trigger reindex of specific repo
- `POST /api/rescan` — re-discover repos
- `next_full_reindex` timestamp in status
- CLI subcommands:
  - `zoekt-rapid status` — pretty-print status from API
  - `zoekt-rapid reindex [repo]` — trigger reindex via API
  - `zoekt-rapid rescan` — trigger rescan via API

**Deliverables:**
- Management endpoints in `server.go`
- CLI commands in `main.go`
- Tests for status endpoint

**Acceptance:** Run `zoekt-rapid status`, see all repos with their state. Trigger a reindex via CLI, see it execute.

---

## Milestone 8: Agent tool integration + CLAUDE.md

**Goal:** Drop-in replacement for the existing zoekt-search tool.

**Scope:**
- Update the zoekt-search skill/tool to point at zoekt-rapid (port 6071) instead of zoekt-webserver (port 6070)
- Verify the search API is compatible (same query format, same response shape)
- Handle stale result flags in the agent tool (re-search hint)
- Write CLAUDE.md for the zoekt-rapid project
- End-to-end testing: agent searches, edits file, searches again, sees edit

**Deliverables:**
- Updated agent tool config
- CLAUDE.md
- Manual E2E verification

**Acceptance:** Use Claude Code to search, edit a file, search again — the edit appears in results within 2s.

---

## Future (not milestoned)

From Phase 3 of the plan — only pursue if needed:
- `fsnotify` watcher for instant reaction to file saves
- Incremental delta update (diff against previous dirty set)
- Repo poll staggering for large workspaces
- Reuse zoekt's `query.RegexpQuery` trigram extraction
