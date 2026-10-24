package main

import (
	"net/netip"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tauseef-creator/mooring/internal/ports"
)

func TestLocalURL(t *testing.T) {
	addrs := func(ss ...string) []netip.Addr {
		var out []netip.Addr
		for _, s := range ss {
			out = append(out, netip.MustParseAddr(s))
		}
		return out
	}
	tests := []struct {
		addrs []netip.Addr
		want  string
	}{
		{addrs("0.0.0.0", "::"), "http://localhost:3000"},
		{addrs("::"), "http://localhost:3000"},
		{addrs("127.0.0.1"), "http://127.0.0.1:3000"},
		{addrs("::1"), "http://[::1]:3000"},
		{addrs("192.168.1.20", "0.0.0.0"), "http://localhost:3000"},
		{nil, "http://localhost:3000"},
	}
	for _, tt := range tests {
		if got := localURL(row{Port: 3000, Addrs: tt.addrs}); got != tt.want {
			t.Errorf("localURL(%v) = %q, want %q", tt.addrs, got, tt.want)
		}
	}
}

// killModel returns a TUI model whose only row claims that pid listens on
// port 3000.
func killModel(t *testing.T, pid int) model {
	t.Helper()
	snap := snapshot{listeners: []ports.Listener{{
		Port: 3000, PID: pid, Command: "sleep 60",
		Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")},
	}}}
	m := newModel()
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
	m, _ = update(t, m, scanMsg{snap: snap})
	return m
}

func startSleep(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd
}

func alive(cmd *exec.Cmd) bool {
	return cmd.ProcessState == nil && cmd.Process.Signal(syscall.Signal(0)) == nil
}

func TestKillPromptCancel(t *testing.T) {
	child := startSleep(t)
	m := killModel(t, child.Process.Pid)

	m, _ = update(t, m, keyPress("k"))
	if m.confirm == nil {
		t.Fatal("k did not ask for confirmation")
	}
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, `Stop "sleep 60" (PID`) || !strings.Contains(v, "y/n") {
		t.Errorf("prompt missing from footer:\n%s", v)
	}

	m, _ = update(t, m, keyPress("n"))
	if m.confirm != nil || m.stopping || m.status != "Cancelled" {
		t.Errorf("after n: confirm=%v stopping=%v status=%q", m.confirm, m.stopping, m.status)
	}
	if !alive(child) {
		t.Error("cancelling stopped the process")
	}
}

// TestKillRechecksBeforeSignaling is the safety check for PID reuse: the scan
// says the child holds port 3000, but by the time the user confirms, nothing
// with that PID listens there. The stop must not signal it.
func TestKillRechecksBeforeSignaling(t *testing.T) {
	child := startSleep(t)
	m := killModel(t, child.Process.Pid)

	m, _ = update(t, m, keyPress("k"))
	m, cmd := update(t, m, keyPress("y"))
	if !m.stopping || cmd == nil {
		t.Fatalf("y did not start stopping: stopping=%v cmd=%v", m.stopping, cmd != nil)
	}
	done, ok := cmd().(stopDoneMsg)
	if !ok {
		t.Fatalf("stop command returned %T", cmd())
	}
	if done.err != nil || !strings.Contains(done.text, "already released port 3000") {
		t.Errorf("stop result = %q, %v", done.text, done.err)
	}
	if !alive(child) {
		t.Fatal("signaled a PID that no longer held the port")
	}

	m, cmd = update(t, m, done)
	if m.stopping || cmd == nil || !strings.Contains(m.status, "already released") {
		t.Errorf("after done: stopping=%v status=%q rescan=%v", m.stopping, m.status, cmd != nil)
	}
}

func TestKillHiddenOwner(t *testing.T) {
	m := killModel(t, 0)
	m, _ = update(t, m, keyPress("k"))
	if m.confirm != nil {
		t.Error("asked to confirm stopping a process mooring cannot see")
	}
	if !m.statusErr || !strings.Contains(m.status, "run with sudo") {
		t.Errorf("status = %q (err=%v), want a sudo hint", m.status, m.statusErr)
	}
}

func TestCtrlCQuitsDuringPrompt(t *testing.T) {
	m := killModel(t, startSleep(t).Process.Pid)
	m, _ = update(t, m, keyPress("k"))
	_, cmd := update(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c did not quit during the prompt")
	}
}

func TestVimKeysDoNotMoveTable(t *testing.T) {
	m := filterModel(t)
	m, _ = update(t, m, keyPress("j"))
	if m.table.Cursor() != 0 {
		t.Errorf("j moved the cursor to %d", m.table.Cursor())
	}
	m, _ = update(t, m, downKey)
	if m.table.Cursor() != 1 {
		t.Errorf("down moved the cursor to %d, want 1", m.table.Cursor())
	}
}

func TestStatusExpires(t *testing.T) {
	m := filterModel(t)
	m, _ = update(t, m, statusMsg{text: "Copied http://localhost:3000"})
	first := m.statusID
	m, _ = update(t, m, statusMsg{text: "Opened http://localhost:3000"})

	m, _ = update(t, m, clearStatusMsg{id: first})
	if m.status == "" {
		t.Error("an older timer cleared a newer status")
	}
	m, _ = update(t, m, clearStatusMsg{id: m.statusID})
	if m.status != "" {
		t.Errorf("status = %q after its timer fired", m.status)
	}
}
