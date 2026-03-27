# zoekt-vanzelf

Search proxy that makes your uncommitted code searchable *instantly*. Sits in front of [zoekt-webserver](https://github.com/sourcegraph/zoekt) and adds working tree awareness — edits, new files, and deletions appear in search results within 2 seconds, no reindex required.

## The problem

Zoekt builds trigram indexes from git commits. That's great for searching across dozens of repos, but it means your working tree changes are invisible until you commit and wait for a reindex cycle. If you're iterating fast — renaming a function, adding a file, exploring unfamiliar code — the search results are always one step behind.

## How it works

```
client → zoekt-vanzelf :6071 → zoekt-webserver :6070 → ~/.zoekt/*.zoekt
               │
               ├── delta index   (in-memory trigram index of dirty files)
               ├── repo poller   (git status every 2s + fsnotify)
               └── reindex mgr   (zoekt-git-index on branch/HEAD change)
```

zoekt-vanzelf merges results from two sources:

1. **Base index** — zoekt's on-disk trigram shards (the full corpus, built by `zoekt-git-index`)
2. **Delta index** — lightweight in-memory trigram index of files modified since the last commit

On every search request, zoekt-vanzelf forwards the query to zoekt, then **suppresses stale results** for dirty files and **injects fresh matches** from the delta index. The merge is transparent — clients see a single unified result set.

### What triggers updates

| Event | Detection | Latency |
|-------|-----------|---------|
| File edit/create/delete | fsnotify + git status | ~50ms |
| Branch switch | git status poll (2s) | ~2s |
| New commit (HEAD change) | git status poll → reindex | seconds–minutes |
| New repo appears under `~/src` | discovery poll (60s) | ~60s |

## Install

The installer sets up the full stack: zoekt, zoekt-vanzelf, initial index, and macOS launchd agents.

```sh
git clone https://github.com/dvydra/zoekt-vanzelf
cd zoekt-vanzelf
./install.sh
```

This will:
1. Install `zoekt-webserver` and `zoekt-git-index` from [sourcegraph/zoekt](https://github.com/sourcegraph/zoekt)
2. Build and install `zoekt-vanzelf`
3. Index all git repos under `~/src` (first run only)
4. Create and start launchd agents so everything runs on login and auto-restarts

After install, verify with:
```sh
zoekt-vanzelf status
```

### Prerequisites

- **Go 1.26+** — install from [go.dev](https://go.dev/dl/) or `brew install go`
- **macOS** — the installer uses launchd for service management
- **git repos under `~/src`** — this is the default root; configurable via `--roots`

### Manual install

If you prefer to install the pieces yourself:

```sh
# Install zoekt (the base search engine)
go install github.com/sourcegraph/zoekt/cmd/zoekt-webserver@latest
go install github.com/sourcegraph/zoekt/cmd/zoekt-git-index@latest

# Start zoekt-webserver
zoekt-webserver -index ~/.zoekt -listen :6070 -rpc

# Index your repos (repeat for each repo, or let zoekt-vanzelf handle it)
zoekt-git-index -index ~/.zoekt ~/src/myproject

# Install zoekt-vanzelf
go install github.com/dvydra/zoekt-vanzelf/cmd/zoekt-vanzelf@latest

# Start the proxy
zoekt-vanzelf serve
```

### Optional: web UI

[neogrok](https://github.com/nicholasgasior/neogrok) provides a browser-based search interface. Point it at zoekt-vanzelf to get live working tree search in the browser:

```sh
npm install -g neogrok
ZOEKT_URL=http://localhost:6071 PORT=3000 neogrok
```

## Usage

```sh
# Start the proxy (discovers repos under ~/src, polls, serves on :6071)
zoekt-vanzelf serve

# Check what it's tracking
zoekt-vanzelf status

# Live dashboard with change highlighting
zoekt-vanzelf status --live

# Trigger a reindex for a specific repo
zoekt-vanzelf reindex myproject

# Trigger reindex for all repos
zoekt-vanzelf reindex

# Re-scan for new/removed repos
zoekt-vanzelf rescan

# Print version
zoekt-vanzelf version
```

### Flags

```sh
zoekt-vanzelf serve \
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

This means zoekt handles the heavy lifting (searching millions of lines across all repos) while zoekt-vanzelf patches in just the handful of files you've touched since your last commit.

## macOS launchd agents

The installer creates two launchd agents in `~/Library/LaunchAgents/`:

| Agent | Label | Service | Log |
|-------|-------|---------|-----|
| `com.zoekt.serve.plist` | zoekt-webserver on `:6070` | Base trigram index | `/tmp/zoekt-serve.log` |
| `com.zoekt.vanzelf.plist` | zoekt-vanzelf on `:6071` | Proxy with live deltas | `/tmp/zoekt-vanzelf.log` |

Both are set to `RunAtLoad` + `KeepAlive` — they start on login and restart if they crash.

```sh
# Restart zoekt-vanzelf
launchctl kickstart -k gui/$(id -u)/com.zoekt.vanzelf

# Stop it
launchctl kill SIGTERM gui/$(id -u)/com.zoekt.vanzelf

# View logs
tail -f /tmp/zoekt-vanzelf.log
```

## Project layout

```
cmd/zoekt-vanzelf/        CLI entry point
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
