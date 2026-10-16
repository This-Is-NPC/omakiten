package project

import (
	"os"
	"path/filepath"
	"strings"
)

// mainCheckoutPath maps path inside a linked git worktree to the same
// relative path inside the repository's main checkout. It reads the
// worktree's `.git` file (`gitdir: …`) and that gitdir's `commondir`,
// so it needs no git executable. ok is false when path is not inside a
// linked worktree or the main checkout cannot be located.
func mainCheckoutPath(path string) (string, bool) {
	top, gitFile, ok := nearestGitEntry(path)
	if !ok {
		return "", false
	}
	gitDir, ok := readGitDirFile(gitFile)
	if !ok {
		return "", false
	}
	common, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return "", false
	}
	commonDir := resolveFrom(gitDir, strings.TrimSpace(string(common)))
	if filepath.Base(commonDir) != ".git" {
		return "", false
	}
	rel, err := filepath.Rel(top, path)
	if err != nil {
		return "", false
	}
	return filepath.Join(filepath.Dir(commonDir), rel), true
}

// nearestGitEntry walks up from path to the first `.git` entry. ok is
// true only when that entry is a file, as in a linked worktree.
func nearestGitEntry(path string) (top, gitFile string, ok bool) {
	for dir := path; ; dir = filepath.Dir(dir) {
		entry := filepath.Join(dir, ".git")
		if info, err := os.Lstat(entry); err == nil {
			return dir, entry, info.Mode().IsRegular()
		}
		if filepath.Dir(dir) == dir {
			return "", "", false
		}
	}
}

func readGitDirFile(gitFile string) (string, bool) {
	content, err := os.ReadFile(gitFile)
	if err != nil {
		return "", false
	}
	gitDir, found := strings.CutPrefix(strings.TrimSpace(string(content)), "gitdir:")
	if !found {
		return "", false
	}
	return resolveFrom(filepath.Dir(gitFile), strings.TrimSpace(gitDir)), true
}

func resolveFrom(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
