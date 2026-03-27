package rapid

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// DeltaIndex is an in-memory trigram index of files whose working tree
// content differs from the base zoekt index.
type DeltaIndex struct {
	// Trigram index for dirty file contents.
	Trigrams TrigramIndex

	// File contents keyed by repo-relative path.
	Files map[string][]byte

	// Paths deleted from the working tree that exist in the base index.
	Tombstones map[string]bool

	// All dirty paths (union of Files keys and Tombstones keys).
	DirtyPaths map[string]bool
}

// DeltaMatch is a single search match from the delta index.
type DeltaMatch struct {
	Path       string
	LineNumber int
	Line       string
	// Byte offsets of the match within Line.
	MatchStart int
	MatchEnd   int
}

// BuildDeltaIndex reads dirty files from disk and builds a delta index.
// repoPath is the absolute path to the repo root.
// dirty is the list of dirty files from git status.
func BuildDeltaIndex(repoPath string, dirty []DirtyFile) *DeltaIndex {
	idx := &DeltaIndex{
		Trigrams:   NewTrigramIndex(),
		Files:      make(map[string][]byte),
		Tombstones: make(map[string]bool),
		DirtyPaths: make(map[string]bool),
	}

	for _, f := range dirty {
		idx.DirtyPaths[f.Path] = true

		switch f.Status {
		case FileDeleted:
			idx.Tombstones[f.Path] = true

		default:
			// Read file content from working tree.
			absPath := filepath.Join(repoPath, f.Path)
			data, err := os.ReadFile(absPath)
			if err != nil {
				// File might have been deleted between git status and now.
				idx.Tombstones[f.Path] = true
				continue
			}

			// Skip binary files (contains null byte in first 8KB).
			if isBinary(data) {
				continue
			}

			idx.Files[f.Path] = data
			idx.Trigrams.Add(f.Path, data)
		}
	}

	return idx
}

// Search queries the delta index with a regex pattern and returns matches.
// If query is non-nil, file filters from it are applied.
func (d *DeltaIndex) Search(pattern string, query *ParsedQuery) ([]DeltaMatch, error) {
	if d == nil || len(d.Files) == 0 {
		return nil, nil
	}

	if pattern == "" {
		return nil, nil
	}

	// Apply case-insensitivity if the query doesn't require case sensitivity.
	compilePattern := pattern
	if query != nil && !query.CaseSensitive {
		compilePattern = "(?i)" + pattern
	}

	re, err := regexp.Compile(compilePattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	// Extract trigrams from the pattern for candidate filtering.
	// For regex patterns, we extract trigrams from literal parts only.
	literals := extractLiterals(pattern)
	var queryTrigrams []Trigram
	for _, lit := range literals {
		queryTrigrams = append(queryTrigrams, ExtractTrigramsFromString(lit)...)
	}

	// Get candidate files.
	var candidates []string
	if len(queryTrigrams) > 0 {
		candidates = d.Trigrams.Candidates(queryTrigrams)
	} else {
		// No trigrams extractable — must scan all files.
		candidates = make([]string, 0, len(d.Files))
		for path := range d.Files {
			candidates = append(candidates, path)
		}
	}

	// Full regex verification.
	var matches []DeltaMatch
	for _, path := range candidates {
		// Apply file filter if present.
		if query != nil && !query.MatchesFileFilter(path) {
			continue
		}

		data, ok := d.Files[path]
		if !ok {
			continue
		}

		fileMatches := searchInFile(path, data, re)
		matches = append(matches, fileMatches...)
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Path != matches[j].Path {
			return matches[i].Path < matches[j].Path
		}
		return matches[i].LineNumber < matches[j].LineNumber
	})

	return matches, nil
}

// IsDirty returns true if the given path is in the dirty set.
func (d *DeltaIndex) IsDirty(path string) bool {
	if d == nil {
		return false
	}
	return d.DirtyPaths[path]
}

// IsTombstoned returns true if the given path was deleted.
func (d *DeltaIndex) IsTombstoned(path string) bool {
	if d == nil {
		return false
	}
	return d.Tombstones[path]
}

func searchInFile(path string, data []byte, re *regexp.Regexp) []DeltaMatch {
	var matches []DeltaMatch
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		locs := re.FindAllStringIndex(line, -1)
		for _, loc := range locs {
			matches = append(matches, DeltaMatch{
				Path:       path,
				LineNumber: lineNum,
				Line:       line,
				MatchStart: loc[0],
				MatchEnd:   loc[1],
			})
		}
	}
	return matches
}

// extractLiterals extracts literal string fragments from a regex pattern.
// This is a simple heuristic — it finds runs of non-metacharacters.
func extractLiterals(pattern string) []string {
	var literals []string
	var current []byte
	escaped := false

	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if escaped {
			// Escaped character — only include if it's a literal escape.
			if isRegexMeta(c) {
				current = append(current, c)
			} else {
				// Special escape like \d, \w — flush current.
				if len(current) >= 3 {
					literals = append(literals, string(current))
				}
				current = current[:0]
			}
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if isRegexMeta(c) {
			if len(current) >= 3 {
				literals = append(literals, string(current))
			}
			current = current[:0]
			continue
		}
		current = append(current, c)
	}
	if len(current) >= 3 {
		literals = append(literals, string(current))
	}
	return literals
}

func isRegexMeta(c byte) bool {
	switch c {
	case '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|', '^', '$':
		return true
	}
	return false
}

func isBinary(data []byte) bool {
	check := data
	if len(check) > 8192 {
		check = check[:8192]
	}
	return bytes.ContainsRune(check, 0)
}
