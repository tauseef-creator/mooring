package ports

import (
	"io/fs"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

const tcp4 = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 48213 1 0000000000000000 100 0 0 10 0
   1: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 9921 1 0000000000000000 100 0 0 10 0
   2: 0100007F:0BB8 0100007F:D431 01 00000000:00000000 00:00000000 00000000  1000        0 50000 1 0000000000000000 20 4 30 10 -1
`

const tcp6 = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000001000000:1435 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 51022 1 0000000000000000 100 0 0 10 0
   1: 00000000000000000000000000000000:1F90 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 9922 1 0000000000000000 100 0 0 10 0
`

func link(target string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(target), Mode: fs.ModeSymlink}
}

// fakeProc is a procfs with three processes:
//   - 1234 (node) owns :3000, and its forked worker 1300 shares the socket
//   - 77 (nginx, empty cmdline) owns :8080 on both IPv4 and IPv6
//   - :5173 belongs to a process we cannot see
func fakeProc() fstest.MapFS {
	return fstest.MapFS{
		"net/tcp":        {Data: []byte(tcp4)},
		"net/tcp6":       {Data: []byte(tcp6)},
		"1234/cmdline":   {Data: []byte("/usr/bin/node\x00server.js\x00")},
		"1234/fd/0":      link("/dev/pts/0"),
		"1234/fd/3":      link("socket:[48213]"),
		"1300/cmdline":   {Data: []byte("/usr/bin/node\x00worker.js\x00")},
		"1300/fd/3":      link("socket:[48213]"),
		"77/cmdline":     {Data: []byte("")},
		"77/comm":        {Data: []byte("nginx\n")},
		"77/fd/4":        link("socket:[9921]"),
		"77/fd/5":        link("socket:[9922]"),
		"self/fd/0":      link("/dev/null"),
		"sys/kernel/foo": {Data: []byte("not a process")},
	}
}

func TestList(t *testing.T) {
	ls, err := List(fakeProc())
	if err != nil {
		t.Fatal(err)
	}
	want := []Listener{
		{Port: 3000, Addrs: addrs("127.0.0.1"), UID: 1000, PID: 1234, Command: "node server.js"},
		{Port: 5173, Addrs: addrs("::1"), UID: 1000},
		{Port: 8080, Addrs: addrs("0.0.0.0", "::"), UID: 0, PID: 77, Command: "nginx"},
	}
	if !reflect.DeepEqual(ls, want) {
		t.Errorf("got  %+v\nwant %+v", ls, want)
	}
}

func TestListWithoutIPv6(t *testing.T) {
	procfs := fakeProc()
	delete(procfs, "net/tcp6")
	ls, err := List(procfs)
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 2 {
		t.Errorf("got %d listeners, want 2", len(ls))
	}
}

func TestListWrongRoot(t *testing.T) {
	if _, err := List(fstest.MapFS{}); err == nil {
		t.Error("want error when net/tcp is missing, got nil")
	}
}

func TestParseIPv4(t *testing.T) {
	socks, err := parse(strings.NewReader(tcp4))
	if err != nil {
		t.Fatal(err)
	}
	want := []socket{
		{addr: netip.MustParseAddr("127.0.0.1"), port: 3000, uid: 1000, inode: 48213},
		{addr: netip.MustParseAddr("0.0.0.0"), port: 8080, uid: 0, inode: 9921},
	}
	if !reflect.DeepEqual(socks, want) {
		t.Errorf("got  %+v\nwant %+v (established socket must be skipped)", socks, want)
	}
}

func TestParseIPv6(t *testing.T) {
	socks, err := parse(strings.NewReader(tcp6))
	if err != nil {
		t.Fatal(err)
	}
	if len(socks) != 2 {
		t.Fatalf("got %d sockets, want 2", len(socks))
	}
	if socks[0].addr != netip.MustParseAddr("::1") || socks[0].port != 5173 {
		t.Errorf("got %v:%d, want [::1]:5173", socks[0].addr, socks[0].port)
	}
}

func addrs(ss ...string) []netip.Addr {
	var out []netip.Addr
	for _, s := range ss {
		out = append(out, netip.MustParseAddr(s))
	}
	return out
}
