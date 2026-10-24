package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func listFixture() []row {
	return []row{
		{Port: 3000, PID: "48213", User: "dev", Command: "npm run dev", Project: "shop-frontend",
			Uptime: "2h 14m", Address: "127.0.0.1"},
		{Port: 4442, PID: "-", User: "root", Command: "docker: nascon-selenium-hub", Project: "~/Downloads/nascon-app-dev",
			Uptime: "225d 10h", Address: "0.0.0.0, ::"},
		{Port: 26575, PID: "39932", User: "tauseefrazaq", Command: "exe --type=utility --utility-sub-type=node.mojom.NodeService",
			Project: "~", Uptime: "1h 5m", Address: "127.0.0.1"},
	}
}

func printLines(t *testing.T, width int) []string {
	t.Helper()
	var b bytes.Buffer
	printTable(&b, listFixture(), width)
	return strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
}

func TestPrintTableUnlimited(t *testing.T) {
	lines := printLines(t, 0)
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want a header and 3 rows", len(lines))
	}
	// Full cells survive, except COMMAND, which is capped at maxCommandWidth.
	for _, want := range []string{"~/Downloads/nascon-app-dev", "docker: nascon-selenium-hub", "0.0.0.0, ::"} {
		if !strings.Contains(lines[2], want) {
			t.Errorf("row is missing %q: %q", want, lines[2])
		}
	}
	if !strings.Contains(lines[3], truncate("exe --type=utility --utility-sub-type=node.mojom.NodeService", maxCommandWidth)) {
		t.Errorf("long command not capped at %d: %q", maxCommandWidth, lines[3])
	}
	// Columns line up: every line has PROJECT's text at the same position.
	// Compare runes, not bytes, since "…" is one character but three bytes.
	col := len([]rune(lines[0][:strings.Index(lines[0], "PROJECT")]))
	for _, l := range lines[1:] {
		if r := []rune(l); r[col-1] != ' ' || r[col] == ' ' {
			t.Errorf("PROJECT column misaligned in %q", l)
		}
	}
	if strings.HasSuffix(lines[0], " ") {
		t.Error("header line has trailing spaces")
	}
}

func TestPrintTableFitsWidth(t *testing.T) {
	natural := printLines(t, 0)
	for _, width := range []int{120, 100, 80} {
		lines := printLines(t, width)
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > width {
				t.Errorf("width %d: line is %d wide: %q", width, w, l)
			}
		}
		// COMMAND gives up space before PROJECT does.
		if width == 120 && !strings.Contains(strings.Join(lines, "\n"), "~/Downloads/nascon-app-dev") {
			t.Errorf("width 120: PROJECT shrank although COMMAND could still give up space")
		}
		if width < ansi.StringWidth(natural[0]) && !strings.Contains(strings.Join(lines, "\n"), "…") {
			t.Errorf("width %d: nothing was truncated", width)
		}
	}
}

func TestPrintTableWideEnough(t *testing.T) {
	natural := printLines(t, 0)
	wide := printLines(t, 500)
	if strings.Join(natural, "\n") != strings.Join(wide, "\n") {
		t.Error("a wide terminal changed the output; it should match the unlimited table")
	}
}
