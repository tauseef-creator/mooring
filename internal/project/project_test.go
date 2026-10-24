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

func TestDescribeWithoutHome(t *testing.T) {
	if got := Describe(fakeRoot(), "/home/me/notes", ""); got != "/home/me/notes" {
		t.Errorf("got %q, want the full path", got)
	}
}
