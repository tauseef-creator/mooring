package ports

import (
	"net/netip"
	"slices"
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
`

func TestParseIPv4(t *testing.T) {
	ls, err := parse(strings.NewReader(tcp4), "tcp")
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 2 {
		t.Fatalf("got %d listeners, want 2 (established socket must be skipped)", len(ls))
	}
	want := Listener{Proto: "tcp", Addr: netip.MustParseAddr("127.0.0.1"), Port: 3000, UID: 1000, Inode: 48213}
	if ls[0] != want {
		t.Errorf("got %+v, want %+v", ls[0], want)
	}
	if ls[1].Addr != netip.MustParseAddr("0.0.0.0") || ls[1].Port != 8080 {
		t.Errorf("got %v:%d, want 0.0.0.0:8080", ls[1].Addr, ls[1].Port)
	}
}

func TestList(t *testing.T) {
	procfs := fstest.MapFS{
		"net/tcp":  {Data: []byte(tcp4)},
		"net/tcp6": {Data: []byte(tcp6)},
	}
	ls, err := List(procfs)
	if err != nil {
		t.Fatal(err)
	}
	var got []uint16
	for _, l := range ls {
		got = append(got, l.Port)
	}
	if want := []uint16{3000, 5173, 8080}; !slices.Equal(got, want) {
		t.Errorf("ports = %v, want %v (sorted)", got, want)
	}
}

func TestListWithoutIPv6(t *testing.T) {
	procfs := fstest.MapFS{"net/tcp": {Data: []byte(tcp4)}}
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

func TestParseIPv6(t *testing.T) {
	ls, err := parse(strings.NewReader(tcp6), "tcp6")
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 1 {
		t.Fatalf("got %d listeners, want 1", len(ls))
	}
	if ls[0].Addr != netip.MustParseAddr("::1") || ls[0].Port != 5173 {
		t.Errorf("got %v:%d, want [::1]:5173", ls[0].Addr, ls[0].Port)
	}
}
