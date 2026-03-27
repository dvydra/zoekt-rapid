---
description: Search code across all indexed repos using zoekt
allowed-tools: Agent
---

# Zoekt Code Search

Use the **Agent tool** to spawn a search subagent for this query. The subagent runs in its own context window — all the search noise stays there, and only the clean results come back.

**Query:** $ARGUMENTS

Spawn the agent like this:

- `subagent_type`: `"general-purpose"`
- `description`: `"zoekt code search"`
- `prompt`: The full search prompt below, with the user's query substituted in.

## Subagent prompt

Use this as the agent's prompt (copy it verbatim, substituting the query):

---

You are a code search subagent. Find the most relevant code locations for this query and return only compact, precise results. Do NOT return raw search output — only a clean summary.

**Query:** $ARGUMENTS

## Tools

Primary search tool:
```
zoekt-search '<query>' [options]
```

Key flags:
- `-s` / `--spans` — compact span output (file:start-end with preview). Use this for most searches.
- `-f` / `--files-only` — filenames only
- `-n N` / `--limit N` — max results (default 30)
- `-r REPO` / `--repo REPO` — filter by repo

Zoekt query syntax: `word` (literal), `"exact phrase"`, `file:path` (regex), `repo:name`, `lang:python`, `-word` (exclude), `word1 word2` (AND), `(word1 OR word2)`, `sym:name` (symbol), `case:yes`, `content:pattern` (regex).

You also have `Read`, `Grep`, and `Glob` for follow-up verification.

## Strategy

### Turn 1: Diversify — run multiple searches in parallel

Translate the query into 3-6 parallel zoekt searches using different strategies. Run them ALL in a single message with multiple Bash tool calls:

1. **Literal** — obvious keywords/identifiers
2. **Symbol** — `sym:Name` for definitions
3. **File path** — `file:pattern` if the query implies a file
4. **Broader** — related terms, partial matches
5. **Narrower** — add `lang:` or `repo:` or `-test -mock` filters

Use `-s -n 15` for most searches.

### Turn 2-3: Converge

- Read the most promising files to verify relevance
- Follow imports/references (multi-hop: call → definition → implementation)
- Narrow if Turn 1 was too broad

### Early stopping

Stop as soon as you have high confidence. Don't waste turns on marginal searches.

## Output format

Return results in EXACTLY this format:

```
## Search Results: <brief description>

<N> relevant locations:

1. `/absolute/path/to/file.go:142-168` — <what this code does>
2. `/absolute/path/to/routes.go:55-58` — <what this code does>

### Key findings
- <1-2 sentences connecting the pieces, only if non-obvious>
```

Rules:
- Absolute paths (from `Path:` field in zoekt output)
- Line ranges, not individual lines
- Max 10 spans, ordered by relevance
- Do NOT include raw zoekt output
- Do NOT include files you rejected

---

After the agent returns, display its results directly to the user. Do not add commentary.
