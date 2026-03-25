# zoekt-vanzelf

Search proxy that sits in front of [zoekt-webserver](https://github.com/sourcegraph/zoekt) and adds working tree awareness. Edits, new files, and deletions are searchable within 2 seconds — no reindex needed.

## Architecture

```
neogrok :3000 → zoekt-vanzelf :6071 → zoekt-webserver :6070 → ~/.zoekt/*.zoekt
                     │
                     ├── delta index (in-memory trigram index of dirty files)
                     ├── repo poller (git status every 2s)
                     └── reindex manager (runs zoekt-git-index on branch/HEAD change)
```

zoekt-vanzelf merges results from two sources:
1. **Base index** — zoekt's on-disk trigram shards (built by `zoekt-git-index`)
2. **Delta index** — in-memory trigram index of files modified since the base index

For dirty files, zoekt results are suppressed and replaced with delta results.

## Development

```sh
make build                     # build binary to ./zoekt-vanzelf
make test                      # run tests
make install                   # install to GOBIN (needed for launchd)
go build ./cmd/zoekt-vanzelf     # or use go directly
```

## Commands

```sh
zoekt-vanzelf serve              # start proxy (discovery + polling + HTTP)
zoekt-vanzelf status             # show all repo states
zoekt-vanzelf reindex [repo]     # trigger reindex (all or specific)
zoekt-vanzelf rescan             # re-discover repos
zoekt-vanzelf discover           # one-shot repo discovery (debug)
zoekt-vanzelf poll               # run polling loop (debug)
```

## Project layout

```
cmd/zoekt-vanzelf/main.go        — CLI entry point and subcommand dispatch
internal/rapid/                 — library code (package rapid):
  config.go                     — configuration with defaults
  discovery.go                  — find git repos under configured roots
  git.go                        — git subprocess helpers (branch/HEAD, porcelain v2 parsing)
  state.go                      — thread-safe repo state table
  poller.go                     — polling loop (2s repo poll, 60s discovery)
  trigram.go                    — trigram extraction and posting list index
  delta.go                      — delta index build and regex search
  proxy.go                      — zoekt API proxy with delta merge
  server.go                     — HTTP server with search, management, and passthrough endpoints
  reindex.go                    — reindex manager with concurrency limiting
  scheduler.go                  — hourly full reindex scheduler
  watcher.go                    — fsnotify watcher for instant file change detection
```

## How delta merge works

On each poll cycle for each repo:
1. Run `git status --porcelain=v2` to get dirty files
2. Read dirty file contents from working tree
3. Build trigram index of dirty files (rebuild from scratch each cycle)

On search:
1. Forward query to zoekt-webserver
2. For each repo with a delta, query the delta index
3. Suppress zoekt results for dirty paths
4. Inject delta matches into the response

## Launchd agents

All managed via `~/Library/LaunchAgents/com.zoekt.*.plist`:
- `com.zoekt.serve` — zoekt-webserver on :6070
- `com.zoekt.rapid` — zoekt-vanzelf on :6071
- `com.zoekt.neogrok` — neogrok on :3000 (points at :6071)
- `com.zoekt.index` — periodic `zoekt.sh index` (every 15min)

## Configuration defaults

- Roots: `~/src`
- Scan depth: 3
- Proxy port: 6071
- Zoekt URL: `http://localhost:6070`
- Repo poll interval: 2s
- Discovery interval: 60s
- Reindex interval: 1h
- Max concurrent reindex: 2
- Max dirty files per repo: 500
- Max delta bytes per repo: 50MB
