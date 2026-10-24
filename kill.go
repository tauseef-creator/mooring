package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"mooring/internal/containers"
	"mooring/internal/ports"
)

const killUsage = `Usage: mooring kill [flags] <port>

Stops whatever is listening on <port>. Processes get SIGTERM, then SIGKILL
if they are still running after the timeout. Containers are stopped through
their engine, which does the same.

Flags:
`

// defaultKillTimeout matches the grace period "docker stop" uses.
const defaultKillTimeout = 10 * time.Second

// killPollInterval is how often to check whether the port has been released.
const killPollInterval = 100 * time.Millisecond

func runKill(args []string) int {
	flags := flag.NewFlagSet("kill", flag.ContinueOnError)
	var yes bool
	flags.BoolVar(&yes, "y", false, "do not ask for confirmation")
	flags.BoolVar(&yes, "yes", false, "same as -y")
	timeout := flags.Duration("timeout", defaultKillTimeout, "how long to wait after SIGTERM before sending SIGKILL")
	flags.Usage = func() {
		fmt.Fprint(flags.Output(), killUsage)
		flags.PrintDefaults()
	}
	if err := parseInterspersed(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return 2
	}
	port, err := parsePort(flags.Arg(0))
	if err != nil {
		errorf("%v", err)
		return 2
	}

	snap, err := scan()
	if err != nil {
		errorf("%v", err)
		return 1
	}
	if c, ok := snap.containers[port]; ok {
		return stopContainer(c, port, yes, *timeout)
	}
	return stopProcesses(snap.listeners, port, yes, *timeout)
}

func stopContainer(c containers.Container, port uint16, yes bool, timeout time.Duration) int {
	what := fmt.Sprintf("%s container %q", c.Endpoint.Runtime, c.Name)
	if !confirm(fmt.Sprintf("Stop %s on port %d?", what, port), yes) {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout+containerAPITimeout)
	defer cancel()
	if err := c.Stop(ctx, timeout); err != nil {
		errorf("stopping %s: %v", what, err)
		return 1
	}
	fmt.Printf("Stopped %s on port %d\n", what, port)
	return 0
}

func stopProcesses(ls []ports.Listener, port uint16, yes bool, timeout time.Duration) int {
	var targets []ports.Listener
	for _, l := range ls {
		if l.Port == port {
			targets = append(targets, l)
		}
	}
	if len(targets) == 0 {
		errorf("nothing is listening on port %d", port)
		return 1
	}

	var pids []int
	var names []string
	for _, t := range targets {
		if t.PID == 0 {
			errorf("port %d belongs to a process of user %s that mooring cannot see; run with sudo", port, userName(t.UID))
			return 1
		}
		pids = append(pids, t.PID)
		names = append(names, fmt.Sprintf("%q (PID %d)", truncate(t.Command, maxCommandWidth), t.PID))
	}
	what := strings.Join(names, ", ")
	if !confirm(fmt.Sprintf("Stop %s on port %d?", what, port), yes) {
		return 1
	}

	procfs := os.DirFS(procRoot())
	released := func() bool {
		ls, err := ports.List(procfs)
		if err != nil {
			return false
		}
		return !slices.ContainsFunc(ls, func(l ports.Listener) bool {
			return l.Port == port && slices.Contains(pids, l.PID)
		})
	}
	forced, err := terminate(pids, released, timeout)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			errorf("%v; run with sudo to stop another user's process", err)
		} else {
			errorf("%v", err)
		}
		return 1
	}
	if forced {
		fmt.Printf("Killed %s on port %d (it ignored SIGTERM for %s)\n", what, port, timeout)
	} else {
		fmt.Printf("Stopped %s on port %d\n", what, port)
	}
	return 0
}

// terminate sends SIGTERM to each process and waits until done reports true.
// Processes still running after timeout get SIGKILL. It reports whether
// SIGKILL was needed.
func terminate(pids []int, done func() bool, timeout time.Duration) (forced bool, err error) {
	var procs []*os.Process
	for _, pid := range pids {
		// On Linux this holds a pidfd, so a PID reused by a new process is
		// never signaled by mistake.
		p, err := os.FindProcess(pid)
		if err != nil {
			return false, err
		}
		defer p.Release()
		procs = append(procs, p)
	}
	signal := func(sig syscall.Signal) error {
		for _, p := range procs {
			if err := p.Signal(sig); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return fmt.Errorf("sending %v to PID %d: %w", sig, p.Pid, err)
			}
		}
		return nil
	}

	if err := signal(syscall.SIGTERM); err != nil {
		return false, err
	}
	if waitUntil(done, timeout) {
		return false, nil
	}
	if err := signal(syscall.SIGKILL); err != nil {
		return true, err
	}
	if waitUntil(done, timeout) {
		return true, nil
	}
	return true, errors.New("the port is still in use after SIGKILL")
}

// waitUntil polls cond until it returns true or timeout passes.
func waitUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(killPollInterval)
	}
}

// confirm asks question on the terminal and reports whether the user agreed.
// Without a terminal it refuses, so scripts must pass -y explicitly.
func confirm(question string, yes bool) bool {
	if yes {
		return true
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		errorf("stdin is not a terminal, so cannot ask for confirmation; pass -y to proceed")
		return false
	}
	fmt.Printf("%s [y/N] ", question)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	fmt.Println("Cancelled.")
	return false
}

// parsePort accepts "3000" or ":3000".
func parsePort(s string) (uint16, error) {
	n, err := strconv.ParseUint(strings.TrimPrefix(s, ":"), 10, 16)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("invalid port %q: want a number from 1 to 65535", s)
	}
	return uint16(n), nil
}

// parseInterspersed parses flags given before or after positional arguments,
// so both "kill -y 3000" and "kill 3000 -y" work. The standard flag package
// stops at the first positional argument.
func parseInterspersed(flags *flag.FlagSet, args []string) error {
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			return err
		}
		args = flags.Args()
		if len(args) == 0 {
			break
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
	return flags.Parse(positional)
}
