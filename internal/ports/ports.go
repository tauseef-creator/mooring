// Package ports reads listening TCP sockets from a Linux procfs.
package ports

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

// tables are the procfs socket tables to read, relative to the procfs root.
var tables = []struct {
	path, proto string
	optional    bool // tcp6 is missing when IPv6 is disabled
}{
	{"net/tcp", "tcp", false},
	{"net/tcp6", "tcp6", true},
}

// stateListen is the kernel's hex code for TCP_LISTEN.
const stateListen = "0A"

// Listener is one listening TCP socket.
type Listener struct {
	Proto string // "tcp" or "tcp6"
	Addr  netip.Addr
	Port  uint16
	UID   int
	Inode uint64 // links the socket to a process via /proc/<pid>/fd
}

// List returns all listening TCP sockets, sorted by port. procfs is the root
// of a Linux procfs, normally os.DirFS("/proc").
func List(procfs fs.FS) ([]Listener, error) {
	var all []Listener
	for _, src := range tables {
		f, err := procfs.Open(src.path)
		if src.optional && errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		ls, err := parse(f, src.proto)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", src.path, err)
		}
		all = append(all, ls...)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Port != all[j].Port {
			return all[i].Port < all[j].Port
		}
		return all[i].Proto < all[j].Proto
	})
	return all, nil
}

// parse reads a /proc/net/tcp or tcp6 table and keeps only listening sockets.
func parse(r io.Reader, proto string) ([]Listener, error) {
	var ls []Listener
	sc := bufio.NewScanner(r)
	sc.Scan() // skip header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 || fields[3] != stateListen {
			continue
		}
		addr, port, err := parseAddr(fields[1])
		if err != nil {
			return nil, err
		}
		uid, err := strconv.Atoi(fields[7])
		if err != nil {
			return nil, fmt.Errorf("bad uid %q", fields[7])
		}
		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("bad inode %q", fields[9])
		}
		ls = append(ls, Listener{Proto: proto, Addr: addr, Port: port, UID: uid, Inode: inode})
	}
	return ls, sc.Err()
}

// parseAddr decodes "0100007F:0BB8" into 127.0.0.1 and 3000. The kernel
// prints the address as 32-bit words in host (little-endian) byte order.
func parseAddr(s string) (netip.Addr, uint16, error) {
	hexIP, hexPort, ok := strings.Cut(s, ":")
	if !ok {
		return netip.Addr{}, 0, fmt.Errorf("bad address %q", s)
	}
	port, err := strconv.ParseUint(hexPort, 16, 16)
	if err != nil {
		return netip.Addr{}, 0, fmt.Errorf("bad port %q", hexPort)
	}
	raw, err := hex.DecodeString(hexIP)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return netip.Addr{}, 0, fmt.Errorf("bad ip %q", hexIP)
	}
	for i := 0; i < len(raw); i += 4 {
		binary.BigEndian.PutUint32(raw[i:], binary.LittleEndian.Uint32(raw[i:]))
	}
	addr, _ := netip.AddrFromSlice(raw)
	return addr.Unmap(), uint16(port), nil
}
