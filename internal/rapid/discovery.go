package rapid

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiscoverRepos walks each root directory up to scanDepth levels looking for
// git repositories (directories containing a .git subdirectory). Repos whose
// paths match any exclude pattern are skipped. Returns a sorted list of
// absolute repo paths.
func DiscoverRepos(roots []string, scanDepth int, excludePatterns []string) ([]string, error) {
	var repos []string
	seen := make(map[string]bool)

	for _, root := range roots {
		root, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}

		info, err := os.Stat(root)
		if err != nil {
			// Skip missing roots.
			continue
		}
		if !info.IsDir() {
			continue
		}

		err = walkForRepos(root, root, scanDepth, excludePatterns, seen, &repos)
		if err != nil {
			return nil, err
		}
	}

	sort.Strings(repos)
	return repos, nil
}

func walkForRepos(dir, root string, maxDepth int, excludePatterns []string, seen map[string]bool, repos *[]string) error {
	depth := depthFrom(root, dir)
	if depth > maxDepth {
		return nil
	}

	// Check if this directory is a git repo.
	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	if err == nil && info.IsDir() {
		absPath, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		if !seen[absPath] && !matchesAny(absPath, excludePatterns) {
			seen[absPath] = true
			*repos = append(*repos, absPath)
		}
		// Don't recurse into git repos looking for nested repos.
		return nil
	}

	// Recurse into subdirectories.
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Skip directories we can't read.
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == ".git" || name == "." || name == ".." {
			continue
		}
		child := filepath.Join(dir, name)
		if err := walkForRepos(child, root, maxDepth, excludePatterns, seen, repos); err != nil {
			return err
		}
	}

	return nil
}

func depthFrom(root, dir string) int {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return 0
	}
	if rel == "." {
		return 0
	}
	depth := 0
	for _, c := range rel {
		if c == filepath.Separator {
			depth++
		}
	}
	return depth + 1
}

func matchesAny(path string, patterns []string) bool {
	// Check if any segment of the path matches a pattern.
	// Patterns like "*/node_modules/*" mean "contains node_modules as a segment".
	// We extract bare directory names from patterns and check path segments.
	for _, pattern := range patterns {
		// Try direct filepath.Match first (works for simple patterns).
		if matched, err := filepath.Match(pattern, path); err == nil && matched {
			return true
		}
		// Extract the core directory name from patterns like "*/node_modules/*".
		name := extractDirName(pattern)
		if name != "" && containsSegment(path, name) {
			return true
		}
	}
	return false
}

// extractDirName extracts a bare directory name from patterns like "*/node_modules/*"
// or "*/.terraform/*". Returns empty string if the pattern isn't in this form.
func extractDirName(pattern string) string {
	pattern = strings.TrimPrefix(pattern, "*/")
	pattern = strings.TrimSuffix(pattern, "/*")
	// Only return if it's now a simple name (no wildcards or separators).
	if strings.ContainsAny(pattern, "*?[/") {
		return ""
	}
	return pattern
}

// containsSegment returns true if any path segment equals name.
func containsSegment(path, name string) bool {
	for _, seg := range strings.Split(path, string(filepath.Separator)) {
		if seg == name {
			return true
		}
	}
	return false
}
