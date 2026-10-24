package ports

import (
	"testing"
	"testing/fstest"
)

func TestSocketInode(t *testing.T) {
	tests := []struct {
		target string
		inode  uint64
		ok     bool
	}{
		{"socket:[48213]", 48213, true},
		{"/dev/null", 0, false},
		{"pipe:[123]", 0, false},
		{"socket:[abc]", 0, false},
		{"socket:[123", 0, false},
	}
	for _, tt := range tests {
		inode, ok := socketInode(tt.target)
		if inode != tt.inode || ok != tt.ok {
			t.Errorf("socketInode(%q) = %d, %v; want %d, %v", tt.target, inode, ok, tt.inode, tt.ok)
		}
	}
}

func TestCommand(t *testing.T) {
	procfs := fstest.MapFS{
		"1/cmdline": {Data: []byte("/usr/bin/python3\x00-m\x00http.server\x00")},
		"2/cmdline": {Data: []byte("nginx: master process /usr/sbin/nginx\x00")},
		"3/cmdline": {Data: []byte("")},
		"3/comm":    {Data: []byte("kworker/0:1\n")},
		"4/cmdline": {Data: []byte("vite\x00")},
	}
	tests := []struct {
		pid  int
		want string
	}{
		{1, "python3 -m http.server"},
		{2, "nginx: master process /usr/sbin/nginx"},
		{3, "kworker/0:1"},
		{4, "vite"},
		{5, ""}, // process exited
	}
	for _, tt := range tests {
		if got := command(procfs, tt.pid); got != tt.want {
			t.Errorf("command(%d) = %q, want %q", tt.pid, got, tt.want)
		}
	}
}
