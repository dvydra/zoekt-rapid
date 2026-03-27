package rapid

import (
	"testing"
)

func TestParseZoektQuery_SimplePattern(t *testing.T) {
	pq := ParseZoektQuery("handleAuth")
	if pq.Pattern != "handleAuth" {
		t.Errorf("pattern = %q, want %q", pq.Pattern, "handleAuth")
	}
	if len(pq.FilePatterns) != 0 {
		t.Errorf("unexpected file patterns: %v", pq.FilePatterns)
	}
}

func TestParseZoektQuery_WithFilters(t *testing.T) {
	pq := ParseZoektQuery("repo:myapp file:\\.go$ handleAuth")
	if pq.Pattern != "handleAuth" {
		t.Errorf("pattern = %q, want %q", pq.Pattern, "handleAuth")
	}
	if len(pq.RepoPatterns) != 1 || pq.RepoPatterns[0] != "myapp" {
		t.Errorf("repo patterns = %v, want [myapp]", pq.RepoPatterns)
	}
	if len(pq.FilePatterns) != 1 || pq.FilePatterns[0] != "\\.go$" {
		t.Errorf("file patterns = %v, want [\\.go$]", pq.FilePatterns)
	}
}

func TestParseZoektQuery_CaseSensitive(t *testing.T) {
	pq := ParseZoektQuery("case:yes Foo")
	if !pq.CaseSensitive {
		t.Error("expected case sensitive")
	}
	if pq.Pattern != "Foo" {
		t.Errorf("pattern = %q, want %q", pq.Pattern, "Foo")
	}
}

func TestParseZoektQuery_QuotedString(t *testing.T) {
	pq := ParseZoektQuery(`"hello world" file:test`)
	if pq.Pattern != "hello world" {
		t.Errorf("pattern = %q, want %q", pq.Pattern, "hello world")
	}
}

func TestParseZoektQuery_MultiplePatternWords(t *testing.T) {
	pq := ParseZoektQuery("func main")
	if pq.Pattern != "func main" {
		t.Errorf("pattern = %q, want %q", pq.Pattern, "func main")
	}
}

func TestParseZoektQuery_SymbolSearch(t *testing.T) {
	pq := ParseZoektQuery("sym:MyType")
	if pq.Pattern != "MyType" {
		t.Errorf("pattern = %q, want %q", pq.Pattern, "MyType")
	}
}

func TestParseZoektQuery_NegativeFilters(t *testing.T) {
	pq := ParseZoektQuery("-repo:vendor -file:test handleAuth")
	if pq.Pattern != "handleAuth" {
		t.Errorf("pattern = %q, want %q", pq.Pattern, "handleAuth")
	}
	// Negative filters are stripped, not stored.
	if len(pq.RepoPatterns) != 0 {
		t.Errorf("unexpected repo patterns: %v", pq.RepoPatterns)
	}
}

func TestParsedQuery_MatchesFileFilter(t *testing.T) {
	pq := ParsedQuery{FilePatterns: []string{`\.go$`}}
	if !pq.MatchesFileFilter("main.go") {
		t.Error("main.go should match .go filter")
	}
	if pq.MatchesFileFilter("main.py") {
		t.Error("main.py should not match .go filter")
	}
}

func TestParsedQuery_MatchesFileFilter_Empty(t *testing.T) {
	pq := ParsedQuery{}
	if !pq.MatchesFileFilter("anything") {
		t.Error("empty filter should match everything")
	}
}

func TestDeltaIndex_SearchWithFileFilter(t *testing.T) {
	root := t.TempDir()

	writeFile(t, root+"/a.go", "package main\n\nfunc handle() {}\n")
	writeFile(t, root+"/b.py", "def handle():\n    pass\n")

	dirty := []DirtyFile{
		{Path: "a.go", Status: FileModified},
		{Path: "b.py", Status: FileModified},
	}
	idx := BuildDeltaIndex(root, dirty)

	// Without file filter, both match.
	matches, err := idx.Search("handle", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches without filter, got %d", len(matches))
	}

	// With .go file filter, only a.go matches.
	pq := &ParsedQuery{FilePatterns: []string{`\.go$`}}
	matches, err = idx.Search("handle", pq)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match with .go filter, got %d", len(matches))
	}
	if matches[0].Path != "a.go" {
		t.Errorf("expected match in a.go, got %s", matches[0].Path)
	}
}
