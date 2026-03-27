package rapid

import (
	"regexp"
	"strings"
)

// ParsedQuery holds the components extracted from a zoekt query string.
type ParsedQuery struct {
	// Pattern is the regex/literal portion of the query to search for.
	Pattern string

	// FilePatterns are file: filters to restrict matches.
	FilePatterns []string

	// RepoPatterns are repo: filters.
	RepoPatterns []string

	// CaseSensitive is true if case:yes was specified.
	CaseSensitive bool
}

// ParseZoektQuery extracts the search pattern and filters from a zoekt query string.
// Zoekt query syntax supports tokens like:
//
//	repo:pattern  — filter by repository name
//	file:pattern  — filter by file path
//	lang:name     — filter by language
//	case:yes/no   — case sensitivity
//	-repo:pattern — negative filter
//	-file:pattern — negative filter
//
// Everything else is the search pattern (possibly joined with spaces).
func ParseZoektQuery(q string) ParsedQuery {
	var pq ParsedQuery
	var patternParts []string

	tokens := tokenizeQuery(q)
	for _, tok := range tokens {
		lower := strings.ToLower(tok)

		switch {
		case strings.HasPrefix(lower, "repo:"):
			pq.RepoPatterns = append(pq.RepoPatterns, tok[5:])
		case strings.HasPrefix(lower, "-repo:"):
			// Negative repo filter — not used for delta, skip.
		case strings.HasPrefix(lower, "r:"):
			pq.RepoPatterns = append(pq.RepoPatterns, tok[2:])
		case strings.HasPrefix(lower, "file:") || strings.HasPrefix(lower, "f:"):
			idx := strings.IndexByte(tok, ':')
			pq.FilePatterns = append(pq.FilePatterns, tok[idx+1:])
		case strings.HasPrefix(lower, "-file:") || strings.HasPrefix(lower, "-f:"):
			// Negative file filter — not used for delta, skip.
		case strings.HasPrefix(lower, "lang:"):
			// Language filter — not applicable to delta search, skip.
		case strings.HasPrefix(lower, "-lang:"):
			// skip
		case strings.HasPrefix(lower, "case:"):
			pq.CaseSensitive = strings.ToLower(tok[5:]) == "yes"
		case strings.HasPrefix(lower, "sym:"):
			// Symbol search — use the symbol name as the pattern.
			patternParts = append(patternParts, tok[4:])
		case strings.HasPrefix(lower, "content:"):
			patternParts = append(patternParts, tok[8:])
		default:
			patternParts = append(patternParts, tok)
		}
	}

	pq.Pattern = strings.Join(patternParts, " ")
	return pq
}

// tokenizeQuery splits a zoekt query into tokens, respecting quoted strings.
func tokenizeQuery(q string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false

	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == ' ' && !inQuote:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// MatchesFileFilter returns true if the given file path matches any of the
// file patterns. If there are no file patterns, everything matches.
func (pq *ParsedQuery) MatchesFileFilter(filePath string) bool {
	if len(pq.FilePatterns) == 0 {
		return true
	}
	for _, pat := range pq.FilePatterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			continue
		}
		if re.MatchString(filePath) {
			return true
		}
	}
	return false
}
