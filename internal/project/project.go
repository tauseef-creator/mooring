// Package project describes which project a directory belongs to.
package project

import (
	"io/fs"
	"path"
	"strings"
)

// Root returns the nearest directory at or above dir that contains a .git
// entry: a directory, or a file for worktrees and submodules. dir is an
// absolute path, and rootfs is the filesystem root, normally os.DirFS("/").
func Root(rootfs fs.FS, dir string) (string, bool) {
	rel := strings.TrimPrefix(path.Clean(dir), "/")
	if rel == "" {
		rel = "."
	}
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
