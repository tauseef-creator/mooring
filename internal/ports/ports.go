// Package ports finds listening TCP ports and the processes that own them,
// using a Linux procfs.
package ports

import (
	"bufio"
	"cmp"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

// stateListen is the kernel's hex code for TCP_LISTEN.
const stateListen = "0A"

// tables are the procfs socket tables to read, relative to the procfs root.
var tables = []struct {
	path     string
	optional bool // tcp6 is missing when IPv6 is disabled
}{
	{"net/tcp", false},
	{"net/tcp6", true},
}

// Listener is a process listening on a TCP port. A process bound to both
// IPv4 and IPv6 is reported once, with both addresses.
type Listener struct {
	Port    uint16
	Addrs   []netip.Addr
	UID     int
	PID     int    // 0 when the owner is not visible, usually another user's process
	Command string // empty when PID is 0
}

// socket is one listening row of a procfs TCP table.
type socket struct {
	addr  netip.Addr
	port  uint16
	uid   int
	inode uint64 // links the socket to a process via <pid>/fd
}

// List returns all listening TCP ports with their owning processes, sorted by
// port. procfs is the root of a Linux procfs, normally os.DirFS("/proc").
func List(procfs fs.FS) ([]Listener, error) {
	socks, err := readSockets(procfs)
	if err != nil {
		return nil, err
	}
	owners, err := socketOwners(procfs)
	if err != nil {
		return nil, err
	}

	type key struct {
		port     uint16
		uid, pid int
	}
	var ls []Listener
	index := make(map[key]int)
	for _, s := range socks {
		k := key{s.port, s.uid, owners[s.inode]}
		if i, ok := index[k]; ok {
			ls[i].Addrs = append(ls[i].Addrs, s.addr)
			continue
		}
		index[k] = len(ls)
		l := Listener{Port: s.port, Addrs: []netip.Addr{s.addr}, UID: s.uid, PID: k.pid}
		if l.PID != 0 {
			l.Command = command(procfs, l.PID)
		}
		ls = append(ls, l)
	}
	return ls, nil
}

// readSockets reads every socket table, sorted by port and then address.
func readSockets(procfs fs.FS) ([]socket, error) {
	var all []socket
	for _, t := range tables {
		f, err := procfs.Open(t.path)
		if t.optional && errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		socks, err := parse(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t.path, err)
		}
		all = append(all, socks...)
	}
	slices.SortFunc(all, func(a, b socket) int {
		return cmp.Or(cmp.Compare(a.port, b.port), a.addr.Compare(b.addr))
	})
	return all, nil
}

// parse reads a procfs tcp or tcp6 table and keeps only listening sockets.
func parse(r io.Reader) ([]socket, error) {
	var socks []socket
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
		socks = append(socks, socket{addr: addr, port: port, uid: uid, inode: inode})
	}
	return socks, sc.Err()
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
