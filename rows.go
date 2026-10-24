package main

import (
	"cmp"
	"os"
	"strconv"
	"time"

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

// row is one listening port, ready to display.
type row struct {
	Port    uint16
	PID     string // "-" when the owner is not visible
	User    string
	Command string
	Project string
	Uptime  string
	Address string

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
		r := row{Port: l.Port, PID: "-", User: userName(l.UID), Address: joinAddrs(l.Addrs)}
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
		r.Uptime = "-"
		if !started.IsZero() {
			r.Uptime = humanDuration(now.Sub(started))
		}
		rows = append(rows, r)
	}
	return rows, hidden
}
