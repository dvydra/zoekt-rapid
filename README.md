# zoekt-rapid

Search proxy that makes your uncommitted code searchable *instantly*. Sits in front of [zoekt-webserver](https://github.com/sourcegraph/zoekt) and adds working tree awareness — edits, new files, and deletions appear in search results within 2 seconds, no reindex required.

## The problem

Zoekt builds trigram indexes from git commits. That's great for searching across dozens of repos, but it means your working tree changes are invisible until you commit and wait for a reindex cycle. If you're iterating fast — renaming a function, adding a file, exploring unfamiliar code — the search results are always one step behind.

## How it works

```
client → zoekt-rapid :6071 → zoekt-webserver :6070 → ~/.zoekt/*.zoekt
               │
               ├── delta index   (in-memory trigram index of dirty files)
               ├── repo poller   (git status every 2s + fsnotify)
               └── reindex mgr   (zoekt-git-index on branch/HEAD change)
```

zoekt-rapid merges results from two sources:

1. **Base index** — zoekt's on-disk trigram shards (the full corpus, built by `zoekt-git-index`)
2. **Delta index** — lightweight in-memory trigram index of files modified since the last commit

On every search request, zoekt-rapid forwards the query to zoekt, then **suppresses stale results** for dirty files and **injects fresh matches** from the delta index. The merge is transparent — clients see a single unified result set.

### What triggers updates

| Event | Detection | Latency |
|-------|-----------|---------|
| File edit/create/delete | fsnotify + git status | ~50ms |
| Branch switch | git status poll (2s) | ~2s |
| New commit (HEAD change) | git status poll → reindex | seconds–minutes |
| New repo appears under `~/src` | discovery poll (60s) | ~60s |

## Install

```sh
go install github.com/dvydra/zoekt-rapid/cmd/zoekt-rapid@latest
```

Or build from source:

```sh
git clone https://github.com/dvydra/zoekt-rapid
cd zoekt-rapid
make install
```

### Prerequisites

- [zoekt](https://github.com/sourcegraph/zoekt) — `zoekt-webserver` running on `:6070` (default)
- Go 1.22+

## Usage

```sh
# Start the proxy (discovers repos under ~/src, polls, serves on :6071)
zoekt-rapid serve

# Check what it's tracking
zoekt-rapid status

# Live dashboard with change highlighting
zoekt-rapid status --live

# Trigger a reindex for a specific repo
zoekt-rapid reindex myproject

# Trigger reindex for all repos
zoekt-rapid reindex

# Re-scan for new/removed repos
zoekt-rapid rescan
```

### Flags

```sh
zoekt-rapid serve \
  --roots ~/src,~/work \     # directories to scan for git repos
  --port 6071 \              # proxy listen port
  --zoekt http://localhost:6070  # upstream zoekt URL
```

## Configuration defaults

| Setting | Default | Description |
|---------|---------|-------------|
| Roots | `~/src` | Directories to scan for git repos |
| Scan depth | 3 | How deep to look for `.git` directories |
| Proxy port | 6071 | HTTP listen port |
| Zoekt URL | `http://localhost:6070` | Upstream zoekt-webserver |
| Repo poll | 2s | How often to check each repo for changes |
| Discovery poll | 60s | How often to scan for new/removed repos |
| Full reindex | 1h | Periodic full reindex of all repos |
| Max concurrent reindex | 2 | Parallel `zoekt-git-index` jobs |
| Max dirty files | 500 | Per-repo delta threshold before early reindex |
| Max delta size | 50MB | Per-repo delta size threshold |

## How delta merge works

On each poll cycle, for each repo:
1. Run `git status --porcelain=v2` to get the dirty file list
2. Read file contents from the working tree
3. Build a trigram index of dirty files (rebuilt from scratch each cycle)

On search:
1. Forward the query to zoekt-webserver
2. For repos with deltas, suppress zoekt results for dirty file paths
3. Run the query against the delta index
4. Merge delta matches into the response

This means zoekt handles the heavy lifting (searching millions of lines across all repos) while zoekt-rapid patches in just the handful of files you've touched since your last commit.

## macOS launchd setup

To run as background services, create plist files in `~/Library/LaunchAgents/`:

```sh
# com.zoekt.rapid.plist — zoekt-rapid on :6071
# com.zoekt.serve.plist — zoekt-webserver on :6070
```

## Project layout

```
cmd/zoekt-rapid/        CLI entry point
internal/rapid/         Library code
  config.go             Configuration and defaults
  discovery.go          Git repo discovery under configured roots
  git.go                Git subprocess helpers
  state.go              Thread-safe repo state table
  poller.go             Polling loop (2s repo poll, 60s discovery)
  watcher.go            fsnotify for instant file change detection
  trigram.go            Trigram extraction and posting lists
  delta.go              Delta index build and regex search
  proxy.go              Zoekt API proxy with delta merge
  server.go             HTTP server and API endpoints
  reindex.go            Reindex manager with concurrency limiting
  scheduler.go          Periodic full reindex scheduler
```

## License

MIT
