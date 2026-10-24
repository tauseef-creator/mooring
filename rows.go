package main

import (
	"cmp"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/tauseef-creator/mooring/internal/containers"
	"github.com/tauseef-creator/mooring/internal/project"
)

// columnTitles are the headers shared by the table and the TUI, in the
// order of row.cells.
var columnTitles = []string{"PORT", "PID", "USER", "COMMAND", "PROJECT", "UPTIME", "ADDRESS"}

// Indexes into columnTitles and row.cells.
const (
	commandColumn = 3
	projectColumn = 4
	addressColumn = 6
)

// shrinkOrder lists the columns that give up width, first to last, when the
// terminal is too narrow.
var shrinkOrder = []int{commandColumn, projectColumn, addressColumn}

// minFlexWidth is the narrowest a column in shrinkOrder may become.
const minFlexWidth = 8

// fitWidths returns the width of each column: its widest cell across lines.
// When the columns plus overhead (padding or gaps between them) would exceed
// width, the columns in shrinkOrder give up space, down to minFlexWidth.
// A width of 0 or less means unlimited.
func fitWidths(lines [][]string, width, overhead int) []int {
	widths := make([]int, len(columnTitles))
	for _, line := range lines {
		for i, c := range line {
			widths[i] = max(widths[i], ansi.StringWidth(c))
		}
	}

	total := overhead
	for _, w := range widths {
		total += w
	}
	for _, i := range shrinkOrder {
		if width <= 0 || total <= width {
			break
		}
		if cut := min(total-width, widths[i]-minFlexWidth); cut > 0 {
			widths[i] -= cut
			total -= cut
		}
	}
	return widths
}

// row is one listening port, ready to display.
type row struct {
	Port    uint16
	PID     string // "-" when the owner is not visible
	User    string
	Command string
	Project string
	Uptime  string
	Address string

	// Details shown for the selected row in the TUI.
	Dir    string // full working or compose directory, empty if unknown
	Branch string // git branch of the project, empty if none
	Listen string // addresses with the port, such as "0.0.0.0:80, [::]:80"
	Addrs  []netip.Addr

	// Container is set when a container publishes the port.
	Container *containers.Container
}

func (r row) cells() []string {
	return []string{strconv.Itoa(int(r.Port)), r.PID, r.User, r.Command, r.Project, r.Uptime, r.Address}
}

// buildRows turns a snapshot into display rows. hidden reports whether some
// owners could not be seen, which happens for other users' processes.
func buildRows(snap snapshot, now time.Time) (rows []row, hidden bool) {
	rootfs := os.DirFS(hostRoot())
	home, _ := os.UserHomeDir()

	for _, l := range snap.listeners {
		r := row{Port: l.Port, PID: "-", User: userName(l.UID), Address: joinAddrs(l.Addrs), Addrs: l.Addrs}
		command, dir, started := l.Command, l.Cwd, l.Started
		// Published container ports are held by a proxy process, so describe
		// the container instead.
		if c, ok := snap.containers[l.Port]; ok {
			r.Container = &c
			command, dir, started = c.Endpoint.Runtime+": "+c.Name, c.Dir, c.Started
		} else if l.PID == 0 {
			hidden = true
		}
		if l.PID != 0 {
			r.PID = strconv.Itoa(l.PID)
		}
		r.Command = cmp.Or(command, "-")
		r.Project = cmp.Or(project.Describe(rootfs, dir, home), "-")
		r.Dir = dir
		if root, ok := project.Root(rootfs, dir); ok && dir != "" {
			r.Branch, _ = project.Branch(rootfs, root)
		}
		listen := make([]string, len(l.Addrs))
		for i, a := range l.Addrs {
			listen[i] = netip.AddrPortFrom(a, l.Port).String()
		}
		r.Listen = strings.Join(listen, ", ")
		r.Uptime = "-"
		if !started.IsZero() {
			r.Uptime = humanDuration(now.Sub(started))
		}
		rows = append(rows, r)
	}
	return rows, hidden
}
