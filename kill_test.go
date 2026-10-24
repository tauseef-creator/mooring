package main

import (
	"bufio"
	"flag"
	"os/exec"
	"testing"
	"time"
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

func TestTerminateGraceful(t *testing.T) {
	pid, done := startChild(t, exec.Command("sh", "-c", "echo ready; exec sleep 30"))
	forced, err := terminate([]int{pid}, done, 5*time.Second)
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
	forced, err := terminate([]int{pid}, done, 300*time.Millisecond)
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
	if _, err := terminate([]int{pid}, done, time.Second); err != nil {
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
