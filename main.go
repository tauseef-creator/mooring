package main

import (
	"cmp"
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/user"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"mooring/internal/containers"
	"mooring/internal/ports"
	"mooring/internal/project"
)

const usage = `mooring: see what's running on your ports

Usage:
  mooring              list listening ports
  mooring kill <port>  stop the process using a port

Environment:
  HOST_PROC            procfs mount point (default /proc)
  HOST_ROOT            filesystem root for finding projects (default /)
  DOCKER_HOST          Docker API socket (default unix:///var/run/docker.sock)
  CONTAINER_HOST       Podman API socket (default: the rootless, then rootful socket)
`

// maxCommandWidth keeps long command lines from breaking the table.
const maxCommandWidth = 40

// containerAPITimeout bounds all Docker and Podman API calls, so a hung
// engine cannot stall the listing.
const containerAPITimeout = 2 * time.Second

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
		fmt.Fprintf(os.Stderr, "mooring: reading procfs at %s: %v\n", root, err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), containerAPITimeout)
	published, err := containers.Published(ctx, containers.Endpoints(os.Getenv))
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mooring: warning:", err)
	}

	rootfs := os.DirFS(cmp.Or(os.Getenv("HOST_ROOT"), "/"))
	home, _ := os.UserHomeDir()
	now := time.Now()

	hidden := false
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "PORT\tPID\tUSER\tCOMMAND\tPROJECT\tUPTIME\tADDRESS")
	for _, l := range ls {
		command, dir, started := l.Command, l.Cwd, l.Started
		// Published container ports are held by a proxy process, so describe
		// the container instead.
		if c, ok := published[l.Port]; ok {
			command, dir, started = c.Runtime+": "+c.Name, c.Dir, c.Started
		} else if l.PID == 0 {
			hidden = true
		}

		pid, uptime := "-", "-"
		if l.PID != 0 {
			pid = strconv.Itoa(l.PID)
		}
		if !started.IsZero() {
			uptime = humanDuration(now.Sub(started))
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			l.Port, pid, userName(l.UID),
			truncate(cmp.Or(command, "-"), maxCommandWidth),
			cmp.Or(project.Describe(rootfs, dir, home), "-"),
			uptime, joinAddrs(l.Addrs))
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

// humanDuration formats d with its two largest units, such as "2h 14m".
func humanDuration(d time.Duration) string {
	d = max(d, 0)
	days := int(d / (24 * time.Hour))
	hours := int(d / time.Hour % 24)
	mins := int(d / time.Minute % 60)
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm", mins)
	default:
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
}

// truncate shortens s to at most n runes, marking the cut with an ellipsis.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
