package main

import (
	"cmp"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/tauseef-creator/mooring/internal/project"
)

// runList prints a table of listening ports.
func runList() int {
	snap, err := scan()
	if err != nil {
		errorf("%v", err)
		return 1
	}

	rootfs := os.DirFS(cmp.Or(os.Getenv("HOST_ROOT"), "/"))
	home, _ := os.UserHomeDir()
	now := time.Now()

	hidden := false
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "PORT\tPID\tUSER\tCOMMAND\tPROJECT\tUPTIME\tADDRESS")
	for _, l := range snap.listeners {
		command, dir, started := l.Command, l.Cwd, l.Started
		// Published container ports are held by a proxy process, so describe
		// the container instead.
		if c, ok := snap.containers[l.Port]; ok {
			command, dir, started = c.Endpoint.Runtime+": "+c.Name, c.Dir, c.Started
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
	return 0
}
