package main

import (
	"cmp"
	"fmt"
	"net/netip"
	"os"
	"os/user"
	"strconv"
	"strings"
	"text/tabwriter"

	"portman/internal/ports"
)

const usage = `portman: see what's running on your ports

Usage:
  portman              list listening ports
  portman kill <port>  stop the process using a port

Environment:
  HOST_PROC            procfs mount point (default /proc)
`

// maxCommandWidth keeps long command lines from breaking the table.
const maxCommandWidth = 48

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			fmt.Print(usage)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
			os.Exit(2)
		}
	}

	root := procRoot()
	ls, err := ports.List(os.DirFS(root))
	if err != nil {
		fmt.Fprintf(os.Stderr, "portman: reading procfs at %s: %v\n", root, err)
		os.Exit(1)
	}

	hidden := false
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "PORT\tPID\tUSER\tCOMMAND\tADDRESS")
	for _, l := range ls {
		pid := "-"
		if l.PID != 0 {
			pid = strconv.Itoa(l.PID)
		} else {
			hidden = true
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n",
			l.Port, pid, userName(l.UID), truncate(cmp.Or(l.Command, "-"), maxCommandWidth), joinAddrs(l.Addrs))
	}
	tw.Flush()

	if hidden && os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "\nSome ports belong to other users' processes. Run with sudo to see them.")
	}
}

// procRoot returns the procfs mount point. HOST_PROC overrides it, following
// the convention used when reading a host's /proc from inside a container.
func procRoot() string {
	return cmp.Or(os.Getenv("HOST_PROC"), "/proc")
}

// userName returns the login name for uid, or the number if it has none.
func userName(uid int) string {
	id := strconv.Itoa(uid)
	if u, err := user.LookupId(id); err == nil {
		return u.Username
	}
	return id
}

func joinAddrs(addrs []netip.Addr) string {
	s := make([]string, len(addrs))
	for i, a := range addrs {
		s[i] = a.String()
	}
	return strings.Join(s, ", ")
}

// truncate shortens s to at most n runes, marking the cut with an ellipsis.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
