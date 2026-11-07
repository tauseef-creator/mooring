package main

import (
	"bufio"
	"flag"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tauseef-creator/mooring/internal/containers"
	"github.com/tauseef-creator/mooring/internal/ports"
)

// startChild starts a command and returns its PID and a func that reports
// whether it has exited.
func startChild(t *testing.T, cmd *exec.Cmd) (int, func() bool) {
	t.Helper()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		cmd.Process.Kill()
		<-exited
	})
	// Wait until the child says it is ready, so signals cannot race its setup.
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatalf("child did not become ready: %v", err)
	}
	return cmd.Process.Pid, func() bool {
		select {
		case <-exited:
			return true
		default:
			return false
		}
	}
}

// terminatePID pins pid then runs terminate, matching how stop() signals.
func terminatePID(t *testing.T, pid int, done func() bool, timeout time.Duration) (bool, error) {
	t.Helper()
	procs, err := pinProcesses([]int{pid})
	if err != nil {
		t.Fatal(err)
	}
	defer releaseProcesses(procs)
	return terminate(procs, done, timeout)
}

func TestTerminateGraceful(t *testing.T) {
	pid, done := startChild(t, exec.Command("sh", "-c", "echo ready; exec sleep 30"))
	forced, err := terminatePID(t, pid, done, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if forced {
		t.Error("forced = true, want a clean exit on SIGTERM")
	}
}

func TestTerminateForced(t *testing.T) {
	ignoresTerm := `trap "" TERM; echo ready; while :; do sleep 0.1; done`
	pid, done := startChild(t, exec.Command("sh", "-c", ignoresTerm))
	start := time.Now()
	forced, err := terminatePID(t, pid, done, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !forced {
		t.Error("forced = false, want SIGKILL for a process that ignores SIGTERM")
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Errorf("SIGKILL sent after %v, before the timeout", elapsed)
	}
}

func TestTerminateAlreadyGone(t *testing.T) {
	pid, done := startChild(t, exec.Command("sh", "-c", "echo ready"))
	for !done() {
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := terminatePID(t, pid, done, time.Second); err != nil {
		t.Errorf("want no error for a process that already exited, got %v", err)
	}
}

func TestParsePort(t *testing.T) {
	tests := []struct {
		in   string
		want uint16
		ok   bool
	}{
		{"3000", 3000, true},
		{":8080", 8080, true},
		{"65535", 65535, true},
		{"0", 0, false},
		{"65536", 0, false},
		{"-1", 0, false},
		{"http", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, err := parsePort(tt.in)
		if got != tt.want || (err == nil) != tt.ok {
			t.Errorf("parsePort(%q) = %d, %v; want %d, ok=%v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func TestParseInterspersed(t *testing.T) {
	for _, args := range [][]string{
		{"-y", "3000"},
		{"3000", "-y"},
		{"--yes", "3000", "-timeout", "2s"},
		{"3000", "-timeout=2s", "-y"},
	} {
		flags := flag.NewFlagSet("kill", flag.ContinueOnError)
		yes := flags.Bool("yes", false, "")
		flags.BoolVar(yes, "y", false, "")
		timeout := flags.Duration("timeout", time.Second, "")
		if err := parseInterspersed(flags, args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !*yes || flags.NArg() != 1 || flags.Arg(0) != "3000" {
			t.Errorf("%v: yes=%v args=%v, want yes=true args=[3000]", args, *yes, flags.Args())
		}
		if len(args) > 2 && *timeout != 2*time.Second {
			t.Errorf("%v: timeout = %v, want 2s", args, *timeout)
		}
	}
}

func TestRunKillRejectsNonPositiveTimeout(t *testing.T) {
	for _, args := range [][]string{
		{"-timeout", "0", "-y", "3000"},
		{"-timeout", "-1s", "-y", "3000"},
	} {
		if code := runKill(args); code != 2 {
			t.Errorf("runKill(%v) = %d, want 2", args, code)
		}
	}
}

func TestIsContainerProxy(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"docker-proxy -proto tcp -host-ip 0.0.0.0", true},
		{"/usr/bin/docker-proxy", true},
		{"rootlessport", true},
		{"pasta --config-net", true},
		{"slirp4netns --configure", true},
		{"npm run dev", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isContainerProxy(tt.cmd); got != tt.want {
			t.Errorf("isContainerProxy(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestFindTarget(t *testing.T) {
	loopback := []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	ctr := containers.Container{
		ID: "abc", Name: "api",
		Endpoint: containers.Endpoint{Runtime: "docker"},
	}

	tests := []struct {
		name    string
		snap    snapshot
		wantPID []int
		wantCtr bool
		err     string
	}{
		{
			name: "process",
			snap: snapshot{listeners: []ports.Listener{{
				Port: 3000, PID: 42, Command: "npm run dev", Addrs: loopback,
			}}},
			wantPID: []int{42},
		},
		{
			name: "container preferred over proxy",
			snap: snapshot{
				listeners: []ports.Listener{{
					Port: 3000, PID: 99, Command: "docker-proxy", Addrs: loopback,
				}},
				containers: map[uint16]containers.Container{3000: ctr},
			},
			wantCtr: true,
		},
		{
			name: "refuse docker-proxy without engine",
			snap: snapshot{listeners: []ports.Listener{{
				Port: 3000, PID: 99, Command: "docker-proxy -proto tcp", Addrs: loopback,
			}}},
			err: "container port forwarder",
		},
		{
			name: "hidden owner",
			snap: snapshot{listeners: []ports.Listener{{
				Port: 3000, PID: 0, UID: 0, Addrs: loopback,
			}}},
			err: "run with sudo",
		},
		{
			name: "nothing listening",
			snap: snapshot{},
			err:  "nothing is listening",
		},
		{
			name: "multiple PIDs",
			snap: snapshot{listeners: []ports.Listener{
				{Port: 3000, PID: 10, Command: "server a", Addrs: loopback},
				{Port: 3000, PID: 11, Command: "server b", Addrs: loopback},
			}},
			wantPID: []int{10, 11},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findTarget(tt.snap, 3000)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want substring %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantCtr {
				if got.container == nil || got.container.Name != "api" {
					t.Fatalf("container = %+v, want api", got.container)
				}
				return
			}
			if got.container != nil {
				t.Fatalf("unexpected container %+v", got.container)
			}
			if len(got.pids) != len(tt.wantPID) {
				t.Fatalf("pids = %v, want %v", got.pids, tt.wantPID)
			}
			for i, p := range tt.wantPID {
				if got.pids[i] != p {
					t.Errorf("pids[%d] = %d, want %d", i, got.pids[i], p)
				}
			}
		})
	}
}

func TestStopPinsBeforeRecheck(t *testing.T) {
	if _, err := os.Stat("/proc/net/tcp"); err != nil {
		t.Skip("requires Linux procfs")
	}
	// A live process that does not hold port 3000: after pinning, the re-check
	// must drop it, so stop reports "already released" without signaling.
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Process.Kill(); child.Wait() })

	tgt := target{
		port: 3000,
		what: `"sleep 60" (PID 1)`,
		pids: []int{child.Process.Pid},
	}
	msg, err := tgt.stop(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "already released port 3000") {
		t.Errorf("msg = %q, want already released", msg)
	}
	if child.Process.Signal(syscall.Signal(0)) != nil {
		t.Fatal("child is gone; stop may have signaled a PID that never held the port")
	}
}
