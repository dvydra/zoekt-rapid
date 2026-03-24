package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestExtractTrigrams(t *testing.T) {
	data := []byte("abcde")
	tris := ExtractTrigrams(data)
	// "abc", "bcd", "cde" = 3 trigrams.
	if len(tris) != 3 {
		t.Errorf("expected 3 trigrams, got %d", len(tris))
	}
}

func TestExtractTrigrams_Short(t *testing.T) {
	if tris := ExtractTrigrams([]byte("ab")); len(tris) != 0 {
		t.Errorf("expected 0 trigrams for short input, got %d", len(tris))
	}
	if tris := ExtractTrigrams(nil); len(tris) != 0 {
		t.Errorf("expected 0 trigrams for nil, got %d", len(tris))
	}
}

func TestExtractTrigrams_Dedup(t *testing.T) {
	data := []byte("aaaa")
	tris := ExtractTrigrams(data)
	// "aaa" appears twice but should be deduped.
	if len(tris) != 1 {
		t.Errorf("expected 1 unique trigram, got %d", len(tris))
	}
}

func TestTrigramIndex_Candidates(t *testing.T) {
	idx := NewTrigramIndex()
	idx.Add("a.go", []byte("func handleAuth() {}"))
	idx.Add("b.go", []byte("func handleHTTP() {}"))
	idx.Add("c.go", []byte("var x = 42"))

	// "handleAuth" trigrams should match only a.go.
	tris := ExtractTrigramsFromString("handleAuth")
	cands := idx.Candidates(tris)
	if len(cands) != 1 || cands[0] != "a.go" {
		t.Errorf("expected [a.go], got %v", cands)
	}

	// "handle" trigrams should match both a.go and b.go.
	tris = ExtractTrigramsFromString("handle")
	cands = idx.Candidates(tris)
	sort.Strings(cands)
	if len(cands) != 2 {
		t.Errorf("expected 2 candidates, got %v", cands)
	}
}

func TestTrigramIndex_EmptyQuery(t *testing.T) {
	idx := NewTrigramIndex()
	idx.Add("a.go", []byte("hello world"))

	cands := idx.Candidates(nil)
	if len(cands) != 1 {
		t.Errorf("expected 1 candidate for empty query, got %d", len(cands))
	}
}

func TestBuildDeltaIndex(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "modified.go"), "package main\n\nfunc hello() { println(\"hello\") }\n")
	writeFile(t, filepath.Join(root, "new.go"), "package main\n\nfunc newFunc() {}\n")

	dirty := []DirtyFile{
		{Path: "modified.go", Status: FileModified},
		{Path: "new.go", Status: FileAdded},
		{Path: "deleted.go", Status: FileDeleted},
	}

	idx := BuildDeltaIndex(root, dirty)

	if len(idx.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(idx.Files))
	}
	if !idx.Tombstones["deleted.go"] {
		t.Error("expected deleted.go in tombstones")
	}
	if len(idx.DirtyPaths) != 3 {
		t.Errorf("expected 3 dirty paths, got %d", len(idx.DirtyPaths))
	}
}

func TestDeltaIndex_Search(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "a.go"), "package main\n\nfunc handleAuth() {\n\tprintln(\"auth\")\n}\n")
	writeFile(t, filepath.Join(root, "b.go"), "package main\n\nfunc handleHTTP() {\n\tprintln(\"http\")\n}\n")

	dirty := []DirtyFile{
		{Path: "a.go", Status: FileModified},
		{Path: "b.go", Status: FileModified},
	}

	idx := BuildDeltaIndex(root, dirty)

	// Search for "handleAuth" — should match only a.go.
	matches, err := idx.Search("handleAuth")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Path != "a.go" {
		t.Errorf("expected match in a.go, got %s", matches[0].Path)
	}
	if matches[0].LineNumber != 3 {
		t.Errorf("expected line 3, got %d", matches[0].LineNumber)
	}

	// Search for "handle" — should match both.
	matches, err = idx.Search("handle")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
}

func TestDeltaIndex_SearchRegex(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "a.go"), "foo123bar\nfoo456bar\nbaz\n")

	dirty := []DirtyFile{{Path: "a.go", Status: FileModified}}
	idx := BuildDeltaIndex(root, dirty)

	matches, err := idx.Search("foo\\d+bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 regex matches, got %d", len(matches))
	}
}

func TestDeltaIndex_SearchNoMatches(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "a.go"), "package main\n")

	dirty := []DirtyFile{{Path: "a.go", Status: FileModified}}
	idx := BuildDeltaIndex(root, dirty)

	matches, err := idx.Search("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestDeltaIndex_NilSafe(t *testing.T) {
	var idx *DeltaIndex

	matches, err := idx.Search("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches from nil index, got %d", len(matches))
	}
	if idx.IsDirty("foo") {
		t.Error("nil index should not report dirty")
	}
	if idx.IsTombstoned("foo") {
		t.Error("nil index should not report tombstoned")
	}
}

func TestDeltaIndex_Tombstone(t *testing.T) {
	root := t.TempDir()

	dirty := []DirtyFile{{Path: "gone.go", Status: FileDeleted}}
	idx := BuildDeltaIndex(root, dirty)

	if !idx.IsTombstoned("gone.go") {
		t.Error("expected gone.go to be tombstoned")
	}
	if !idx.IsDirty("gone.go") {
		t.Error("expected gone.go to be dirty")
	}
}

func TestDeltaIndex_BinarySkipped(t *testing.T) {
	root := t.TempDir()

	// Write a binary file (contains null byte).
	writeFile(t, filepath.Join(root, "bin.dat"), "hello\x00world")

	dirty := []DirtyFile{{Path: "bin.dat", Status: FileModified}}
	idx := BuildDeltaIndex(root, dirty)

	// Binary file should be in dirty paths but not in Files.
	if !idx.IsDirty("bin.dat") {
		t.Error("expected bin.dat to be dirty")
	}
	if _, ok := idx.Files["bin.dat"]; ok {
		t.Error("binary file should not be in Files")
	}
}

func TestExtractLiterals(t *testing.T) {
	tests := []struct {
		pattern string
		want    int // number of literals with len >= 3
	}{
		{"handleAuth", 1},       // whole thing is literal
		{"handle.*Auth", 2},     // "handle" and "Auth" — but Auth is only 4 chars
		{"foo\\d+bar", 2},       // "foo" and "bar"
		{"a.b", 0},              // too short after splitting
		{"abcdef", 1},           // one literal
		{"\\(literal\\)", 1},    // escaped parens are literal
	}

	for _, tt := range tests {
		lits := extractLiterals(tt.pattern)
		if len(lits) != tt.want {
			t.Errorf("extractLiterals(%q) = %v (len %d), want len %d", tt.pattern, lits, len(lits), tt.want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
