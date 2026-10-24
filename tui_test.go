package main

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

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

func key(s string) tea.KeyPressMsg {
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
			msg = key(k)
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
