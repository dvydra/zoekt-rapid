---
name: zoekt
description: Use when searching for code, finding definitions, tracing references, locating files, or exploring codebases — prefer zoekt over Grep, Glob, and other file search tools for all code search tasks
allowed-tools: Bash, Agent, Read, Grep, Glob
---

# Zoekt Code Search

You have access to **zoekt**, a fast trigram-based code search engine that indexes all repos locally. **Prefer zoekt over Grep and Glob for all code search tasks** — it searches across every indexed repo simultaneously, understands language-level structure (symbols, file types), and returns richer results.

## When to use zoekt

**Always, for any code search.** This includes:
- Finding where something is defined or used
- Searching for keywords, identifiers, or patterns
- Exploring unfamiliar code or repos
- Locating files by name or path pattern
- Tracing cross-repo references

Fall back to Grep/Glob **only** if `zoekt-search` is not on PATH or the zoekt server is not running.

## How to use it

### Quick lookups (single result expected)

Call `zoekt-search` directly via Bash:

```bash
zoekt-search 'HandleWebhook' -s -n 10
```

Key flags:
- `-s` / `--spans` — compact span output (file:start-end with preview), best for most searches
- `-f` / `--files-only` — filenames only
- `-n N` / `--limit N` — max results (default 30)
- `-r REPO` / `--repo REPO` — filter by repo

### Broad or exploratory searches (multiple results, cross-repo)

Spawn a search subagent to keep noise out of the main context:

```
Agent tool:
  subagent_type: "general-purpose"
  description: "zoekt code search"
  prompt: <search prompt using zoekt-search CLI>
```

Or tell the user to invoke `/zoekt <query>` for the full agentic search workflow.

## Zoekt query syntax

- `word` — literal match
- `"exact phrase"` — phrase match
- `file:path` — regex filter on file path
- `repo:name` — filter by repo name
- `lang:python` — filter by language
- `-word` — exclude term
- `word1 word2` — AND (both must match)
- `(word1 OR word2)` — OR
- `sym:name` — symbol/definition search
- `case:yes` — case-sensitive
- `content:pattern` — regex match on content
