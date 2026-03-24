package main

import (
	"testing"
)

func TestParsePorcelainV2_Modified(t *testing.T) {
	// Ordinary modified file (worktree changed).
	input := []byte("1 .M N... 100644 100644 100644 abc123 def456 src/main.go\n")

	files, err := ParsePorcelainV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != "src/main.go" {
		t.Errorf("path = %q, want %q", files[0].Path, "src/main.go")
	}
	if files[0].Status != FileModified {
		t.Errorf("status = %v, want modified", files[0].Status)
	}
}

func TestParsePorcelainV2_Deleted(t *testing.T) {
	input := []byte("1 .D N... 100644 100644 000000 abc123 def456 old_file.go\n")

	files, err := ParsePorcelainV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Status != FileDeleted {
		t.Errorf("status = %v, want deleted", files[0].Status)
	}
}

func TestParsePorcelainV2_Added(t *testing.T) {
	input := []byte("1 A. N... 000000 100644 100644 0000000 abc123 new_file.go\n")

	files, err := ParsePorcelainV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Status != FileAdded {
		t.Errorf("status = %v, want added", files[0].Status)
	}
}

func TestParsePorcelainV2_Untracked(t *testing.T) {
	input := []byte("? newfile.txt\n")

	files, err := ParsePorcelainV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != "newfile.txt" {
		t.Errorf("path = %q, want %q", files[0].Path, "newfile.txt")
	}
	if files[0].Status != FileAdded {
		t.Errorf("status = %v, want added", files[0].Status)
	}
}

func TestParsePorcelainV2_Rename(t *testing.T) {
	input := []byte("2 R. N... 100644 100644 100644 abc123 def456 R100 new.go\told.go\n")

	files, err := ParsePorcelainV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Status != FileRenamed {
		t.Errorf("status = %v, want renamed", files[0].Status)
	}
}

func TestParsePorcelainV2_Unmerged(t *testing.T) {
	input := []byte("u UU N... 100644 100644 100644 100644 abc123 def456 ghi789 conflict.go\n")

	files, err := ParsePorcelainV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Status != FileUnmerged {
		t.Errorf("status = %v, want unmerged", files[0].Status)
	}
}

func TestParsePorcelainV2_Mixed(t *testing.T) {
	input := []byte(`1 .M N... 100644 100644 100644 abc123 def456 modified.go
1 .D N... 100644 100644 000000 abc123 def456 deleted.go
? untracked.txt
`)

	files, err := ParsePorcelainV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}
}

func TestParsePorcelainV2_Empty(t *testing.T) {
	files, err := ParsePorcelainV2([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
}

func TestParsePorcelainV2_Ignored(t *testing.T) {
	input := []byte("! ignored_file.txt\n")

	files, err := ParsePorcelainV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files (ignored), got %d", len(files))
	}
}

func TestClassifyXY(t *testing.T) {
	tests := []struct {
		xy   string
		want FileStatus
	}{
		{".M", FileModified},
		{"M.", FileModified},
		{"MM", FileModified},
		{".D", FileDeleted},
		{"D.", FileDeleted},
		{"A.", FileAdded},
		{"AM", FileAdded},
		{"..", FileModified},
	}

	for _, tt := range tests {
		got := classifyXY(tt.xy)
		if got != tt.want {
			t.Errorf("classifyXY(%q) = %v, want %v", tt.xy, got, tt.want)
		}
	}
}

func TestDirtySetChanged(t *testing.T) {
	a := []DirtyFile{{Path: "a.go", Status: FileModified}}
	b := []DirtyFile{{Path: "a.go", Status: FileModified}}
	c := []DirtyFile{{Path: "a.go", Status: FileDeleted}}
	d := []DirtyFile{{Path: "a.go", Status: FileModified}, {Path: "b.go", Status: FileAdded}}

	if dirtySetChanged(a, b) {
		t.Error("identical sets should not be changed")
	}
	if !dirtySetChanged(a, c) {
		t.Error("different status should be changed")
	}
	if !dirtySetChanged(a, d) {
		t.Error("different count should be changed")
	}
	if !dirtySetChanged(nil, a) {
		t.Error("nil vs non-nil should be changed")
	}
	if dirtySetChanged(nil, nil) {
		t.Error("nil vs nil should not be changed")
	}
}
