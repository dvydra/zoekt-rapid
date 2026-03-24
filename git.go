package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const gitTimeout = 10 * time.Second

// BranchHead holds the current branch name and HEAD SHA for a repo.
type BranchHead struct {
	Branch string
	SHA    string
}

// DirtyFile represents a file whose working tree content differs from HEAD.
type DirtyFile struct {
	Path   string
	Status FileStatus
}

type FileStatus int

const (
	FileModified  FileStatus = iota // tracked file with changes
	FileAdded                       // new untracked file
	FileDeleted                     // tracked file deleted from worktree
	FileRenamed                     // renamed file
	FileCopied                      // copied file
	FileUnmerged                    // unmerged (conflict)
)

func (s FileStatus) String() string {
	switch s {
	case FileModified:
		return "modified"
	case FileAdded:
		return "added"
	case FileDeleted:
		return "deleted"
	case FileRenamed:
		return "renamed"
	case FileCopied:
		return "copied"
	case FileUnmerged:
		return "unmerged"
	default:
		return "unknown"
	}
}

// GetBranchAndHead returns the current branch and HEAD SHA for a repo.
func GetBranchAndHead(repoPath string) (BranchHead, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	// Use symbolic-ref for branch name (works on empty repos).
	// Falls back to rev-parse --abbrev-ref for detached HEAD.
	var branch string
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "symbolic-ref", "--short", "HEAD")
	branchOut, err := cmd.Output()
	if err != nil {
		// Detached HEAD — fall back.
		cmd = exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
		branchOut, err = cmd.Output()
		if err != nil {
			return BranchHead{}, fmt.Errorf("git branch: %w", err)
		}
	}
	branch = strings.TrimSpace(string(branchOut))

	cmd = exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "HEAD")
	shaOut, err := cmd.Output()
	if err != nil {
		// Empty repo (no commits yet).
		return BranchHead{Branch: branch, SHA: ""}, nil
	}

	return BranchHead{
		Branch: branch,
		SHA:    strings.TrimSpace(string(shaOut)),
	}, nil
}

// GetDirtyFiles returns files whose working tree content differs from HEAD.
func GetDirtyFiles(repoPath string) ([]DirtyFile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "status", "--porcelain=v2")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}

	return ParsePorcelainV2(out)
}

// ParsePorcelainV2 parses the output of `git status --porcelain=v2`.
func ParsePorcelainV2(data []byte) ([]DirtyFile, error) {
	var files []DirtyFile

	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		switch line[0] {
		case '1': // ordinary changed entry
			f, err := parseOrdinaryEntry(line)
			if err != nil {
				continue
			}
			files = append(files, f)

		case '2': // rename or copy entry
			f, err := parseRenameEntry(line)
			if err != nil {
				continue
			}
			files = append(files, f)

		case 'u': // unmerged entry
			f, err := parseUnmergedEntry(line)
			if err != nil {
				continue
			}
			files = append(files, f)

		case '?': // untracked
			// Format: ? <path>
			if len(line) > 2 {
				files = append(files, DirtyFile{
					Path:   string(line[2:]),
					Status: FileAdded,
				})
			}

		case '!': // ignored — skip
			continue
		}
	}

	return files, nil
}

// parseOrdinaryEntry parses a porcelain v2 "1" line.
// Format: 1 XY sub mH mI mW hH hI <path>
func parseOrdinaryEntry(line []byte) (DirtyFile, error) {
	fields := bytes.Fields(line)
	if len(fields) < 9 {
		return DirtyFile{}, fmt.Errorf("malformed ordinary entry: %s", line)
	}

	xy := string(fields[1])
	path := string(fields[8])

	status := classifyXY(xy)
	return DirtyFile{Path: path, Status: status}, nil
}

// parseRenameEntry parses a porcelain v2 "2" line.
// Format: 2 XY sub mH mI mW hH hI Xscore <path>\t<origPath>
func parseRenameEntry(line []byte) (DirtyFile, error) {
	// The path field may contain a tab separating path and origPath.
	fields := bytes.Fields(line)
	if len(fields) < 10 {
		return DirtyFile{}, fmt.Errorf("malformed rename entry: %s", line)
	}

	pathField := string(fields[9])
	// Take the new path (before tab).
	if idx := strings.IndexByte(pathField, '\t'); idx >= 0 {
		pathField = pathField[:idx]
	}

	return DirtyFile{Path: pathField, Status: FileRenamed}, nil
}

// parseUnmergedEntry parses a porcelain v2 "u" line.
// Format: u XY sub m1 m2 m3 mW h1 h2 h3 <path>
func parseUnmergedEntry(line []byte) (DirtyFile, error) {
	fields := bytes.Fields(line)
	if len(fields) < 11 {
		return DirtyFile{}, fmt.Errorf("malformed unmerged entry: %s", line)
	}

	path := string(fields[10])
	return DirtyFile{Path: path, Status: FileUnmerged}, nil
}

// classifyXY maps the two-character XY status to a FileStatus.
// X = index status, Y = worktree status. We care about both.
func classifyXY(xy string) FileStatus {
	if len(xy) < 2 {
		return FileModified
	}

	// If worktree shows deletion.
	if xy[1] == 'D' {
		return FileDeleted
	}
	// If index shows deletion.
	if xy[0] == 'D' {
		return FileDeleted
	}
	// If index shows addition.
	if xy[0] == 'A' {
		return FileAdded
	}

	return FileModified
}
