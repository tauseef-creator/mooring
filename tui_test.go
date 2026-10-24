package main

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tauseef-creator/mooring/internal/containers"
	"github.com/tauseef-creator/mooring/internal/ports"
)

var testNow = time.Now()

func testSnapshot(portNums ...uint16) snapshot {
	var ls []ports.Listener
	for _, p := range portNums {
		ls = append(ls, ports.Listener{
			Port: p, Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")},
			PID: int(p), Command: "server --port " + strings.Repeat("x", 30),
		})
	}
	return snapshot{listeners: ls}
}

func keyPress(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// update applies msg and returns the concrete model.
func update(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(model), cmd
}

func TestColumnsFitNaturalWidth(t *testing.T) {
	rows, _ := buildRows(testSnapshot(3000), testNow)
	cols := columns(rows, 1000)
	for i, c := range cols {
		want := max(len(columnTitles[i]), len(rows[0].cells()[i]))
		if c.Width != want {
			t.Errorf("%s width = %d, want %d", c.Title, c.Width, want)
		}
	}
}

func TestColumnsShrinkWhenNarrow(t *testing.T) {
	rows, _ := buildRows(testSnapshot(3000), testNow)
	const width = 60
	total := 0
	cols := columns(rows, width)
	for _, c := range cols {
		total += c.Width + cellPadding
		if c.Width < minFlexWidth && c.Width < len(c.Title) {
			t.Errorf("%s shrank to %d", c.Title, c.Width)
		}
	}
	natural := columns(rows, 1000)
	if cols[commandColumn].Width >= natural[commandColumn].Width {
		t.Error("COMMAND did not shrink")
	}
	if total > width && cols[commandColumn].Width != minFlexWidth {
		t.Errorf("total %d > %d but COMMAND still has slack", total, width)
	}
}

func TestScanKeepsCursorOnSamePort(t *testing.T) {
	m := newModel()
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = update(t, m, scanMsg{snap: testSnapshot(3000, 5173, 8080)})
	m.table.SetCursor(1) // 5173

	// 3000 went away and 4000 appeared before 5173.
	m, _ = update(t, m, scanMsg{snap: testSnapshot(4000, 4500, 5173, 8080)})
	if got := m.rows[m.table.Cursor()].Port; got != 5173 {
		t.Errorf("cursor on port %d, want 5173", got)
	}
}

func TestRefreshLoopIgnoresStaleTicks(t *testing.T) {
	m := newModel()
	m, cmd := update(t, m, scanMsg{snap: testSnapshot(3000)})
	if cmd == nil {
		t.Fatal("scan result did not schedule a tick")
	}
	stale := tickMsg{gen: m.gen}
	m, _ = update(t, m, scanMsg{snap: testSnapshot(3000)}) // e.g. a manual refresh

	if _, cmd := update(t, m, stale); cmd != nil {
		t.Error("stale tick started a scan; refresh loops would multiply")
	}
	if _, cmd := update(t, m, tickMsg{gen: m.gen}); cmd == nil {
		t.Error("current tick did not start a scan")
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		var msg tea.KeyPressMsg
		switch k {
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "ctrl+c":
			msg = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
		default:
			msg = keyPress(k)
		}
		_, cmd := update(t, newModel(), msg)
		if cmd == nil {
			t.Errorf("%s: no command", k)
			continue
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s: did not quit", k)
		}
	}
}

// detailsModel returns a sized model showing one row built from l, with
// HOST_ROOT pointing at a fake filesystem holding a git repo at /shop.
func detailsModel(t *testing.T, width, height int, l ports.Listener, c *containers.Container) model {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "shop/.git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "shop/.git/HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOST_ROOT", root)

	snap := snapshot{listeners: []ports.Listener{l}}
	if c != nil {
		snap.containers = map[uint16]containers.Container{l.Port: *c}
	}
	m := newModel()
	m, _ = update(t, m, tea.WindowSizeMsg{Width: width, Height: height})
	m, _ = update(t, m, scanMsg{snap: snap})
	return m
}

func TestDetailsPanel(t *testing.T) {
	process := ports.Listener{
		Port: 3000, PID: 4321, Command: "npm run dev", Cwd: "/shop/apps/web",
		Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::1")},
	}
	hidden := ports.Listener{Port: 53, Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.53")}}
	container := &containers.Container{
		ID: "0123456789abcdef", Name: "api", Image: "api:dev", Dir: "/shop",
		Endpoint: containers.Endpoint{Runtime: "docker"},
	}

	tests := []struct {
		name string
		l    ports.Listener
		c    *containers.Container
		want []string
	}{
		{"process", process, nil, []string{
			"npm run dev", "(PID 4321, user", "path    /shop/apps/web",
			"branch  main", "listen  127.0.0.1:3000, [::1]:3000",
		}},
		{"container", hidden, container, []string{
			"docker container api", "(image api:dev, id 0123456789ab)", "path    /shop", "branch  main",
		}},
		{"hidden owner", hidden, nil, []string{
			"Process of user", "run with sudo", "path    -", "branch  -", "listen  127.0.0.53:53",
		}},
	}
	for _, tt := range tests {
		v := ansi.Strip(detailsModel(t, 120, 30, tt.l, tt.c).View().Content)
		for _, want := range tt.want {
			if !strings.Contains(v, want) {
				t.Errorf("%s: view is missing %q", tt.name, want)
			}
		}
	}
}

func TestDetailsPanelHiddenWhenShort(t *testing.T) {
	l := ports.Listener{Port: 3000, PID: 1, Command: "vite", Addrs: []netip.Addr{netip.MustParseAddr("::1")}}
	m := detailsModel(t, 120, 10, l, nil)
	if m.showDetails || strings.Contains(ansi.Strip(m.View().Content), "listen  ") {
		t.Error("details panel shown in a 10-line window")
	}
}

func TestDetailsPanelNeverWraps(t *testing.T) {
	l := ports.Listener{
		Port: 3000, PID: 1, Command: "node " + strings.Repeat("--flag ", 20),
		Cwd: "/shop/" + strings.Repeat("deep/", 20), Addrs: []netip.Addr{netip.MustParseAddr("::1")},
	}
	const width = 50
	m := detailsModel(t, width, 30, l, nil)
	panel := m.detailsView()
	if got := strings.Count(panel, "\n") + 1; got != detailsLines {
		t.Errorf("panel has %d lines, want %d", got, detailsLines)
	}
	for _, line := range strings.Split(panel, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line is %d wide, window is %d: %q", w, width, line)
		}
	}
}

func TestViewStates(t *testing.T) {
	m := newModel()
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	if v := m.View().Content; !strings.Contains(v, "Scanning") {
		t.Errorf("before first scan, view = %q", v)
	}
	m, _ = update(t, m, scanMsg{snap: testSnapshot()})
	if v := m.View().Content; !strings.Contains(v, "Nothing is listening") {
		t.Errorf("with no ports, view = %q", v)
	}
	m, _ = update(t, m, scanMsg{snap: testSnapshot(3000, 8080)})
	v := m.View().Content
	for _, want := range []string{"2 ports · 0 containers", "3000", "8080"} {
		if !strings.Contains(v, want) {
			t.Errorf("view is missing %q", want)
		}
	}
}
