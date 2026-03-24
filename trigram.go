package rapid

// Trigram is a 3-byte sequence used for fast candidate filtering.
type Trigram uint32

// ExtractTrigrams returns all unique trigrams in data.
func ExtractTrigrams(data []byte) []Trigram {
	if len(data) < 3 {
		return nil
	}

	seen := make(map[Trigram]bool)
	var trigrams []Trigram

	for i := 0; i <= len(data)-3; i++ {
		t := Trigram(uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2]))
		if !seen[t] {
			seen[t] = true
			trigrams = append(trigrams, t)
		}
	}
	return trigrams
}

// ExtractTrigramsFromString extracts trigrams from a string pattern.
// This is used for query-side trigram extraction — given a literal search
// string, find the trigrams that must appear in any matching file.
func ExtractTrigramsFromString(s string) []Trigram {
	return ExtractTrigrams([]byte(s))
}

// TrigramIndex maps trigrams to the set of file paths that contain them.
type TrigramIndex map[Trigram]map[string]bool

// NewTrigramIndex creates an empty trigram index.
func NewTrigramIndex() TrigramIndex {
	return make(TrigramIndex)
}

// Add indexes a file's content into the trigram index.
func (idx TrigramIndex) Add(path string, data []byte) {
	for _, t := range ExtractTrigrams(data) {
		if idx[t] == nil {
			idx[t] = make(map[string]bool)
		}
		idx[t][path] = true
	}
}

// Candidates returns file paths that contain all given trigrams.
// If trigrams is empty, returns all indexed paths.
func (idx TrigramIndex) Candidates(trigrams []Trigram) []string {
	if len(trigrams) == 0 {
		return idx.AllPaths()
	}

	// Start with the smallest posting list.
	smallest := trigrams[0]
	for _, t := range trigrams[1:] {
		if len(idx[t]) < len(idx[smallest]) {
			smallest = t
		}
	}

	candidates := make(map[string]bool)
	for path := range idx[smallest] {
		candidates[path] = true
	}

	// Intersect with remaining trigrams.
	for _, t := range trigrams {
		if t == smallest {
			continue
		}
		postings := idx[t]
		for path := range candidates {
			if !postings[path] {
				delete(candidates, path)
			}
		}
		if len(candidates) == 0 {
			return nil
		}
	}

	result := make([]string, 0, len(candidates))
	for path := range candidates {
		result = append(result, path)
	}
	return result
}

// AllPaths returns all file paths in the index.
func (idx TrigramIndex) AllPaths() []string {
	seen := make(map[string]bool)
	for _, files := range idx {
		for path := range files {
			seen[path] = true
		}
	}
	result := make([]string, 0, len(seen))
	for path := range seen {
		result = append(result, path)
	}
	return result
}
