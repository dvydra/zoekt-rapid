package rapid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverRepos_FindsRepos(t *testing.T) {
	root := t.TempDir()

	// Create repos at depth 1.
	mkGitRepo(t, filepath.Join(root, "repo-a"))
	mkGitRepo(t, filepath.Join(root, "repo-b"))

	repos, err := DiscoverRepos([]string{root}, 3, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d: %v", len(repos), repos)
	}
	if filepath.Base(repos[0]) != "repo-a" || filepath.Base(repos[1]) != "repo-b" {
		t.Fatalf("unexpected repos: %v", repos)
	}
}

func TestDiscoverRepos_NestedDepth(t *testing.T) {
	root := t.TempDir()

	// Repo at depth 2: root/org/repo
	mkGitRepo(t, filepath.Join(root, "org", "repo"))

	repos, err := DiscoverRepos([]string{root}, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d: %v", len(repos), repos)
	}

	// Depth 1 should not find it.
	repos, err = DiscoverRepos([]string{root}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos at depth 1, got %d: %v", len(repos), repos)
	}
}

func TestDiscoverRepos_RespectsDepthLimit(t *testing.T) {
	root := t.TempDir()

	// Repo at depth 4: root/a/b/c/repo
	mkGitRepo(t, filepath.Join(root, "a", "b", "c", "repo"))

	repos, err := DiscoverRepos([]string{root}, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos (beyond depth 3), got %d: %v", len(repos), repos)
	}

	// Depth 4 should find it.
	repos, err = DiscoverRepos([]string{root}, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo at depth 4, got %d", len(repos))
	}
}

func TestDiscoverRepos_ExcludePatterns(t *testing.T) {
	root := t.TempDir()

	mkGitRepo(t, filepath.Join(root, "good-repo"))
	mkGitRepo(t, filepath.Join(root, "node_modules"))

	repos, err := DiscoverRepos([]string{root}, 3, []string{"node_modules"})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo (excluded node_modules), got %d: %v", len(repos), repos)
	}
	if filepath.Base(repos[0]) != "good-repo" {
		t.Fatalf("unexpected repo: %v", repos[0])
	}
}

func TestDiscoverRepos_MissingRoot(t *testing.T) {
	repos, err := DiscoverRepos([]string{"/nonexistent/path"}, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos for missing root, got %d", len(repos))
	}
}

func TestDiscoverRepos_MultipleRoots(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()

	mkGitRepo(t, filepath.Join(root1, "repo-x"))
	mkGitRepo(t, filepath.Join(root2, "repo-y"))

	repos, err := DiscoverRepos([]string{root1, root2}, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos from multiple roots, got %d: %v", len(repos), repos)
	}
}

func TestDiscoverRepos_DoesNotRecurseIntoGitRepo(t *testing.T) {
	root := t.TempDir()

	// Outer repo contains a nested repo — should only find the outer one.
	outer := filepath.Join(root, "outer")
	mkGitRepo(t, outer)
	mkGitRepo(t, filepath.Join(outer, "inner"))

	repos, err := DiscoverRepos([]string{root}, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo (no recursion into git repo), got %d: %v", len(repos), repos)
	}
	if filepath.Base(repos[0]) != "outer" {
		t.Fatalf("unexpected repo: %v", repos[0])
	}
}

func TestDiscoverRepos_SortedOutput(t *testing.T) {
	root := t.TempDir()

	mkGitRepo(t, filepath.Join(root, "charlie"))
	mkGitRepo(t, filepath.Join(root, "alpha"))
	mkGitRepo(t, filepath.Join(root, "bravo"))

	repos, err := DiscoverRepos([]string{root}, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 3 {
		t.Fatalf("expected 3 repos, got %d", len(repos))
	}
	if filepath.Base(repos[0]) != "alpha" || filepath.Base(repos[1]) != "bravo" || filepath.Base(repos[2]) != "charlie" {
		t.Fatalf("repos not sorted: %v", repos)
	}
}

func mkGitRepo(t *testing.T, path string) {
	t.Helper()
	gitDir := filepath.Join(path, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
}
