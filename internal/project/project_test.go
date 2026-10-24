package project

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func fakeRoot() fstest.MapFS {
	return fstest.MapFS{
		"home/me/shop/.git":            {Mode: fs.ModeDir},
		"home/me/shop/apps/web/a.js":   {},
		"home/me/wt/.git":              {Data: []byte("gitdir: /home/me/shop/.git/worktrees/wt")},
		"home/me/notes/todo.txt":       {},
		"home/me/shop-old/readme.txt":  {},
		"srv/www/index.html":           {},
		"home/me/shop/vendor/x/.keep":  {},
		"home/me/shop/vendor/x/.git/x": {},
	}
}

func TestDescribe(t *testing.T) {
	tests := []struct {
		dir, want string
	}{
		{"/home/me/shop", "shop"},
		{"/home/me/shop/apps/web", "shop/apps/web"},
		{"/home/me/shop/vendor/x", "x"}, // nested repo wins
		{"/home/me/wt", "wt"},           // worktree with a .git file
		{"/home/me/notes", "~/notes"},
		{"/home/me", "~"},
		{"/home/me/shop-old", "~/shop-old"}, // not fooled by the shop prefix
		{"/srv/www", "/srv/www"},
		{"/", "/"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Describe(fakeRoot(), tt.dir, "/home/me"); got != tt.want {
			t.Errorf("Describe(%q) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}

func TestBranch(t *testing.T) {
	const sha = "4f243907c1d0e2b5a6f7e8d9c0b1a2f3e4d5c6b7"
	rootfs := fstest.MapFS{
		"repo/.git/HEAD":              {Data: []byte("ref: refs/heads/feature/checkout\n")},
		"detached/.git/HEAD":          {Data: []byte(sha + "\n")},
		"main/.git/worktrees/wt/HEAD": {Data: []byte("ref: refs/heads/hotfix\n")},
		"wt/.git":                     {Data: []byte("gitdir: /main/.git/worktrees/wt\n")},
		"mods/lib/.git":               {Data: []byte("gitdir: ../.git/modules/lib\n")},
		"mods/.git/modules/lib/HEAD":  {Data: []byte("ref: refs/heads/main\n")},
		"broken/.git":                 {Data: []byte("not a gitdir line\n")},
		"empty/.git/config":           {},
	}
	tests := []struct {
		root, want string
		ok         bool
	}{
		{"/repo", "feature/checkout", true},
		{"/detached", "4f24390", true},
		{"/wt", "hotfix", true},     // worktree, absolute gitdir
		{"/mods/lib", "main", true}, // submodule, relative gitdir
		{"/broken", "", false},
		{"/empty", "", false}, // no HEAD
		{"/missing", "", false},
	}
	for _, tt := range tests {
		got, ok := Branch(rootfs, tt.root)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Branch(%q) = %q, %v; want %q, %v", tt.root, got, ok, tt.want, tt.ok)
		}
	}
}

func TestDescribeWithoutHome(t *testing.T) {
	if got := Describe(fakeRoot(), "/home/me/notes", ""); got != "/home/me/notes" {
		t.Errorf("got %q, want the full path", got)
	}
}
