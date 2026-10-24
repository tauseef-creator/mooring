// Package project describes which project a directory belongs to.
package project

import (
	"encoding/hex"
	"io/fs"
	"path"
	"strings"
)

// shortHashLen matches git's default abbreviated commit length.
const shortHashLen = 7

// Root returns the nearest directory at or above dir that contains a .git
// entry: a directory, or a file for worktrees and submodules. dir is an
// absolute path, and rootfs is the filesystem root, normally os.DirFS("/").
func Root(rootfs fs.FS, dir string) (string, bool) {
	rel := fsPath(dir)
	for {
		if _, err := fs.Stat(rootfs, path.Join(rel, ".git")); err == nil {
			return path.Join("/", rel), true
		}
		if rel == "." {
			return "", false
		}
		rel = path.Dir(rel)
	}
}

// Branch returns the branch checked out in the repository at root, or a
// short commit hash when HEAD is detached. It reads .git/HEAD directly, and
// follows the "gitdir:" pointer that worktrees and submodules use.
func Branch(rootfs fs.FS, root string) (string, bool) {
	gitDir := path.Join(root, ".git")
	if b, err := fs.ReadFile(rootfs, fsPath(gitDir)); err == nil {
		dir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
		if !ok {
			return "", false
		}
		if !path.IsAbs(dir) {
			dir = path.Join(root, dir)
		}
		gitDir = dir
	}

	b, err := fs.ReadFile(rootfs, fsPath(path.Join(gitDir, "HEAD")))
	if err != nil {
		return "", false
	}
	head := strings.TrimSpace(string(b))
	if branch, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
		return branch, true
	}
	if _, err := hex.DecodeString(head); err == nil && len(head) >= shortHashLen {
		return head[:shortHashLen], true
	}
	return "", false
}

// fsPath converts an absolute path to the form fs.FS expects: no leading
// slash, and "." for the root.
func fsPath(abs string) string {
	if rel := strings.TrimPrefix(path.Clean(abs), "/"); rel != "" {
		return rel
	}
	return "."
}

// Describe returns a short label for dir: the repository name, plus the
// subdirectory when dir is inside it ("shop/apps/web"). Outside a repository
// it returns dir itself, with home shortened to "~". An empty dir gives "".
func Describe(rootfs fs.FS, dir, home string) string {
	if dir == "" {
		return ""
	}
	if root, ok := Root(rootfs, dir); ok {
		label := path.Base(root)
		if sub := strings.TrimPrefix(dir, root); sub != "" {
			label += sub
		}
		return label
	}
	if home != "" {
		if dir == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(dir, home+"/"); ok {
			return "~/" + rest
		}
	}
	return dir
}
