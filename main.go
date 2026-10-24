package main

import (
	"cmp"
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/user"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/tauseef-creator/mooring/internal/containers"
	"github.com/tauseef-creator/mooring/internal/ports"
)

const usage = `mooring: see what's running on your ports

Usage:
  mooring                      browse ports interactively (prints a table when piped)
  mooring list                 print a table of listening ports
  mooring kill [flags] <port>  stop whatever is using a port (see mooring kill -h)
  mooring version              print the version

Environment:
  HOST_PROC            procfs mount point (default /proc)
  HOST_ROOT            filesystem root for finding projects (default /)
  DOCKER_HOST          Docker API socket (default unix:///var/run/docker.sock)
  CONTAINER_HOST       Podman API socket (default: the rootless, then rootful socket)
`

// maxCommandWidth keeps long command lines from breaking the output.
const maxCommandWidth = 40

// containerAPITimeout bounds Docker and Podman API calls, so a hung engine
// cannot stall mooring.
const containerAPITimeout = 2 * time.Second

// hiddenHint explains rows whose owner is not visible.
const hiddenHint = "Some ports belong to other users' processes. Run with sudo to see them."

// version is set at build time by release builds:
// go build -ldflags "-X main.version=v0.1.0"
var version string

// versionString reports the release version. Other builds fall back to the
// version Go records in the binary: the module version for "go install", or
// one derived from the git commit for a local build. "dev" covers the rest.
func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// run executes the command in args and returns the exit code: 0 on success,
// 1 on failure and 2 on a usage error.
func run(args []string) int {
	if len(args) == 0 {
		if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
			return runTUI()
		}
		return runList()
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	case "-v", "--version", "version":
		fmt.Println("mooring", versionString())
		return 0
	case "list":
		return runList()
	case "kill":
		return runKill(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// snapshot is what mooring knows about the ports in use at one moment.
type snapshot struct {
	listeners  []ports.Listener
	containers map[uint16]containers.Container // by published host port
	warning    error                           // a container engine failed; the rest is valid
}

// scan reads the listening ports and the ports published by containers. A
// container engine failure only sets snapshot.warning, since the port list
// still works without it.
func scan() (snapshot, error) {
	root := procRoot()
	ls, err := ports.List(os.DirFS(root))
	if err != nil {
		return snapshot{}, fmt.Errorf("reading procfs at %s: %w", root, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), containerAPITimeout)
	defer cancel()
	published, err := containers.Published(ctx, containers.Endpoints(os.Getenv))
	return snapshot{listeners: ls, containers: published, warning: err}, nil
}

// procRoot returns the procfs mount point. HOST_PROC overrides it, following
// the convention used when reading a host's /proc from inside a container.
func procRoot() string {
	return cmp.Or(os.Getenv("HOST_PROC"), "/proc")
}

// hostRoot returns the filesystem root used to find projects. HOST_ROOT
// overrides it, alongside HOST_PROC.
func hostRoot() string {
	return cmp.Or(os.Getenv("HOST_ROOT"), "/")
}

// errorf prints a message prefixed with the program name to stderr.
func errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mooring: "+format+"\n", args...)
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
